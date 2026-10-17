package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"strings"
	"time"

	"github.com/Sriram-PR/ghv/internal/read"
	"github.com/Sriram-PR/ghv/internal/sanitize"
)

var noBotsFlag = flag{name: "no-bots", kind: boolFlag, help: `leave out bots: accounts of type Bot and logins ending in "[bot]"`}

func init() {
	register(&command{
		path: "pr activity", args: []string{"N"}, flags: []flag{repoFlag, sinceFlag, noBotsFlag},
		help: "Show a pull request's comments, reviews, and inline review comments as one timeline",
		run:  func(ctx context.Context, a *app) error { return runActivity(ctx, a, true) },
	})
	register(&command{
		path: "issue activity", args: []string{"N"}, flags: []flag{repoFlag, sinceFlag, noBotsFlag},
		help: "Show an issue's comments as a timeline",
		run:  func(ctx context.Context, a *app) error { return runActivity(ctx, a, false) },
	})
}

// activityEvent is one item of the --json events list. Kind is comment,
// review, or review_comment; Time is when it was posted or submitted.
type activityEvent struct {
	Kind      string    `json:"kind"`
	ID        int64     `json:"id"`
	Author    string    `json:"author"`
	Bot       bool      `json:"bot"`
	Time      time.Time `json:"time"`
	State     string    `json:"state,omitempty"`
	Path      string    `json:"path,omitempty"`
	Line      int       `json:"line,omitempty"`
	Outdated  bool      `json:"outdated,omitempty"`
	InReplyTo int64     `json:"in_reply_to,omitempty"`
	URL       string    `json:"url"`
	Body      string    `json:"body"`
}

// activityView is the stable --json schema of ghv pr activity and ghv
// issue activity.
type activityView struct {
	Repo   string          `json:"repo"`
	Number int             `json:"number"`
	Events []activityEvent `json:"events"`
}

type githubEvent struct {
	ID           int64      `json:"id"`
	User         githubUser `json:"user"`
	CreatedAt    time.Time  `json:"created_at"`
	SubmittedAt  *time.Time `json:"submitted_at"`
	State        string     `json:"state"`
	Path         string     `json:"path"`
	Line         *int       `json:"line"`
	OriginalLine *int       `json:"original_line"`
	InReplyTo    int64      `json:"in_reply_to_id"`
	HTMLURL      string     `json:"html_url"`
	Body         string     `json:"body"`
}

func isBot(u githubUser) bool { return u.Type == "Bot" || strings.HasSuffix(u.Login, "[bot]") }

func runActivity(ctx context.Context, a *app, pr bool) error {
	repo, n, err := a.repoAndNumber()
	if err != nil {
		return err
	}
	since, err := a.since()
	if err != nil {
		return err
	}
	c, err := a.read()
	if err != nil {
		return err
	}
	q := ""
	if !since.IsZero() {
		q = "?" + url.Values{"since": {since.Format(time.RFC3339)}}.Encode()
	}
	type source struct{ path, kind string }
	sources := []source{{fmt.Sprintf("repos/%s/issues/%d/comments%s", repo, n, q), "comment"}}
	if pr {
		sources = append(sources,
			source{fmt.Sprintf("repos/%s/pulls/%d/reviews", repo, n), "review"},
			source{fmt.Sprintf("repos/%s/pulls/%d/comments%s", repo, n, q), "review_comment"})
	}
	v := activityView{Repo: repo, Number: n, Events: []activityEvent{}}
	for _, s := range sources {
		evs, err := fetchEvents(ctx, c, s.path, s.kind)
		if err != nil {
			return err
		}
		for _, ev := range evs {
			if ev.Time.Before(since) || (ev.Bot && a.inv.bool("no-bots")) {
				continue
			}
			v.Events = append(v.Events, ev)
		}
	}
	slices.SortStableFunc(v.Events, func(x, y activityEvent) int { return x.Time.Compare(y.Time) })
	if a.json() {
		return writeJSON(a.stdout, v)
	}
	for _, ev := range v.Events {
		fmt.Fprintf(a.stdout, "%s  %s  %s\n", ev.Time.UTC().Format(time.RFC3339), sanitize.Line(ev.Author), sanitize.Line(eventLabel(ev)))
		if body := strings.TrimRight(sanitize.Terminal(ev.Body), "\n"); body != "" {
			fmt.Fprintln(a.stdout, "    "+strings.ReplaceAll(body, "\n", "\n    "))
		}
		fmt.Fprintln(a.stdout)
	}
	return nil
}

func eventLabel(ev activityEvent) string {
	switch ev.Kind {
	case "review":
		return "review " + ev.State
	case "review_comment":
		s := fmt.Sprintf("review comment %d on %s:%d", ev.ID, ev.Path, ev.Line)
		if ev.Outdated {
			s += " (outdated)"
		}
		if ev.InReplyTo != 0 {
			s += fmt.Sprintf(", reply to %d", ev.InReplyTo)
		}
		return s
	}
	return ev.Kind
}

// fetchEvents reads one timeline source. Reviews still pending have no
// submitted_at and are left out.
func fetchEvents(ctx context.Context, c *read.Client, path, kind string) ([]activityEvent, error) {
	items, err := c.Paginate(ctx, path, "", read.DefaultLimit)
	if err != nil {
		return nil, err
	}
	var out []activityEvent
	for _, raw := range items {
		var g githubEvent
		if err := json.Unmarshal(raw, &g); err != nil {
			return nil, fmt.Errorf("GitHub's %s list: %v", strings.ReplaceAll(kind, "_", " "), err)
		}
		ev := activityEvent{Kind: kind, ID: g.ID, Author: g.User.Login, Bot: isBot(g.User), Time: g.CreatedAt, URL: g.HTMLURL, Body: g.Body}
		switch kind {
		case "review":
			if g.SubmittedAt == nil {
				continue
			}
			ev.Time, ev.State = *g.SubmittedAt, g.State
		case "review_comment":
			ev.Path, ev.InReplyTo = g.Path, g.InReplyTo
			switch {
			case g.Line != nil:
				ev.Line = *g.Line
			case g.OriginalLine != nil:
				ev.Line, ev.Outdated = *g.OriginalLine, true
			}
		}
		out = append(out, ev)
	}
	return out, nil
}
