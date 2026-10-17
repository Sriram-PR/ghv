package main

import (
	"strings"
	"testing"
)

const issueJSON = `{"number":5,"title":"Crash on \u202estart","state":"closed","state_reason":"completed",
"user":{"login":"dave","type":"User"},"labels":[{"name":"bug"},{"name":"p1"}],"assignees":[{"login":"erin"}],
"comments":2,"body":"Steps\n\u001b[2Jhere","created_at":"2026-10-01T10:00:00Z","updated_at":"2026-10-02T10:00:00Z",
"closed_at":"2026-10-02T09:00:00Z","html_url":"https://github.com/o/r/issues/5"}`

func TestIssueView(t *testing.T) {
	rt := newRoutes(t, map[string]string{"/repos/o/r/issues/5": issueJSON})
	e, _, _ := testEnv(t, rt)
	r := runWith(t, registry, e, "issue", "view", "5", "-R", "o/r")
	want := "o/r#5 Crash on <U+202E>start\n\n" +
		"state        closed (completed)\n" +
		"author       dave\n" +
		"labels       bug, p1\n" +
		"assignees    erin\n" +
		"comments     2\n" +
		"created      2026-10-01T10:00:00Z\n" +
		"updated      2026-10-02T10:00:00Z\n" +
		"closed       2026-10-02T09:00:00Z\n" +
		"url          https://github.com/o/r/issues/5\n" +
		"\nSteps\n<U+001B>[2Jhere\n"
	if r.code != 0 || r.stdout != want {
		t.Fatalf("exit %d, stderr %q, stdout:\n%s\nwant:\n%s", r.code, r.stderr, r.stdout, want)
	}
	r = runWith(t, registry, e, "issue", "view", "5", "-R", "o/r", "--json")
	for _, s := range []string{`"state_reason": "completed"`, `"assignees": [` + "\n" + `    "erin"`, `"title": "Crash on \u202estart"`} {
		if !strings.Contains(r.stdout, s) {
			t.Fatalf("missing %s in:\n%s", s, r.stdout)
		}
	}
}

func TestIssueViewOfAPullRequest(t *testing.T) {
	rt := newRoutes(t, map[string]string{"/repos/o/r/issues/7": `{"number":7,"title":"x","state":"open","pull_request":{"url":"u"}}`})
	e, _, _ := testEnv(t, rt)
	r := runWith(t, registry, e, "issue", "view", "7", "-R", "o/r")
	if r.code != 1 || r.stdout != "" || !strings.Contains(r.stderr, "o/r#7 is a pull request; use ghv pr view 7") {
		t.Fatalf("exit %d, stdout %q, stderr %q", r.code, r.stdout, r.stderr)
	}
}

func TestIssueList(t *testing.T) {
	rt := newRoutes(t, map[string]string{
		"/repos/o/r/issues?labels=bug%2Cp1&since=2026-10-11T12%3A00%3A00Z&state=all&per_page=100": `[` +
			`{"number":9,"title":"Open one","state":"open","user":{"login":"dave"},"labels":[{"name":"bug"}],"comments":1,"updated_at":"2026-10-18T10:00:00Z","html_url":"u9"},` +
			`{"number":8,"title":"A PR","state":"open","user":{"login":"x"},"pull_request":{"url":"u"},"updated_at":"2026-10-18T09:00:00Z"},` +
			`{"number":5,"title":"Closed one","state":"closed","user":{"login":"erin"},"labels":[],"comments":0,"updated_at":"2026-10-17T10:00:00Z","html_url":"u5"}]`,
	})
	e, _, _ := testEnv(t, rt)
	r := runWith(t, registry, e, "issue", "list", "-R", "o/r", "--state", "all", "--label", "bug", "--label", "p1", "--since", "7d")
	want := "#9  open    dave  Open one\n#5  closed  erin  Closed one\n"
	if r.code != 0 || r.stdout != want {
		t.Fatalf("exit %d, stderr %q, asked %q, stdout:\n%s", r.code, r.stderr, rt.uris(), r.stdout)
	}
	r = runWith(t, registry, e, "issue", "list", "-R", "o/r", "--state", "all", "--label", "bug", "--label", "p1", "--since", "7d", "--limit", "1", "--json")
	if !strings.Contains(r.stdout, `"number": 9`) || strings.Contains(r.stdout, `"number": 5`) || strings.Contains(r.stdout, `"number": 8`) {
		t.Fatalf("json:\n%s", r.stdout)
	}
}

func TestIssueListBadSince(t *testing.T) {
	rt := newRoutes(t, nil)
	e, _, _ := testEnv(t, rt)
	r := runWith(t, registry, e, "issue", "list", "-R", "o/r", "--since", "soon")
	if r.code != 2 || len(rt.uris()) != 0 || !strings.Contains(r.stderr, `--since "soon"`) {
		t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
	}
}
