package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"slices"
	"time"

	"github.com/Sriram-PR/ghv/internal/read"
)

const statusLimit = 100

func init() {
	register(&command{
		path: "status", flags: []flag{sinceFlag},
		help: "Show your open pull requests, your review requests, and issues and pull requests involving you updated since T (default 7d); needs a read token",
		run:  runStatus,
	})
}

// statusView is the stable --json schema of ghv status. Each search
// returns at most 100 items; lists are most recently updated first.
type statusView struct {
	Since           time.Time  `json:"since"`
	Authored        []issueHit `json:"authored"`
	ReviewRequested []issueHit `json:"review_requested"`
	Involved        []issueHit `json:"involved"`
}

func runStatus(ctx context.Context, a *app) error {
	since, err := a.since()
	if err != nil {
		return err
	}
	if since.IsZero() {
		since = a.now().AddDate(0, 0, -7).UTC()
	}
	c, err := a.read()
	if err != nil {
		return err
	}
	if !c.Authenticated() {
		return &read.APIError{Status: 401, Code: read.CodeTokenRequired, Message: "ghv status needs a read token, since it searches with @me"}
	}
	v := statusView{Since: since}
	// GitHub's issue search needs is:issue or is:pull-request (is:pr finds
	// nothing), so involvement takes two searches.
	updated := " involves:@me updated:>=" + since.Format(time.RFC3339)
	var issues []issueHit
	for _, s := range []struct {
		q   string
		out *[]issueHit
	}{
		{"is:pull-request is:open author:@me", &v.Authored},
		{"is:pull-request is:open review-requested:@me", &v.ReviewRequested},
		{"is:pull-request" + updated, &v.Involved},
		{"is:issue" + updated, &issues},
	} {
		if *s.out, err = searchIssues(ctx, c, s.q); err != nil {
			return err
		}
	}
	v.Involved = append(v.Involved, issues...)
	slices.SortStableFunc(v.Involved, func(x, y issueHit) int { return y.UpdatedAt.Compare(x.UpdatedAt) })
	if a.json() {
		return writeJSON(a.stdout, v)
	}
	section := func(title string, hits []issueHit, row func(issueHit) []string) {
		fmt.Fprintf(a.stdout, "%s (%d)\n", title, len(hits))
		if len(hits) == 0 {
			fmt.Fprintln(a.stdout, "  none")
			return
		}
		rows := make([][]string, len(hits))
		for i, h := range hits {
			rows[i] = append([]string{fmt.Sprintf("  %s#%d", h.Repo, h.Number)}, row(h)...)
		}
		writeTable(a.stdout, rows)
	}
	day := func(h issueHit) []string { return []string{h.UpdatedAt.UTC().Format(time.DateOnly), h.Title} }
	section("Your open pull requests", v.Authored, day)
	fmt.Fprintln(a.stdout)
	section("Review requests", v.ReviewRequested, day)
	fmt.Fprintln(a.stdout)
	section("Updated since "+since.Format(time.RFC3339)+", involving you", v.Involved, func(h issueHit) []string {
		kind := "issue"
		if h.PullRequest {
			kind = "pr"
		}
		return append([]string{kind, h.State}, day(h)...)
	})
	fmt.Fprintln(a.stdout, "\nRun 'ghv pr activity N -R OWNER/REPO --since T' or 'ghv issue activity ...' for what changed.")
	return nil
}

func searchIssues(ctx context.Context, c *read.Client, q string) ([]issueHit, error) {
	p := "search/issues?" + url.Values{"q": {q}, "sort": {"updated"}, "order": {"desc"}, "per_page": {fmt.Sprint(statusLimit)}}.Encode()
	items, err := c.Paginate(ctx, p, "", statusLimit)
	if err != nil {
		return nil, err
	}
	hits := []issueHit{}
	for _, raw := range items {
		var g githubIssueHit
		if err := json.Unmarshal(raw, &g); err != nil {
			return nil, fmt.Errorf("GitHub's search results: %v", err)
		}
		hits = append(hits, g.hit())
	}
	return hits, nil
}
