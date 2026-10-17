package main

import (
	"strings"
	"testing"
)

func TestSearchRepos(t *testing.T) {
	rt := newRoutes(t, map[string]string{
		"/search/repositories?q=go+cli&per_page=100": `{"total_count":1,"incomplete_results":false,"items":[` +
			`{"full_name":"cli/cli","description":"GitHub CLI \u202e","stargazers_count":38000,"language":"Go","updated_at":"2026-10-18T10:00:00Z","html_url":"https://github.com/cli/cli"}]}`,
	})
	e, _, _ := testEnv(t, rt)
	r := runWith(t, registry, e, "search", "repos", "go cli")
	if r.code != 0 || r.stdout != "cli/cli  38000  Go  GitHub CLI <U+202E>\n" {
		t.Fatalf("exit %d, stderr %q, asked %q, stdout %q", r.code, r.stderr, rt.uris(), r.stdout)
	}
	e, _, _ = testEnv(t, rt)
	r = runWith(t, registry, e, "search", "repos", "go cli", "--json")
	if !strings.HasPrefix(r.stdout, "[\n") || !strings.Contains(r.stdout, `"stars": 38000`) {
		t.Fatalf("json:\n%s", r.stdout)
	}
}

const issueHits = `{"total_count":2,"incomplete_results":false,"items":[` +
	`{"repository_url":"https://api.github.com/repos/o/r","number":5,"title":"Crash","state":"open","user":{"login":"dave"},"updated_at":"2026-10-18T10:00:00Z","html_url":"u5"},` +
	`{"repository_url":"https://api.github.com/repos/up/x","number":9,"title":"Fix crash","state":"closed","user":{"login":"erin"},"pull_request":{"merged_at":"2026-10-17T10:00:00Z"},"updated_at":"2026-10-17T10:00:00Z","html_url":"u9"}]}`

func TestSearchIssuesAndPRsAddTheirQualifier(t *testing.T) {
	rt := newRoutes(t, map[string]string{
		"/search/issues?q=crash+is%3Aissue&per_page=5":        issueHits,
		"/search/issues?q=crash+is%3Apull-request&per_page=5": issueHits,
	})
	e, _, _ := testEnv(t, rt)
	r := runWith(t, registry, e, "search", "issues", "crash", "--limit", "5")
	want := "o/r#5   open    dave  Crash\nup/x#9  merged  erin  Fix crash\n"
	if r.code != 0 || r.stdout != want {
		t.Fatalf("exit %d, stderr %q, asked %q, stdout:\n%s", r.code, r.stderr, rt.uris(), r.stdout)
	}
	r = runWith(t, registry, e, "search", "prs", "crash", "--limit", "5", "--json")
	if r.code != 0 || !strings.Contains(r.stdout, `"repo": "up/x"`) || !strings.Contains(r.stdout, `"pull_request": true`) {
		t.Fatalf("exit %d, stderr %q, json:\n%s", r.code, r.stderr, r.stdout)
	}
}

func TestSearchCode(t *testing.T) {
	rt := newRoutes(t, map[string]string{
		"/search/code?q=Paginate+repo%3Ao%2Fr&per_page=100": `{"total_count":1,"incomplete_results":false,"items":[` +
			`{"name":"paginate.go","path":"internal/read/paginate.go","sha":"abc","html_url":"u","repository":{"full_name":"o/r"}}]}`,
	})
	e, _, _ := testEnv(t, rt)
	setToken(t, e, "ghp_x")
	r := runWith(t, registry, e, "search", "code", "Paginate repo:o/r")
	if r.code != 0 || r.stdout != "o/r  internal/read/paginate.go\n" {
		t.Fatalf("exit %d, stderr %q, asked %q, stdout %q", r.code, r.stderr, rt.uris(), r.stdout)
	}
}

func TestSearchCodeWithoutTokenSendsNothing(t *testing.T) {
	rt := newRoutes(t, nil)
	e, _, _ := testEnv(t, rt)
	r := runWith(t, registry, e, "search", "code", "x", "--json")
	if r.code != 3 || len(rt.uris()) != 0 || !strings.Contains(r.stderr, `"code": "token_required"`) {
		t.Fatalf("exit %d, asked %q, stderr %q", r.code, rt.uris(), r.stderr)
	}
}

func TestSearchNeedsAQuery(t *testing.T) {
	e, _, _ := testEnv(t, nil)
	r := runWith(t, registry, e, "search", "repos")
	if r.code != 2 || !strings.Contains(r.stderr, "ghv search repos needs QUERY") {
		t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
	}
	r = runWith(t, registry, e, "search", "repos", " ")
	if r.code != 2 || !strings.Contains(r.stderr, "the query is empty") {
		t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
	}
}
