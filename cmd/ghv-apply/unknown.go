package main

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/Sriram-PR/ghv/internal/audit"
	"github.com/Sriram-PR/ghv/internal/draft"
)

func waitsToBeChecked(n int) string {
	if n == 1 {
		return "1 draft in unknown/ waits for its result to be checked"
	}
	return fmt.Sprintf("%d drafts in unknown/ wait for their result to be checked", n)
}

// unknownDraft is a draft in unknown/, read once, with when it was sent.
type unknownDraft struct {
	id     string
	loaded draft.Loaded
	err    error
	sentAt time.Time
	edited bool
}

func readUnknown(q draft.Queue, es []draft.Entry) []unknownDraft {
	var us []unknownDraft
	for _, en := range es {
		l, err := draft.ReadFile(filepath.Join(q.Dir(draft.Unknown), en.ID+".json"))
		u := unknownDraft{id: en.ID, loaded: l, err: err, sentAt: l.Draft.CreatedAt}
		if o := en.Outcome; o != nil {
			if !o.At.IsZero() {
				u.sentAt = o.At
			}
			u.edited = o.Edited
		}
		us = append(us, u)
	}
	slices.SortStableFunc(us, func(a, b unknownDraft) int {
		if (a.err != nil) != (b.err != nil) {
			if a.err != nil {
				return 1
			}
			return -1
		}
		return a.loaded.Draft.CreatedAt.Compare(b.loaded.Draft.CreatedAt)
	})
	return us
}

// recoverUnknown checks on GitHub each draft whose send had no clear
// answer. One found moves to applied/ with an audit line; for one not
// found the reviewer decides; a failed lookup leaves it for next time.
func (ss *session) recoverUnknown(ctx context.Context, es []draft.Entry) (kept, requeued int, err error) {
	us := readUnknown(ss.q, es)
	var foundN, rejectedN int
	for i, u := range us {
		ss.tty.Printf("\n== Unknown result %d of %d ==\n", i+1, len(us))
		if u.err != nil {
			ss.say("Draft %s in unknown/ cannot be read: %v; it stays there.", u.id, u.err)
			kept++
			continue
		}
		d := u.loaded.Draft
		ss.say("%s: %s to %s, sent %s", u.id, d.Kind(), d.Repo, u.sentAt.UTC().Format("2006-01-02 15:04 UTC"))
		f, err := ss.rv.findSent(ctx, d, u.sentAt, u.edited)
		if err != nil {
			ss.say("Cannot check %s: %v; it stays in unknown/.", u.id, err)
			kept++
			continue
		}
		if f.URL != "" {
			if ss.foundSent(u, f) {
				foundN++
			} else {
				kept++
			}
			continue
		}
		ss.say("Not found on GitHub. Checked %s.", f.Checked)
		to, err := ss.askUnknown()
		if err != nil {
			return kept, requeued, err
		}
		switch to {
		case draft.Pending:
			err = ss.q.Move(u.id, draft.Unknown, draft.Pending, u.loaded.SHA256, draft.Outcome{At: ss.e.now().UTC(), Note: "not found on GitHub after an unknown send; queued again"})
			if err == nil {
				ss.say("Queued %s again.", u.id)
				requeued++
				continue
			}
		case draft.Rejected:
			err = ss.q.Move(u.id, draft.Unknown, draft.Rejected, u.loaded.SHA256, draft.Outcome{At: ss.e.now().UTC(), Reason: "not found on GitHub after an unknown send"})
			if err == nil {
				ss.say("Rejected %s.", u.id)
				rejectedN++
				continue
			}
		default:
			ss.say("Kept %s in unknown/.", u.id)
		}
		if err != nil {
			ss.say("Cannot move %s: %v; it stays in unknown/.", u.id, err)
		}
		kept++
	}
	ss.tty.Printf("\nChecked %s: found %d, queued again %d, rejected %d, kept %d.\n",
		plural(len(us), "unknown result", "unknown results"), foundN, requeued, rejectedN, kept)
	return kept, requeued, nil
}

// foundSent records a draft GitHub shows was made: applied/, with the
// send time, and an audit line marked found.
func (ss *session) foundSent(u unknownDraft, f found) bool {
	d := u.loaded.Draft
	o := draft.Outcome{At: u.sentAt, URL: f.URL, FullName: f.FullName, Edited: u.edited, Note: "found on GitHub after an unknown send"}
	if err := ss.q.Move(u.id, draft.Unknown, draft.Applied, u.loaded.SHA256, o); err != nil {
		ss.say("Found on GitHub: %s, but cannot move %s to applied/: %v", f.URL, u.id, err)
		return false
	}
	ss.say("Found on GitHub: %s", f.URL)
	err := audit.Append(audit.Path(ss.e.home), audit.Entry{Time: u.sentAt, Draft: u.id, Kind: string(d.Kind()), Repo: d.Repo, URL: f.URL, Found: true})
	if err != nil {
		ss.say("Warning: cannot write the audit log: %v", err)
	}
	return true
}

// askUnknown is where a draft not found goes: back to the queue, to
// rejected/, or nowhere (Unknown).
func (ss *session) askUnknown() (draft.State, error) {
	for {
		ans, err := ss.tty.ReadLine("[q]ueue it again, [r]eject it, or [k]eep it in unknown/? ")
		if err != nil {
			return "", err
		}
		switch strings.ToLower(strings.TrimSpace(ans)) {
		case "q", "queue":
			return draft.Pending, nil
		case "r", "reject":
			return draft.Rejected, nil
		case "k", "keep":
			return draft.Unknown, nil
		}
		ss.tty.Printf("Answer with one of the letters in brackets.\n")
	}
}
