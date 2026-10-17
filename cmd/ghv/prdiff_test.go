package main

import (
	"net/http"
	"strings"
	"testing"
)

func TestPRDiff(t *testing.T) {
	var accept string
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		accept = r.Header.Get("Accept")
		w.Write([]byte("diff --git a/x b/x\n+evil \x1b]0;title\x07\n"))
	})
	e, _, _ := testEnv(t, h)
	r := runWith(t, registry, e, "pr", "diff", "7", "-R", "o/r")
	if r.code != 0 || r.stdout != "diff --git a/x b/x\n+evil <U+001B>]0;title<U+0007>\n" {
		t.Fatalf("exit %d, stdout %q, stderr %q", r.code, r.stdout, r.stderr)
	}
	if accept != "application/vnd.github.diff" {
		t.Fatalf("Accept %q", accept)
	}
	e, _, _ = testEnv(t, h)
	r = runWith(t, registry, e, "pr", "diff", "7", "-R", "o/r", "--json")
	if r.code != 0 || !strings.Contains(r.stdout, `"diff": "diff --git a/x b/x\n+evil \u001b]0;title\u0007\n"`) {
		t.Fatalf("json %s", r.stdout)
	}
}

func TestPRCommits(t *testing.T) {
	rt := newRoutes(t, map[string]string{
		"/repos/o/r/pulls/7/commits?per_page=100": `[` +
			`{"sha":"4536b2180ef7444227b41d4b37b80ffe80912655","author":{"login":"alice"},"commit":{"author":{"name":"Alice A","date":"2026-10-16T10:38:27Z"},"message":"Clarify the flag\n\nLonger text"}},` +
			`{"sha":"0123456789abcdef0123456789abcdef01234567","author":null,"commit":{"author":{"name":"Nobody","date":"2026-10-16T11:00:00Z"},"message":"Second"}}]`,
	})
	e, _, _ := testEnv(t, rt)
	r := runWith(t, registry, e, "pr", "commits", "7", "-R", "o/r")
	want := "4536b21  2026-10-16T10:38:27Z  alice   Clarify the flag\n" +
		"0123456  2026-10-16T11:00:00Z  Nobody  Second\n"
	if r.code != 0 || r.stdout != want {
		t.Fatalf("exit %d, stderr %q, stdout:\n%s", r.code, r.stderr, r.stdout)
	}
	r = runWith(t, registry, e, "pr", "commits", "7", "-R", "o/r", "--json")
	if !strings.Contains(r.stdout, `"message": "Clarify the flag\n\nLonger text"`) || !strings.Contains(r.stdout, `"author": "Nobody"`) {
		t.Fatalf("json:\n%s", r.stdout)
	}
}
