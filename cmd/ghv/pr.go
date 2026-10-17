package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/Sriram-PR/ghv/internal/sanitize"
)

func init() {
	register(&command{path: "pr view", args: []string{"N"}, flags: []flag{repoFlag}, help: "Show a pull request", run: runPRView})
	register(&command{
		path:  "pr list",
		flags: []flag{repoFlag, stateFlag, limitFlag},
		help:  "List pull requests, most recently created first",
		run:   runPRList,
	})
}

var stateFlag = flag{name: "state", kind: stringFlag, arg: "STATE", help: "open, closed, or all", def: "open"}

type githubUser struct {
	Login string `json:"login"`
	Type  string `json:"type"`
}

type githubPR struct {
	Number int        `json:"number"`
	Title  string     `json:"title"`
	State  string     `json:"state"`
	Draft  bool       `json:"draft"`
	User   githubUser `json:"user"`
	Base   struct {
		Ref string `json:"ref"`
	} `json:"base"`
	Head struct {
		Label string `json:"label"`
		SHA   string `json:"sha"`
	} `json:"head"`
	Labels []struct {
		Name string `json:"name"`
	} `json:"labels"`
	RequestedReviewers []githubUser `json:"requested_reviewers"`
	Additions          int          `json:"additions"`
	Deletions          int          `json:"deletions"`
	ChangedFiles       int          `json:"changed_files"`
	Commits            int          `json:"commits"`
	Comments           int          `json:"comments"`
	ReviewComments     int          `json:"review_comments"`
	Body               string       `json:"body"`
	CreatedAt          time.Time    `json:"created_at"`
	UpdatedAt          time.Time    `json:"updated_at"`
	MergedAt           *time.Time   `json:"merged_at"`
	ClosedAt           *time.Time   `json:"closed_at"`
	HTMLURL            string       `json:"html_url"`
}

// state is open, closed, merged, or draft (an open draft).
func (g githubPR) state() string {
	switch {
	case g.MergedAt != nil:
		return "merged"
	case g.State == "open" && g.Draft:
		return "draft"
	}
	return g.State
}

// prView is the stable --json schema of ghv pr view.
type prView struct {
	Repo           string     `json:"repo"`
	Number         int        `json:"number"`
	Title          string     `json:"title"`
	State          string     `json:"state"`
	Author         string     `json:"author"`
	Base           string     `json:"base"`
	Head           string     `json:"head"`
	HeadSHA        string     `json:"head_sha"`
	Labels         []string   `json:"labels"`
	Reviewers      []string   `json:"requested_reviewers"`
	Additions      int        `json:"additions"`
	Deletions      int        `json:"deletions"`
	ChangedFiles   int        `json:"changed_files"`
	Commits        int        `json:"commits"`
	Comments       int        `json:"comments"`
	ReviewComments int        `json:"review_comments"`
	CreatedAt      time.Time  `json:"created_at"`
	UpdatedAt      time.Time  `json:"updated_at"`
	MergedAt       *time.Time `json:"merged_at"`
	ClosedAt       *time.Time `json:"closed_at"`
	URL            string     `json:"url"`
	Body           string     `json:"body"`
}

// prSummary is one item of ghv pr list --json.
type prSummary struct {
	Number    int       `json:"number"`
	Title     string    `json:"title"`
	State     string    `json:"state"`
	Author    string    `json:"author"`
	Base      string    `json:"base"`
	Head      string    `json:"head"`
	UpdatedAt time.Time `json:"updated_at"`
	URL       string    `json:"url"`
}

// getPR fetches pull request n of repo.
func (a *app) getPR(ctx context.Context, repo string, n int) (githubPR, error) {
	var g githubPR
	c, err := a.read()
	if err != nil {
		return g, err
	}
	resp, err := c.Get(ctx, fmt.Sprintf("repos/%s/pulls/%d", repo, n), "")
	if err != nil {
		return g, err
	}
	if err := json.Unmarshal(resp.Body, &g); err != nil {
		return g, fmt.Errorf("GitHub's pull request response: %v", err)
	}
	return g, nil
}

// repoAndNumber reads -R (or the checkout's origin) and positional N.
func (a *app) repoAndNumber() (string, int, error) {
	n, err := a.number(0)
	if err != nil {
		return "", 0, err
	}
	repo, err := a.repo()
	return repo, n, err
}

func runPRView(ctx context.Context, a *app) error {
	repo, n, err := a.repoAndNumber()
	if err != nil {
		return err
	}
	g, err := a.getPR(ctx, repo, n)
	if err != nil {
		return err
	}
	v := prView{
		Repo: repo, Number: g.Number, Title: g.Title, State: g.state(), Author: g.User.Login,
		Base: g.Base.Ref, Head: g.Head.Label, HeadSHA: g.Head.SHA, Labels: []string{}, Reviewers: []string{},
		Additions: g.Additions, Deletions: g.Deletions, ChangedFiles: g.ChangedFiles, Commits: g.Commits,
		Comments: g.Comments, ReviewComments: g.ReviewComments, CreatedAt: g.CreatedAt, UpdatedAt: g.UpdatedAt,
		MergedAt: g.MergedAt, ClosedAt: g.ClosedAt, URL: g.HTMLURL, Body: g.Body,
	}
	for _, l := range g.Labels {
		v.Labels = append(v.Labels, l.Name)
	}
	for _, u := range g.RequestedReviewers {
		v.Reviewers = append(v.Reviewers, u.Login)
	}
	if a.json() {
		return writeJSON(a.stdout, v)
	}
	out := a.stdout
	fmt.Fprintf(out, "%s#%d %s\n\n", repo, v.Number, sanitize.Line(v.Title))
	row := rowWriter(out)
	row("state", v.State)
	row("author", v.Author)
	row("base", v.Base)
	row("head", fmt.Sprintf("%s (%s)", v.Head, short(v.HeadSHA)))
	row("labels", strings.Join(v.Labels, ", "))
	row("reviewers", strings.Join(v.Reviewers, ", "))
	row("changes", fmt.Sprintf("+%d -%d, %d file%s, %d commit%s", v.Additions, v.Deletions, v.ChangedFiles, plural(v.ChangedFiles), v.Commits, plural(v.Commits)))
	row("comments", fmt.Sprintf("%d, review comments %d", v.Comments, v.ReviewComments))
	row("created", v.CreatedAt.Format(time.RFC3339))
	row("updated", v.UpdatedAt.Format(time.RFC3339))
	if v.MergedAt != nil {
		row("merged", v.MergedAt.Format(time.RFC3339))
	} else if v.ClosedAt != nil {
		row("closed", v.ClosedAt.Format(time.RFC3339))
	}
	row("url", v.URL)
	writeBody(out, v.Body)
	return nil
}

// rowWriter prints "key  value" rows, skipping empty values.
func rowWriter(out io.Writer) func(k, v string) {
	return func(k, v string) {
		if v != "" {
			fmt.Fprintf(out, "%-12s %s\n", k, sanitize.Line(v))
		}
	}
}

// writeBody prints a Markdown body after a blank line, terminal-safe.
func writeBody(out io.Writer, body string) {
	if body = strings.TrimRight(sanitize.Terminal(body), "\n"); body != "" {
		fmt.Fprintf(out, "\n%s\n", body)
	}
}

func short(sha string) string { return sha[:min(7, len(sha))] }

func (a *app) state() (string, error) {
	s := a.inv.str("state")
	switch s {
	case "open", "closed", "all":
		return s, nil
	}
	return "", usagef("--state must be open, closed, or all")
}

func runPRList(ctx context.Context, a *app) error {
	state, err := a.state()
	if err != nil {
		return err
	}
	repo, err := a.repo()
	if err != nil {
		return err
	}
	c, err := a.read()
	if err != nil {
		return err
	}
	items, err := c.Paginate(ctx, fmt.Sprintf("repos/%s/pulls?state=%s", repo, state), "", a.inv.int("limit"))
	if err != nil {
		return err
	}
	list := []prSummary{}
	for _, raw := range items {
		var g githubPR
		if err := json.Unmarshal(raw, &g); err != nil {
			return fmt.Errorf("GitHub's pull request list: %v", err)
		}
		list = append(list, prSummary{g.Number, g.Title, g.state(), g.User.Login, g.Base.Ref, g.Head.Label, g.UpdatedAt, g.HTMLURL})
	}
	if a.json() {
		return writeJSON(a.stdout, list)
	}
	rows := make([][]string, len(list))
	for i, p := range list {
		rows[i] = []string{fmt.Sprintf("#%d", p.Number), p.State, p.Author, p.Title}
	}
	writeTable(a.stdout, rows)
	return nil
}

// writeTable prints rows in columns two spaces apart, every cell
// sanitised; the last column is not padded.
func writeTable(out io.Writer, rows [][]string) {
	var width []int
	for _, r := range rows {
		for i, cell := range r {
			r[i] = sanitize.Line(cell)
			if i >= len(width) {
				width = append(width, 0)
			}
			width[i] = max(width[i], len([]rune(r[i])))
		}
	}
	for _, r := range rows {
		var b strings.Builder
		for i, cell := range r {
			if i == len(r)-1 {
				b.WriteString(cell)
				break
			}
			b.WriteString(cell + strings.Repeat(" ", width[i]-len([]rune(cell))+2))
		}
		fmt.Fprintln(out, b.String())
	}
}
