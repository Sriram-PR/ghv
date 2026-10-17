package main

import (
	"strings"
	"testing"
)

const (
	authoredQ = "/search/issues?order=desc&per_page=100&q=is%3Apull-request+is%3Aopen+author%3A%40me&sort=updated"
	reviewQ   = "/search/issues?order=desc&per_page=100&q=is%3Apull-request+is%3Aopen+review-requested%3A%40me&sort=updated"
)

// involvedQ is the involves:@me search for kind (issue or pull-request).
func involvedQ(kind, since string) string {
	return "/search/issues?order=desc&per_page=100&q=is%3A" + kind + "+involves%3A%40me+updated%3A%3E%3D" + since + "&sort=updated"
}

func statusRoutes(t *testing.T, since string) *routes {
	return newRoutes(t, map[string]string{
		authoredQ: `{"total_count":1,"items":[{"repository_url":"https://api.github.com/repos/o/r","number":7,"title":"My fix","state":"open","user":{"login":"me"},"pull_request":{},"updated_at":"2026-10-18T09:00:00Z","html_url":"u7"}]}`,
		reviewQ:   `{"total_count":0,"items":[]}`,
		involvedQ("pull-request", since): `{"total_count":2,"items":[` +
			`{"repository_url":"https://api.github.com/repos/o/r","number":7,"title":"My fix","state":"open","user":{"login":"me"},"pull_request":{},"updated_at":"2026-10-18T09:00:00Z","html_url":"u7"},` +
			`{"repository_url":"https://api.github.com/repos/o/r","number":2,"title":"Old PR","state":"closed","user":{"login":"me"},"pull_request":{},"updated_at":"2026-10-12T09:00:00Z","html_url":"u2"}]}`,
		involvedQ("issue", since): `{"total_count":1,"items":[` +
			`{"repository_url":"https://api.github.com/repos/up/x","number":3,"title":"Bug \u202e","state":"closed","user":{"login":"bo"},"updated_at":"2026-10-17T09:00:00Z","html_url":"u3"}]}`,
	})
}

func TestStatus(t *testing.T) {
	rt := statusRoutes(t, "2026-10-11T12%3A00%3A00Z")
	e, _, _ := testEnv(t, rt)
	setToken(t, e, "ghp_x")
	r := runWith(t, registry, e, "status")
	want := "Your open pull requests (1)\n" +
		"  o/r#7  2026-10-18  My fix\n" +
		"\nReview requests (0)\n" +
		"  none\n" +
		"\nUpdated since 2026-10-11T12:00:00Z, involving you (3)\n" +
		"  o/r#7   pr     open    2026-10-18  My fix\n" +
		"  up/x#3  issue  closed  2026-10-17  Bug <U+202E>\n" +
		"  o/r#2   pr     closed  2026-10-12  Old PR\n" +
		"\nRun 'ghv pr activity N -R OWNER/REPO --since T' or 'ghv issue activity ...' for what changed.\n"
	if r.code != 0 || r.stdout != want {
		t.Fatalf("exit %d, stderr %q, asked %q, stdout:\n%s\nwant:\n%s", r.code, r.stderr, rt.uris(), r.stdout, want)
	}
}

func TestStatusSinceAndJSON(t *testing.T) {
	rt := statusRoutes(t, "2026-10-06T07%3A00%3A00Z")
	e, _, _ := testEnv(t, rt)
	setToken(t, e, "ghp_x")
	r := runWith(t, registry, e, "status", "--since", "2026-10-06T07:00:00Z", "--json")
	for _, s := range []string{`"since": "2026-10-06T07:00:00Z"`, `"authored": [`, `"review_requested": []`, `"repo": "up/x"`} {
		if r.code != 0 || !strings.Contains(r.stdout, s) {
			t.Fatalf("exit %d, stderr %q, missing %s in:\n%s", r.code, r.stderr, s, r.stdout)
		}
	}
}

func TestStatusNeedsAToken(t *testing.T) {
	rt := newRoutes(t, nil)
	e, _, _ := testEnv(t, rt)
	r := runWith(t, registry, e, "status")
	if r.code != 3 || len(rt.uris()) != 0 || !strings.Contains(r.stderr, "ghv status needs a read token") {
		t.Fatalf("exit %d, asked %q, stderr %q", r.code, rt.uris(), r.stderr)
	}
}
