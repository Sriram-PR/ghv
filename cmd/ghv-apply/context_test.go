package main

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Sriram-PR/ghv/internal/draft"
	"github.com/Sriram-PR/ghv/internal/fetch"
	"github.com/Sriram-PR/ghv/internal/harden"
)

type apiReply struct {
	status int
	header http.Header
	body   string
}

// fakeAPI answers by request URI, prefixed "diff " for a diff; anything
// else is a 404.
type fakeAPI struct {
	routes map[string]apiReply
	reqs   []*http.Request
}

func (f *fakeAPI) RoundTrip(r *http.Request) (*http.Response, error) {
	f.reqs = append(f.reqs, r)
	key := r.URL.RequestURI()
	if r.Header.Get("Accept") == "application/vnd.github.diff" {
		key = "diff " + key
	}
	rp, ok := f.routes[key]
	if !ok {
		rp = apiReply{status: 404, body: `{"message":"Not Found"}`}
	}
	h := rp.header
	if h == nil {
		h = http.Header{}
	}
	return &http.Response{StatusCode: rp.status, Header: h, Body: io.NopCloser(strings.NewReader(rp.body)), Request: r}, nil
}

func (f *fakeAPI) count(uri string) int {
	n := 0
	for _, r := range f.reqs {
		if r.URL.RequestURI() == uri {
			n++
		}
	}
	return n
}

func ok(body string) apiReply { return apiReply{status: 200, body: body} }

var reviewNow = time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)

// baseRoutes describe o/r, an old repository of an old organization the
// user has never taken part in, and a user who owns Sriram-PR/ghv.
func baseRoutes() map[string]apiReply {
	return map[string]apiReply{
		"/user":      {status: 200, header: http.Header{"X-Oauth-Scopes": {"public_repo"}}, body: `{"login":"Sriram-PR"}`},
		"/repos/o/r": ok(`{"full_name":"o/r","created_at":"2019-01-02T00:00:00Z","owner":{"login":"o","type":"Organization"}}`),
		"/users/o":   ok(`{"login":"o","type":"Organization","created_at":"2015-03-04T00:00:00Z"}`),
		"/search/issues?q=repo%3Ao%2Fr+involves%3ASriram-PR&per_page=1": ok(`{"total_count":0}`),
		"/users/Sriram-PR/repos?type=owner&per_page=100":                ok(`[{"full_name":"Sriram-PR/ghv"}]`),
		"/users/Sriram-PR/events/public?per_page=100":                   ok(`[]`),
	}
}

func newReviewer(routes map[string]apiReply) (*reviewer, *fakeAPI) {
	f := &fakeAPI{routes: routes}
	return &reviewer{fc: fetch.New(harden.Client(f)), login: "Sriram-PR", now: reviewNow}, f
}

func contextOf(t *testing.T, routes map[string]apiReply, d draft.Draft) reviewContext {
	t.Helper()
	rv, _ := newReviewer(routes)
	return rv.context(context.Background(), d)
}

func has(t *testing.T, what string, got []string, want ...string) {
	t.Helper()
	all := strings.Join(got, "\n")
	for _, w := range want {
		if !strings.Contains(all, w) {
			t.Errorf("%s lacks %q:\n%s", what, w, all)
		}
	}
}

func issueDraft(repo string) draft.Draft {
	return draft.Draft{Repo: repo, Fields: draft.IssueCreateFields{Title: "t", Body: "b"}}
}

func TestContextRepository(t *testing.T) {
	rc := contextOf(t, baseRoutes(), issueDraft("o/r"))
	has(t, "lines", rc.Lines,
		"Repository: o/r, created 2019-01-02, 7 years ago",
		"Owner: o (Organization), on GitHub since 2015-03-04, 11 years ago")
	has(t, "flags", rc.Flags, "you have not taken part in o/r before")
	if len(rc.Flags) != 1 || len(rc.Blocks) != 0 {
		t.Fatalf("flags %q, blocks %q", rc.Flags, rc.Blocks)
	}
}

func TestContextYourOwnRepository(t *testing.T) {
	routes := baseRoutes()
	// GitHub matches names without regard to case.
	routes["/repos/sriram-pr/ghv"] = ok(`{"full_name":"Sriram-PR/ghv","created_at":"2026-10-16T00:00:00Z","owner":{"login":"Sriram-PR","type":"User"}}`)
	routes["/users/Sriram-PR"] = ok(`{"login":"Sriram-PR","type":"User","created_at":"2018-01-01T00:00:00Z"}`)
	rv, f := newReviewer(routes)
	rc := rv.context(context.Background(), issueDraft("sriram-pr/ghv"))
	has(t, "lines", rc.Lines, "Sriram-PR/ghv is yours.")
	has(t, "flags", rc.Flags, "Sriram-PR/ghv was created 2 days ago")
	if len(rc.Flags) != 1 || len(rc.Blocks) != 0 {
		t.Fatalf("a case-only difference is not a rename, and the user's own repo is no lookalike: flags %q, blocks %q", rc.Flags, rc.Blocks)
	}
	for _, r := range f.reqs {
		if strings.HasPrefix(r.URL.Path, "/search/") {
			t.Fatalf("searched for the user's history in their own repo")
		}
	}
}

func TestContextRename(t *testing.T) {
	routes := baseRoutes()
	routes["/repos/docker/docker"] = apiReply{status: 301, header: http.Header{"Location": {"https://api.github.com/repositories/7691631"}}}
	routes["/repositories/7691631"] = ok(`{"full_name":"o/r","created_at":"2019-01-02T00:00:00Z","owner":{"login":"o","type":"Organization"}}`)
	rc := contextOf(t, routes, issueDraft("docker/docker"))
	has(t, "flags", rc.Flags, "GitHub knows docker/docker as o/r (renamed or transferred); ghv-apply sends to o/r")
}

func TestContextLookalike(t *testing.T) {
	routes := baseRoutes()
	routes["/repos/Srlram-PR/ghv"] = ok(`{"full_name":"Srlram-PR/ghv","created_at":"2026-10-15T00:00:00Z","owner":{"login":"Srlram-PR","type":"User"}}`)
	routes["/users/Srlram-PR"] = ok(`{"login":"Srlram-PR","type":"User","created_at":"2026-10-15T00:00:00Z"}`)
	routes["/search/issues?q=repo%3ASrlram-PR%2Fghv+involves%3ASriram-PR&per_page=1"] = ok(`{"total_count":0}`)
	rc := contextOf(t, routes, issueDraft("Srlram-PR/ghv"))
	has(t, "flags", rc.Flags,
		"Srlram-PR/ghv looks like Sriram-PR/ghv, a repository you know",
		"Srlram-PR/ghv was created 3 days ago",
		"Srlram-PR joined GitHub 3 days ago",
		"you have not taken part in Srlram-PR/ghv before")
}

func TestContextKnownReposFetchedOnce(t *testing.T) {
	rv, f := newReviewer(baseRoutes())
	rv.context(context.Background(), issueDraft("o/r"))
	rv.context(context.Background(), issueDraft("o/r"))
	if f.count("/users/Sriram-PR/repos?type=owner&per_page=100") != 1 || f.count("/users/Sriram-PR/events/public?per_page=100") != 1 {
		t.Fatalf("requests: %d", len(f.reqs))
	}
}

func TestContextUnavailable(t *testing.T) {
	rc := contextOf(t, baseRoutes(), issueDraft("o/missing"))
	has(t, "blocks", rc.Blocks, "GitHub has no public repository o/missing")

	limited := apiReply{status: 403, header: http.Header{"X-Ratelimit-Remaining": {"0"}}}
	routes := baseRoutes()
	routes["/repos/o/r"] = limited
	rc = contextOf(t, routes, issueDraft("o/r"))
	has(t, "blocks", rc.Blocks, "cannot read o/r from GitHub: GET /repos/o/r: GitHub's anonymous rate limit is used up")

	routes = baseRoutes()
	routes["/users/o"] = limited
	routes["/search/issues?q=repo%3Ao%2Fr+involves%3ASriram-PR&per_page=1"] = limited
	routes["/users/Sriram-PR/repos?type=owner&per_page=100"] = limited
	rc = contextOf(t, routes, issueDraft("o/r"))
	has(t, "lines", rc.Lines, "Owner: o (unavailable: ", "Your history with o/r: unavailable: ", "Lookalike check: unavailable: ")
	if len(rc.Blocks) != 0 {
		t.Fatalf("blocks %q", rc.Blocks)
	}
}

func TestContextComment(t *testing.T) {
	routes := baseRoutes()
	routes["/repos/o/r/issues/7"] = ok(`{"number":7,"title":"Crash \u001b[31m","state":"closed","user":{"login":"u"},"pull_request":{"url":"x"}}`)
	rc := contextOf(t, routes, draft.Draft{Repo: "o/r", Fields: draft.CommentFields{Number: 7, Body: "b"}})
	has(t, "lines", rc.Lines, "Comment on pull request #7: Crash <U+001B>[31m (closed), opened by u")
	has(t, "flags", rc.Flags, "pull request #7 is closed")

	rc = contextOf(t, baseRoutes(), draft.Draft{Repo: "o/r", Fields: draft.CommentFields{Number: 8, Body: "b"}})
	has(t, "blocks", rc.Blocks, "o/r has no issue or pull request #8")
}

func TestContextFork(t *testing.T) {
	fork := draft.Draft{Repo: "o/r", Fields: draft.ForkFields{}}
	rc := contextOf(t, baseRoutes(), fork)
	has(t, "lines", rc.Lines, "You have no repository named r, so the fork will be Sriram-PR/r.")

	routes := baseRoutes()
	routes["/repos/Sriram-PR/r"] = ok(`{"full_name":"Sriram-PR/r","fork":true,"parent":{"full_name":"o/r"}}`)
	rc = contextOf(t, routes, fork)
	has(t, "flags", rc.Flags, "you already have this fork, Sriram-PR/r; GitHub returns it rather than making another")

	routes["/repos/Sriram-PR/r"] = ok(`{"full_name":"Sriram-PR/r","fork":false}`)
	rc = contextOf(t, routes, fork)
	has(t, "flags", rc.Flags, "you already have Sriram-PR/r; GitHub would name the fork r-1")

	routes = baseRoutes()
	routes["/repos/Sriram-PR/ghv"] = ok(`{"full_name":"Sriram-PR/ghv","created_at":"2020-01-01T00:00:00Z","owner":{"login":"Sriram-PR","type":"User"}}`)
	rc = contextOf(t, routes, draft.Draft{Repo: "Sriram-PR/ghv", Fields: draft.ForkFields{}})
	has(t, "blocks", rc.Blocks, "Sriram-PR/ghv is yours; GitHub cannot fork a repository into the account that owns it")
}

func TestDistance(t *testing.T) {
	for _, tt := range []struct {
		a, b string
		want int
	}{
		{"", "", 0}, {"abc", "abc", 0}, {"abc", "abd", 1}, {"abc", "ab", 1},
		{"sriram-pr/ghv", "srlram-pr/ghv", 1}, {"cli/cli", "c1i/cl1", 2}, {"kitten", "sitting", 3},
	} {
		if got := distance(tt.a, tt.b); got != tt.want {
			t.Errorf("distance(%q, %q) = %d, want %d", tt.a, tt.b, got, tt.want)
		}
	}
}

func TestPlural(t *testing.T) {
	for n, want := range map[int]string{0: "0 commits", 1: "1 commit", 2: "2 commits"} {
		if got := plural(n, "commit", "commits"); got != want {
			t.Errorf("plural(%d) = %q, want %q", n, got, want)
		}
	}
	if got := ago(reviewNow.Add(-36*time.Hour), reviewNow); got != "1 day ago" {
		t.Errorf("ago = %q", got)
	}
}
