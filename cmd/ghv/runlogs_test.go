package main

import (
	"strings"
	"testing"
)

const (
	runLogsURL = "https://results-receiver.actions.githubusercontent.com/rest/runs/x/logs"
	jobLogURL  = "https://productionresultssa1.blob.core.windows.net/actions-results/x/job-logs-"
)

var bom = string([]byte{0xef, 0xbb, 0xbf})

func logsHub(t *testing.T) *fakeHub {
	h := newHub(t)
	h.redirect["/repos/o/r/actions/runs/38920693349/logs"] = runLogsURL + "?sig=s"
	h.files[runLogsURL] = zipBytes(t,
		"1_test.txt", bom+"\x1b[31mFAIL\n",
		"test/system.txt", "runner",
		"10_e2e.txt", "e2e ok",
		"0_lint.txt", bom+"lint ok\n",
		"lint/system.txt", "runner",
	)
	h.api["/repos/o/r/actions/runs/38920693349/jobs?filter=latest&per_page=100"] = `{"total_count":4,"jobs":[
{"id":71,"name":"test","status":"completed","conclusion":"failure"},
{"id":72,"name":"lint","status":"completed","conclusion":"success"},
{"id":73,"name":"e2e","status":"in_progress","conclusion":null},
{"id":74,"name":"slow","status":"completed","conclusion":"timed_out"}]}`
	for _, id := range []string{"71", "74"} {
		h.redirect["/repos/o/r/actions/jobs/"+id+"/logs"] = jobLogURL + id + ".txt?sig=s"
		h.files[jobLogURL+id+".txt"] = []byte(bom + "job " + id + " log\n")
	}
	return h
}

func TestRunLogs(t *testing.T) {
	h := logsHub(t)
	e, _, _ := testEnv(t, h)
	setToken(t, e, "ghp_x")
	r := runWith(t, registry, e, "run", "logs", "38920693349", "-R", "o/r")
	want := "== lint ==\nlint ok\n== test ==\n<U+001B>[31mFAIL\n== e2e ==\ne2e ok\n"
	if r.code != 0 || r.stdout != want {
		t.Fatalf("exit %d, stderr %q, stdout:\n%s\nwant:\n%s", r.code, r.stderr, r.stdout, want)
	}
	r = runWith(t, registry, e, "run", "logs", "38920693349", "-R", "o/r", "--json")
	for _, s := range []string{`"repo": "o/r"`, `"run_id": 38920693349`, `"name": "lint"`, `"log": "lint ok\n"`, `"log": "\u001b[31mFAIL\n"`} {
		if !strings.Contains(r.stdout, s) {
			t.Fatalf("missing %s in:\n%s", s, r.stdout)
		}
	}
}

func TestRunLogsFailed(t *testing.T) {
	h := logsHub(t)
	e, _, _ := testEnv(t, h)
	setToken(t, e, "ghp_x")
	r := runWith(t, registry, e, "run", "logs", "38920693349", "-R", "o/r", "--failed")
	want := "== test (failure) ==\njob 71 log\n== slow (timed_out) ==\njob 74 log\n"
	if r.code != 0 || r.stdout != want {
		t.Fatalf("exit %d, stderr %q, stdout:\n%s\nwant:\n%s", r.code, r.stderr, r.stdout, want)
	}
	for _, u := range h.uris() {
		if strings.Contains(u, "jobs/72") || strings.Contains(u, "jobs/73") || strings.HasSuffix(u, "runs/38920693349/logs") {
			t.Fatalf("fetched %s", u)
		}
	}
	r = runWith(t, registry, e, "run", "logs", "38920693349", "-R", "o/r", "--failed", "--json")
	if !strings.Contains(r.stdout, `"conclusion": "timed_out"`) {
		t.Fatalf("stdout:\n%s", r.stdout)
	}
}

func TestRunLogsNoFailedJobs(t *testing.T) {
	h := newHub(t)
	h.api["/repos/o/r/actions/runs/5/jobs?filter=latest&per_page=100"] = `{"total_count":1,"jobs":[{"id":1,"name":"a","status":"completed","conclusion":"success"}]}`
	e, _, _ := testEnv(t, h)
	setToken(t, e, "ghp_x")
	r := runWith(t, registry, e, "run", "logs", "5", "-R", "o/r", "--failed")
	if r.code != 0 || r.stdout != "no failed jobs in run 5\n" {
		t.Fatalf("exit %d, stdout %q", r.code, r.stdout)
	}
	r = runWith(t, registry, e, "run", "logs", "5", "-R", "o/r", "--failed", "--json")
	if !strings.Contains(r.stdout, `"jobs": []`) {
		t.Fatalf("stdout %s", r.stdout)
	}
}

func TestRunLogsNeedsAToken(t *testing.T) {
	h := newHub(t)
	e, _, _ := testEnv(t, h)
	r := runWith(t, registry, e, "run", "logs", "5", "-R", "o/r", "--json")
	if r.code != 3 || !strings.Contains(r.stderr, `"code": "token_required"`) || len(h.uris()) != 0 {
		t.Fatalf("exit %d, stderr %q, asked %q", r.code, r.stderr, h.uris())
	}
}

func TestRunLogsSizeCap(t *testing.T) {
	h := newHub(t)
	h.redirect["/repos/o/r/actions/runs/5/logs"] = runLogsURL + "?sig=s"
	h.files[runLogsURL] = zipBytes(t, "0_big.txt", strings.Repeat("x", 2<<20))
	e, _, _ := testEnv(t, h)
	setToken(t, e, "ghp_x")
	r := runWith(t, registry, e, "run", "logs", "5", "-R", "o/r", "--max-size", "1")
	if r.code != 1 || !strings.Contains(r.stderr, "over 1048576 bytes") || r.stdout != "" {
		t.Fatalf("exit %d, stderr %q, stdout %q", r.code, r.stderr, r.stdout)
	}
}

func TestRunIDs(t *testing.T) {
	for _, bad := range []string{"0", "007", "x", "99999999999999999999"} {
		h := newHub(t)
		e, _, _ := testEnv(t, h)
		setToken(t, e, "ghp_x")
		r := runWith(t, registry, e, "run", "logs", bad, "-R", "o/r")
		if r.code != 2 || !strings.Contains(r.stderr, "is not a run ID") || len(h.uris()) != 0 {
			t.Errorf("%s: exit %d, stderr %q", bad, r.code, r.stderr)
		}
	}
	h := newHub(t)
	e, _, _ := testEnv(t, h)
	setToken(t, e, "ghp_x")
	if r := runWith(t, registry, e, "run", "logs", "5", "-R", "o/r", "--max-size", "0"); r.code != 2 || len(h.uris()) != 0 {
		t.Fatalf("--max-size 0: exit %d, stderr %q", r.code, r.stderr)
	}
}

func TestRunLogsFailedNamesTheSizeCap(t *testing.T) {
	h := logsHub(t)
	h.files[jobLogURL+"71.txt"] = []byte(strings.Repeat("x", 1<<20))
	e, _, _ := testEnv(t, h)
	setToken(t, e, "ghp_x")
	r := runWith(t, registry, e, "run", "logs", "38920693349", "-R", "o/r", "--failed", "--max-size", "1")
	if r.code != 1 || !strings.Contains(r.stderr, "--max-size 1 MiB") {
		t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
	}
}
