package main

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"
)

const checksSHA = "9e1e1c929f882fd93e1c67ea520a4529352aab9a"

// checksServer answers the PR, its check runs, and its combined status.
// Check runs report in_progress for the first pendingRounds rounds.
func checksServer(t *testing.T, pendingRounds int, statuses string) (http.Handler, *int) {
	var mu sync.Mutex
	rounds := 0
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		defer mu.Unlock()
		switch r.URL.RequestURI() {
		case "/repos/o/r/pulls/7":
			rounds++
			io.WriteString(w, `{"number":7,"state":"open","head":{"label":"a:b","sha":"`+checksSHA+`"}}`)
		case "/repos/o/r/commits/" + checksSHA + "/check-runs?per_page=100":
			test := `{"name":"test","status":"completed","conclusion":"failure","html_url":"https://github.com/o/r/runs/2"}`
			if rounds <= pendingRounds {
				test = `{"name":"test","status":"in_progress","conclusion":null,"html_url":"https://github.com/o/r/runs/2"}`
			}
			io.WriteString(w, `{"total_count":3,"check_runs":[`+
				`{"name":"lint","status":"completed","conclusion":"success","html_url":"https://github.com/o/r/runs/1"},`+test+`,`+
				`{"name":"docs \u202e","status":"completed","conclusion":"skipped","html_url":"https://github.com/o/r/runs/3"}]}`)
		case "/repos/o/r/commits/" + checksSHA + "/status":
			io.WriteString(w, statuses)
		default:
			t.Errorf("unexpected %s", r.URL.RequestURI())
			w.WriteHeader(404)
		}
	}), &rounds
}

const noStatuses = `{"state":"pending","total_count":0,"statuses":[]}`

func TestPRChecks(t *testing.T) {
	h, _ := checksServer(t, 0, `{"state":"success","total_count":1,"statuses":[{"context":"ci/legacy","state":"success","target_url":"https://ci.example/1"}]}`)
	e, _, _ := testEnv(t, h)
	r := runWith(t, registry, e, "pr", "checks", "7", "-R", "o/r")
	want := "o/r#7 at 9e1e1c9: 3 passed, 1 failed, 0 pending\n\n" +
		"failure  test           https://github.com/o/r/runs/2\n" +
		"success  ci/legacy      https://ci.example/1\n" +
		"skipped  docs <U+202E>  https://github.com/o/r/runs/3\n" +
		"success  lint           https://github.com/o/r/runs/1\n"
	if r.code != 0 || r.stdout != want {
		t.Fatalf("exit %d, stderr %q, stdout:\n%s\nwant:\n%s", r.code, r.stderr, r.stdout, want)
	}
}

func TestPRChecksIgnoresEmptyCombinedStatus(t *testing.T) {
	h, _ := checksServer(t, 0, noStatuses)
	e, _, _ := testEnv(t, h)
	r := runWith(t, registry, e, "pr", "checks", "7", "-R", "o/r", "--json")
	for _, s := range []string{`"head_sha": "` + checksSHA + `"`, `"passed": 2`, `"failed": 1`, `"pending": 0`, `"kind": "check_run"`, `"name": "docs \u202e"`} {
		if !strings.Contains(r.stdout, s) {
			t.Fatalf("missing %s in:\n%s", s, r.stdout)
		}
	}
}

func TestPRChecksWatch(t *testing.T) {
	h, rounds := checksServer(t, 2, noStatuses)
	e, _, errb := testEnv(t, h)
	var slept []time.Duration
	e.sleep = func(ctx context.Context, d time.Duration) error {
		slept = append(slept, d)
		return nil
	}
	r := runWith(t, registry, e, "pr", "checks", "7", "-R", "o/r", "--watch")
	if r.code != 0 || *rounds != 3 || len(slept) != 2 || slept[0] != 2*time.Minute || slept[1] != 2*time.Minute {
		t.Fatalf("exit %d, rounds %d, slept %v, stderr %q", r.code, *rounds, slept, errb.String())
	}
	if !strings.HasPrefix(r.stdout, "o/r#7 at 9e1e1c9: 2 passed, 1 failed, 0 pending\n") || strings.Count(r.stdout, "o/r#7 at") != 1 {
		t.Fatalf("stdout:\n%s", r.stdout)
	}
	if strings.Count(r.stderr, "1 pending; checking again in 2m") != 2 {
		t.Fatalf("stderr %q", r.stderr)
	}
}

func TestPRChecksWatchStopsOnCancel(t *testing.T) {
	h, _ := checksServer(t, 100, noStatuses)
	e, _, _ := testEnv(t, h)
	e.sleep = func(ctx context.Context, d time.Duration) error { return context.Canceled }
	r := runWith(t, registry, e, "pr", "checks", "7", "-R", "o/r", "--watch")
	if r.code != 1 || r.stdout != "" {
		t.Fatalf("exit %d, stdout %q", r.code, r.stdout)
	}
}
