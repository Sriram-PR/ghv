package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"strings"
	"time"

	"github.com/Sriram-PR/ghv/internal/read"
)

func init() {
	for _, s := range []struct{ kind, help string }{
		{"repos", "Search repositories"},
		{"issues", "Search issues; ghv adds is:issue to the query"},
		{"prs", "Search pull requests; ghv adds is:pull-request to the query"},
		{"code", "Search code; needs a read token"},
	} {
		register(&command{
			path: "search " + s.kind, args: []string{"QUERY"}, flags: []flag{limitFlag},
			help: s.help + ". GitHub returns at most 1,000 results; a rate limit that lifts within a minute is waited out",
			run:  func(ctx context.Context, a *app) error { return runSearch(ctx, a, s.kind) },
		})
	}
}

// repoHit, issueHit, and codeHit are the items of ghv search --json.
type repoHit struct {
	FullName    string    `json:"full_name"`
	Description string    `json:"description"`
	Stars       int       `json:"stars"`
	Language    string    `json:"language"`
	UpdatedAt   time.Time `json:"updated_at"`
	URL         string    `json:"url"`
}

type issueHit struct {
	Repo        string    `json:"repo"`
	Number      int       `json:"number"`
	Title       string    `json:"title"`
	State       string    `json:"state"`
	Author      string    `json:"author"`
	PullRequest bool      `json:"pull_request"`
	UpdatedAt   time.Time `json:"updated_at"`
	URL         string    `json:"url"`
}

type codeHit struct {
	Repo string `json:"repo"`
	Path string `json:"path"`
	SHA  string `json:"sha"`
	URL  string `json:"url"`
}

var searchPaths = map[string]struct{ path, qualifier string }{
	"repos":  {"search/repositories", ""},
	"issues": {"search/issues", "is:issue"},
	"prs":    {"search/issues", "is:pull-request"},
	"code":   {"search/code", ""},
}

func runSearch(ctx context.Context, a *app, kind string) error {
	q := strings.TrimSpace(a.inv.args[0])
	if q == "" {
		return usagef("the query is empty")
	}
	c, err := a.read()
	if err != nil {
		return err
	}
	if kind == "code" && !c.Authenticated() {
		return &read.APIError{Status: 401, Code: read.CodeTokenRequired, Message: "code search needs a read token"}
	}
	sp := searchPaths[kind]
	if sp.qualifier != "" {
		q += " " + sp.qualifier
	}
	items, err := c.Paginate(ctx, sp.path+"?"+url.Values{"q": {q}}.Encode(), "", a.inv.int("limit"))
	if err != nil {
		return err
	}
	switch kind {
	case "repos":
		return printHits(a, items, func(g struct {
			FullName        string    `json:"full_name"`
			Description     string    `json:"description"`
			StargazersCount int       `json:"stargazers_count"`
			Language        string    `json:"language"`
			UpdatedAt       time.Time `json:"updated_at"`
			HTMLURL         string    `json:"html_url"`
		}) (repoHit, []string) {
			h := repoHit{g.FullName, g.Description, g.StargazersCount, g.Language, g.UpdatedAt, g.HTMLURL}
			return h, []string{h.FullName, fmt.Sprint(h.Stars), h.Language, h.Description}
		})
	case "code":
		return printHits(a, items, func(g struct {
			Path       string `json:"path"`
			SHA        string `json:"sha"`
			HTMLURL    string `json:"html_url"`
			Repository struct {
				FullName string `json:"full_name"`
			} `json:"repository"`
		}) (codeHit, []string) {
			h := codeHit{g.Repository.FullName, g.Path, g.SHA, g.HTMLURL}
			return h, []string{h.Repo, h.Path}
		})
	}
	return printHits(a, items, func(g githubIssueHit) (issueHit, []string) {
		h := g.hit()
		return h, []string{fmt.Sprintf("%s#%d", h.Repo, h.Number), h.State, h.Author, h.Title}
	})
}

type githubIssueHit struct {
	RepositoryURL string     `json:"repository_url"`
	Number        int        `json:"number"`
	Title         string     `json:"title"`
	State         string     `json:"state"`
	User          githubUser `json:"user"`
	PullRequest   *struct {
		MergedAt *time.Time `json:"merged_at"`
	} `json:"pull_request"`
	UpdatedAt time.Time `json:"updated_at"`
	HTMLURL   string    `json:"html_url"`
}

// hit converts a search item; State is merged for a merged pull request.
func (g githubIssueHit) hit() issueHit {
	h := issueHit{
		Repo: strings.TrimPrefix(g.RepositoryURL, "https://api.github.com/repos/"), Number: g.Number,
		Title: g.Title, State: g.State, Author: g.User.Login, PullRequest: g.PullRequest != nil,
		UpdatedAt: g.UpdatedAt, URL: g.HTMLURL,
	}
	if g.PullRequest != nil && g.PullRequest.MergedAt != nil {
		h.State = "merged"
	}
	return h
}

// printHits decodes each search item as G, converts it, and prints the
// results as a JSON array or a table.
func printHits[G, H any](a *app, items []json.RawMessage, conv func(G) (H, []string)) error {
	hits := []H{}
	var rows [][]string
	for _, raw := range items {
		var g G
		if err := json.Unmarshal(raw, &g); err != nil {
			return fmt.Errorf("GitHub's search results: %v", err)
		}
		h, row := conv(g)
		hits = append(hits, h)
		rows = append(rows, row)
	}
	if a.json() {
		return writeJSON(a.stdout, hits)
	}
	writeTable(a.stdout, rows)
	return nil
}
