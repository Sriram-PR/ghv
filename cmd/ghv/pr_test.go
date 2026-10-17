package main

import (
	"path/filepath"
	"strings"
	"testing"
)

const prJSON = `{"number":7,"title":"Fix \u001b[31mred","state":"closed","draft":false,
"user":{"login":"alice","type":"User"},"base":{"ref":"main"},"head":{"label":"alice:fix","sha":"9e1e1c929f882fd93e1c67ea520a4529352aab9a"},
"labels":[{"name":"bug"}],"requested_reviewers":[{"login":"bob","type":"User"}],
"additions":9,"deletions":6,"changed_files":1,"commits":2,"comments":3,"review_comments":4,
"body":"Line one\r\nline two \u202e","created_at":"2026-10-16T10:00:00Z","updated_at":"2026-10-17T10:00:00Z",
"merged_at":"2026-10-17T09:00:00Z","closed_at":"2026-10-17T09:00:00Z","html_url":"https://github.com/o/r/pull/7"}`

func TestPRView(t *testing.T) {
	rt := newRoutes(t, map[string]string{"/repos/o/r/pulls/7": prJSON})
	e, _, _ := testEnv(t, rt)
	r := runWith(t, registry, e, "pr", "view", "7", "-R", "o/r")
	want := "o/r#7 Fix <U+001B>[31mred\n\n" +
		"state        merged\n" +
		"author       alice\n" +
		"base         main\n" +
		"head         alice:fix (9e1e1c9)\n" +
		"labels       bug\n" +
		"reviewers    bob\n" +
		"changes      +9 -6, 1 file, 2 commits\n" +
		"comments     3, review comments 4\n" +
		"created      2026-10-16T10:00:00Z\n" +
		"updated      2026-10-17T10:00:00Z\n" +
		"merged       2026-10-17T09:00:00Z\n" +
		"url          https://github.com/o/r/pull/7\n" +
		"\nLine one\nline two <U+202E>\n"
	if r.code != 0 || r.stdout != want {
		t.Fatalf("exit %d, stderr %q, stdout:\n%s\nwant:\n%s", r.code, r.stderr, r.stdout, want)
	}
}

func TestPRViewJSON(t *testing.T) {
	rt := newRoutes(t, map[string]string{"/repos/o/r/pulls/7": prJSON})
	e, _, _ := testEnv(t, rt)
	r := runWith(t, registry, e, "pr", "view", "7", "-R", "o/r", "--json")
	for _, s := range []string{`"state": "merged"`, `"head": "alice:fix"`, `"labels": [` + "\n" + `    "bug"`, `"merged_at": "2026-10-17T09:00:00Z"`, `"body": "Line one\r\nline two \u202e"`, `"title": "Fix \u001b[31mred"`} {
		if !strings.Contains(r.stdout, s) {
			t.Fatalf("missing %s in:\n%s", s, r.stdout)
		}
	}
}

func TestPRList(t *testing.T) {
	rt := newRoutes(t, map[string]string{
		"/repos/o/r/pulls?state=all&per_page=2": `[` +
			`{"number":8,"title":"Draft one","state":"open","draft":true,"user":{"login":"carol"},"head":{"label":"carol:x"},"base":{"ref":"main"},"updated_at":"2026-10-18T10:00:00Z","html_url":"u8"},` +
			`{"number":7,"title":"Merged one","state":"closed","user":{"login":"alice"},"head":{"label":"alice:fix"},"base":{"ref":"main"},"merged_at":"2026-10-17T09:00:00Z","updated_at":"2026-10-17T10:00:00Z","html_url":"u7"}]`,
	})
	e, _, _ := testEnv(t, rt)
	writeFile(t, filepath.Join(e.dir, ".git", "config"), "[remote \"origin\"]\n\turl = https://github.com/o/r\n")
	r := runWith(t, registry, e, "pr", "list", "--state", "all", "--limit", "2")
	want := "#8  draft   carol  Draft one\n#7  merged  alice  Merged one\n"
	if r.code != 0 || r.stdout != want {
		t.Fatalf("exit %d, stderr %q, stdout:\n%s", r.code, r.stderr, r.stdout)
	}
	r = runWith(t, registry, e, "pr", "list", "--state", "all", "--limit", "2", "--json")
	if !strings.Contains(r.stdout, `"state": "draft"`) || !strings.Contains(r.stdout, `"number": 7`) {
		t.Fatalf("json:\n%s", r.stdout)
	}
}

func TestPRListEmptyIsArray(t *testing.T) {
	rt := newRoutes(t, map[string]string{"/repos/o/r/pulls?state=open&per_page=100": `[]`})
	e, _, _ := testEnv(t, rt)
	r := runWith(t, registry, e, "pr", "list", "-R", "o/r", "--json")
	if r.code != 0 || r.stdout != "[]\n" {
		t.Fatalf("exit %d, stdout %q, stderr %q", r.code, r.stdout, r.stderr)
	}
}

func TestPRUsageErrors(t *testing.T) {
	tests := []struct {
		args   []string
		stderr string
	}{
		{[]string{"pr", "view", "x", "-R", "o/r"}, `"x" is not a number`},
		{[]string{"pr", "view", "0", "-R", "o/r"}, `"0" is not a number`},
		{[]string{"pr", "view", "-R", "o/r"}, "ghv pr view needs N"},
		{[]string{"pr", "view", "7", "-R", "bad"}, `-R "bad" is not OWNER/REPO`},
		{[]string{"pr", "list", "-R", "o/r", "--state", "merged"}, "--state must be open, closed, or all"},
	}
	for _, tt := range tests {
		t.Run(strings.Join(tt.args, " "), func(t *testing.T) {
			rt := newRoutes(t, nil)
			e, _, _ := testEnv(t, rt)
			r := runWith(t, registry, e, tt.args...)
			if r.code != 2 || len(rt.uris()) != 0 || !strings.Contains(r.stderr, tt.stderr) {
				t.Fatalf("exit %d, sent %q, stderr %q", r.code, rt.uris(), r.stderr)
			}
		})
	}
}

func TestNoRepoMessageNamesNoUnknownFlag(t *testing.T) {
	e, _, _ := testEnv(t, nil)
	r := runWith(t, registry, e, "repo", "view")
	if r.code != 2 || strings.Contains(r.stderr, "-R") {
		t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
	}
	r = runWith(t, registry, e, "pr", "list")
	if r.code != 2 || !strings.Contains(r.stderr, "-R OWNER/REPO") {
		t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
	}
}
