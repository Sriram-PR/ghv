package main

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Sriram-PR/ghv/internal/draft"
)

// limitedRoutes answer o/r only to a request with a token: the anonymous
// rate limit is used up, until 12:30 UTC.
func limitedRoutes() map[string]apiReply {
	routes := baseRoutes()
	reset := fmt.Sprint(time.Date(2026, 10, 18, 12, 30, 0, 0, time.UTC).Unix())
	routes["auth /repos/o/r"] = routes["/repos/o/r"]
	routes["/repos/o/r"] = apiReply{status: 403,
		header: http.Header{"X-Ratelimit-Remaining": {"0"}, "X-Ratelimit-Reset": {reset}},
		body:   `{"message":"API rate limit exceeded"}`}
	return routes
}

const askToken = "GitHub's anonymous rate limit is used up (it resets at 12:30 UTC), so some context is missing.\nRead the rest with your write token, by GET requests only? [y/N] \n"

func TestReviewReadsWithTheWriteToken(t *testing.T) {
	ft := answers(t, oldPass, "y", "s")
	e, f := reviewEnv(t, ft, limitedRoutes())
	queueDraft(t, e.home, draft.Draft{Repo: "o/r", CreatedAt: at(9), Fields: draft.IssueCreateFields{Title: "A", Body: ""}})
	if r := runWith(e); r.code != 0 {
		t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
	}
	if !strings.Contains(ft.shown(), askToken+"Context from GitHub:\n  Repository: o/r, created 2019-01-02") ||
		strings.Contains(ft.shown(), "will not send") {
		t.Fatalf("terminal:\n%s", ft.shown())
	}
	asked := false
	for _, r := range f.reqs {
		auth := r.Header.Get("Authorization")
		if r.Method != "GET" {
			t.Fatalf("%s %s", r.Method, r.URL)
		}
		if asked && auth != "Bearer "+testToken {
			t.Fatalf("%s without the token after the reviewer said yes", r.URL)
		}
		if r.URL.Path == "/repos/o/r" && auth == "" {
			asked = true
		}
	}
	if f.count("/repos/o/r") != 2 {
		t.Fatalf("o/r read %d times", f.count("/repos/o/r"))
	}
}

func TestReviewDeclinesTheWriteToken(t *testing.T) {
	ft := answers(t, oldPass, "", "s", "s")
	e, f := reviewEnv(t, ft, limitedRoutes())
	queueDraft(t, e.home, draft.Draft{Repo: "o/r", CreatedAt: at(9), Fields: draft.IssueCreateFields{Title: "A", Body: ""}})
	queueDraft(t, e.home, draft.Draft{Repo: "o/r", CreatedAt: at(10), Fields: draft.IssueCreateFields{Title: "B", Body: ""}})
	if r := runWith(e); r.code != 0 {
		t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
	}
	shown := ft.shown()
	blocked := "  - cannot read o/r from GitHub: GET /repos/o/r: GitHub's anonymous rate limit is used up; it resets at 12:30 UTC\n"
	if strings.Count(shown, "Read the rest with your write token") != 1 || strings.Count(shown, blocked) != 2 {
		t.Fatalf("terminal:\n%s", shown)
	}
	for _, r := range f.reqs[1:] {
		if r.Header.Get("Authorization") != "" {
			t.Fatalf("%s sent with a token", r.URL)
		}
	}
}
