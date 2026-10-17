package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/Sriram-PR/ghv/internal/sanitize"
)

func init() {
	register(&command{path: "issue view", args: []string{"N"}, flags: []flag{repoFlag}, help: "Show an issue", run: runIssueView})
	register(&command{
		path: "issue list",
		flags: []flag{
			repoFlag, stateFlag,
			{name: "label", kind: multiFlag, arg: "NAME", help: "only issues with this label; repeat for all of several"},
			sinceFlag, limitFlag,
		},
		help: "List issues, most recently created first; --since filters by last update",
		run:  runIssueList,
	})
}

type githubIssue struct {
	Number      int        `json:"number"`
	Title       string     `json:"title"`
	State       string     `json:"state"`
	StateReason string     `json:"state_reason"`
	User        githubUser `json:"user"`
	Labels      []struct {
		Name string `json:"name"`
	} `json:"labels"`
	Assignees   []githubUser    `json:"assignees"`
	Comments    int             `json:"comments"`
	Body        string          `json:"body"`
	CreatedAt   time.Time       `json:"created_at"`
	UpdatedAt   time.Time       `json:"updated_at"`
	ClosedAt    *time.Time      `json:"closed_at"`
	HTMLURL     string          `json:"html_url"`
	PullRequest json.RawMessage `json:"pull_request"`
}

func (g githubIssue) labels() []string {
	l := []string{}
	for _, x := range g.Labels {
		l = append(l, x.Name)
	}
	return l
}

// issueView is the stable --json schema of ghv issue view.
type issueView struct {
	Repo        string     `json:"repo"`
	Number      int        `json:"number"`
	Title       string     `json:"title"`
	State       string     `json:"state"`
	StateReason string     `json:"state_reason,omitempty"`
	Author      string     `json:"author"`
	Labels      []string   `json:"labels"`
	Assignees   []string   `json:"assignees"`
	Comments    int        `json:"comments"`
	CreatedAt   time.Time  `json:"created_at"`
	UpdatedAt   time.Time  `json:"updated_at"`
	ClosedAt    *time.Time `json:"closed_at"`
	URL         string     `json:"url"`
	Body        string     `json:"body"`
}

// issueSummary is one item of ghv issue list --json.
type issueSummary struct {
	Number    int       `json:"number"`
	Title     string    `json:"title"`
	State     string    `json:"state"`
	Author    string    `json:"author"`
	Labels    []string  `json:"labels"`
	Comments  int       `json:"comments"`
	UpdatedAt time.Time `json:"updated_at"`
	URL       string    `json:"url"`
}

func runIssueView(ctx context.Context, a *app) error {
	repo, n, err := a.repoAndNumber()
	if err != nil {
		return err
	}
	c, err := a.read()
	if err != nil {
		return err
	}
	resp, err := c.Get(ctx, fmt.Sprintf("repos/%s/issues/%d", repo, n), "")
	if err != nil {
		return err
	}
	var g githubIssue
	if err := json.Unmarshal(resp.Body, &g); err != nil {
		return fmt.Errorf("GitHub's issue response: %v", err)
	}
	if g.PullRequest != nil {
		return fmt.Errorf("%s#%d is a pull request; use ghv pr view %d", repo, n, n)
	}
	v := issueView{
		Repo: repo, Number: g.Number, Title: g.Title, State: g.State, StateReason: g.StateReason,
		Author: g.User.Login, Labels: g.labels(), Assignees: []string{}, Comments: g.Comments,
		CreatedAt: g.CreatedAt, UpdatedAt: g.UpdatedAt, ClosedAt: g.ClosedAt, URL: g.HTMLURL, Body: g.Body,
	}
	for _, u := range g.Assignees {
		v.Assignees = append(v.Assignees, u.Login)
	}
	if a.json() {
		return writeJSON(a.stdout, v)
	}
	out := a.stdout
	fmt.Fprintf(out, "%s#%d %s\n\n", repo, v.Number, sanitize.Line(v.Title))
	row := rowWriter(out)
	state := v.State
	if v.StateReason != "" {
		state += " (" + v.StateReason + ")"
	}
	row("state", state)
	row("author", v.Author)
	row("labels", strings.Join(v.Labels, ", "))
	row("assignees", strings.Join(v.Assignees, ", "))
	row("comments", fmt.Sprint(v.Comments))
	row("created", v.CreatedAt.Format(time.RFC3339))
	row("updated", v.UpdatedAt.Format(time.RFC3339))
	if v.ClosedAt != nil {
		row("closed", v.ClosedAt.Format(time.RFC3339))
	}
	row("url", v.URL)
	writeBody(out, v.Body)
	return nil
}

func isPullRequest(raw json.RawMessage) bool {
	var probe struct {
		PullRequest json.RawMessage `json:"pull_request"`
	}
	return json.Unmarshal(raw, &probe) == nil && probe.PullRequest != nil
}

func runIssueList(ctx context.Context, a *app) error {
	state, err := a.state()
	if err != nil {
		return err
	}
	since, err := a.since()
	if err != nil {
		return err
	}
	repo, err := a.repo()
	if err != nil {
		return err
	}
	q := url.Values{"state": {state}}
	if l := a.inv.list("label"); len(l) > 0 {
		q.Set("labels", strings.Join(l, ","))
	}
	if !since.IsZero() {
		q.Set("since", since.Format(time.RFC3339))
	}
	c, err := a.read()
	if err != nil {
		return err
	}
	items, err := c.PaginateFunc(ctx, fmt.Sprintf("repos/%s/issues?%s", repo, q.Encode()), "", a.inv.int("limit"),
		func(raw json.RawMessage) bool { return !isPullRequest(raw) })
	if err != nil {
		return err
	}
	list := []issueSummary{}
	for _, raw := range items {
		var g githubIssue
		if err := json.Unmarshal(raw, &g); err != nil {
			return fmt.Errorf("GitHub's issue list: %v", err)
		}
		list = append(list, issueSummary{g.Number, g.Title, g.State, g.User.Login, g.labels(), g.Comments, g.UpdatedAt, g.HTMLURL})
	}
	if a.json() {
		return writeJSON(a.stdout, list)
	}
	rows := make([][]string, len(list))
	for i, s := range list {
		rows[i] = []string{fmt.Sprintf("#%d", s.Number), s.State, s.Author, s.Title}
	}
	writeTable(a.stdout, rows)
	return nil
}
