package main

import (
	"path/filepath"
	"strings"
	"testing"
)

const runSHA = "983a448d1f0c3d5e7a1b2c4d6e8f0a1b3c5d7e9f"

const runsJSON = `{"total_count":2,"workflow_runs":[
{"id":38920693349,"name":"Lint","display_title":"Fix \u001b[31mred","status":"completed","conclusion":"failure","event":"pull_request",
"head_branch":"fix","head_sha":"` + runSHA + `","run_number":12,"run_attempt":2,"created_at":"2026-10-16T15:02:39Z",
"html_url":"https://github.com/o/r/actions/runs/38920693349"},
{"id":38920693350,"name":"Unit tests","display_title":"Fix red","status":"in_progress","conclusion":null,"event":"push",
"head_branch":"main","head_sha":"` + runSHA + `","run_number":40,"run_attempt":1,"created_at":"2026-10-16T15:03:00Z",
"html_url":"https://github.com/o/r/actions/runs/38920693350"}]}`

func TestRunList(t *testing.T) {
	rt := newRoutes(t, map[string]string{"/repos/o/r/actions/runs?per_page=20": runsJSON})
	e, _, _ := testEnv(t, rt)
	writeFile(t, filepath.Join(e.dir, ".git", "config"), "[remote \"origin\"]\n\turl = https://github.com/o/r\n")
	r := runWith(t, registry, e, "run", "list")
	want := "38920693349  failure      Lint        fix   Fix <U+001B>[31mred\n" +
		"38920693350  in_progress  Unit tests  main  Fix red\n"
	if r.code != 0 || r.stdout != want {
		t.Fatalf("exit %d, stderr %q, stdout:\n%s\nwant:\n%s", r.code, r.stderr, r.stdout, want)
	}
}

func TestRunListCommitAndJSON(t *testing.T) {
	rt := newRoutes(t, map[string]string{"/repos/o/r/actions/runs?head_sha=" + runSHA + "&per_page=5": runsJSON})
	e, _, _ := testEnv(t, rt)
	r := runWith(t, registry, e, "run", "list", "-R", "o/r", "--commit", strings.ToUpper(runSHA), "--limit", "5", "--json")
	for _, s := range []string{`"id": 38920693349`, `"workflow": "Lint"`, `"title": "Fix \u001b[31mred"`, `"state": "failure"`,
		`"status": "completed"`, `"conclusion": "failure"`, `"event": "pull_request"`, `"branch": "fix"`, `"head_sha": "` + runSHA + `"`,
		`"run_number": 12`, `"attempt": 2`, `"created_at": "2026-10-16T15:02:39Z"`, `"state": "in_progress"`, `"conclusion": ""`} {
		if !strings.Contains(r.stdout, s) {
			t.Fatalf("exit %d, stderr %q, missing %s in:\n%s", r.code, r.stderr, s, r.stdout)
		}
	}
}

func TestRunListEmptyIsArray(t *testing.T) {
	rt := newRoutes(t, map[string]string{"/repos/o/r/actions/runs?per_page=20": `{"total_count":0,"workflow_runs":[]}`})
	e, _, _ := testEnv(t, rt)
	r := runWith(t, registry, e, "run", "list", "-R", "o/r", "--json")
	if r.code != 0 || r.stdout != "[]\n" {
		t.Fatalf("exit %d, stdout %q", r.code, r.stdout)
	}
}

func TestRunListNeedsAFullSHA(t *testing.T) {
	rt := newRoutes(t, nil)
	e, _, _ := testEnv(t, rt)
	r := runWith(t, registry, e, "run", "list", "-R", "o/r", "--commit", "983a448")
	if r.code != 2 || !strings.Contains(r.stderr, "full 40-character commit SHA") || len(rt.uris()) != 0 {
		t.Fatalf("exit %d, stderr %q, asked %q", r.code, r.stderr, rt.uris())
	}
}
