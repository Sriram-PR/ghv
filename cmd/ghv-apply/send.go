package main

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Sriram-PR/ghv/internal/audit"
	"github.com/Sriram-PR/ghv/internal/draft"
	"github.com/Sriram-PR/ghv/internal/fetch"
	"github.com/Sriram-PR/ghv/internal/sanitize"
	"github.com/Sriram-PR/ghv/internal/write"
)

// send makes the approved request. The draft moves to unknown/ first, so a
// crash mid-send never leaves it looking unsent; GitHub's answer decides
// where it goes next. stop ends the session: nothing more can be sent.
func (ss *session) send(ctx context.Context, it item, d draft.Draft, fullName string, req write.Request, edited bool) (tally, bool) {
	if f, ok := d.Fields.(draft.PRCreateFields); ok {
		if why := ss.headMoved(ctx, d.Repo, f); why != "" {
			ss.say("%s. Not sent; %s stays in the queue.", why, it.id)
			return left, false
		}
	}
	now := ss.e.now().UTC()
	if err := ss.q.Move(it.id, draft.Pending, draft.Unknown, it.loaded.SHA256, draft.Outcome{At: now, Note: "sending", Edited: edited}); err != nil {
		ss.say("Not sent: %v", err)
		return left, false
	}
	res := write.Send(ctx, ss.e.client, ss.tok, req)
	o := draft.Outcome{At: now, Status: res.Status, Edited: edited}
	switch res.Fate {
	case write.Done:
		o.URL, o.FullName = res.URL, res.FullName
		var notes []string
		// A pull request edit's answer names the pull request's author.
		if d.Kind() != draft.KindPREdit && !strings.EqualFold(res.Author, ss.rv.login) {
			notes = append(notes, fmt.Sprintf("GitHub says %s made it, not %s", res.Author, ss.rv.login))
		}
		if f, ok := d.Fields.(draft.PRCreateFields); ok && res.HeadSHA != f.HeadSHA {
			notes = append(notes, fmt.Sprintf("the pull request's head is %s, not the reviewed %s", res.HeadSHA, f.HeadSHA))
		}
		ss.say("Sent: %s", res.URL)
		if _, ok := d.Fields.(draft.ForkFields); ok {
			if note := ss.waitForFork(ctx, res.FullName); note != "" {
				notes = append(notes, note)
			}
		}
		o.Note = strings.Join(notes, "; ")
		if o.Note != "" {
			ss.say("Warning: %s.", o.Note)
		}
		err := audit.Append(audit.Path(ss.e.home), audit.Entry{
			Time: now, Draft: it.id, Kind: string(d.Kind()), Repo: fullName,
			RequestSHA256: req.SHA256(), Status: res.Status, URL: res.URL,
		})
		if err != nil {
			ss.say("Warning: cannot write the audit log: %v", err)
		}
		ss.finish(it, draft.Applied, o)
		return sent, false
	case write.Refused:
		o.Reason = "GitHub refused it: " + res.Message
		ss.say("%s", o.Reason)
		ss.finish(it, draft.Rejected, o)
		return rejected, false
	case write.NotSent:
		o.Note = "not sent: " + res.Message
		ss.say("Not sent: %s", res.Message)
		ss.say("The session ends here; %s stays in the queue.", it.id)
		ss.finish(it, draft.Pending, o)
		return left, true
	}
	ss.say("The request may have gone out, but GitHub's answer does not say what happened: %s", res.Message)
	ss.say("%s stays in unknown/; check GitHub before asking for it again.", it.id)
	return unsure, false
}

// Forks are made in the background after GitHub's 202.
const (
	forkPoll  = 3 * time.Second
	forkTries = 21 // the first read at once, the last after 60 seconds
)

// waitForFork reads the fork until GitHub has made it, and says why not
// when it gives up.
func (ss *session) waitForFork(ctx context.Context, fullName string) string {
	if fullName == "" {
		return "GitHub did not say the fork's name"
	}
	ss.say("Waiting for GitHub to make the fork...")
	for i := range forkTries {
		if i > 0 {
			ss.e.sleep(forkPoll)
		}
		_, err := ss.rv.fc.Repo(ctx, fullName)
		if err == nil {
			ss.say("Fork ready: https://github.com/%s", fullName)
			return ""
		}
		if !errors.Is(err, fetch.ErrNotFound) {
			return fmt.Sprintf("cannot check the fork: %v", err)
		}
	}
	return "GitHub had not finished the fork after 60 seconds"
}

// headMoved says why a pull request cannot be sent now, or "" when its
// head branch still points at the reviewed commit. Minutes can pass
// between the preview and the answer.
func (ss *session) headMoved(ctx context.Context, draftRepo string, f draft.PRCreateFields) string {
	_, repo, branch := headOf(draftRepo, f)
	sha, err := ss.rv.fc.BranchHead(ctx, repo, branch)
	switch {
	case err != nil:
		return fmt.Sprintf("Cannot check where %s points: %v", f.Head, err)
	case sha != f.HeadSHA:
		return fmt.Sprintf("%s has moved from %s to %s since you reviewed it", f.Head, short(f.HeadSHA), short(sha))
	}
	return ""
}

// finish records where a sent draft ended up. The request has gone either
// way, so a failure here is reported, not fatal.
func (ss *session) finish(it item, to draft.State, o draft.Outcome) {
	if err := ss.q.Move(it.id, draft.Unknown, to, it.loaded.SHA256, o); err != nil {
		ss.say("Warning: cannot move %s out of unknown/: %v", it.id, err)
	}
}

func (ss *session) say(format string, a ...any) {
	ss.tty.Printf("%s\n", sanitize.Line(fmt.Sprintf(format, a...)))
}
