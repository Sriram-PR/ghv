package main

import (
	"strings"
	"testing"
	"time"

	"github.com/Sriram-PR/ghv/internal/draft"
)

const forkURL = "https://github.com/Sriram-PR/r"

func forkRoutes() map[string]apiReply {
	routes := baseRoutes()
	routes["POST /repos/o/r/forks"] = apiReply{status: 202, body: `{"full_name":"Sriram-PR/r","html_url":"` + forkURL + `","owner":{"login":"Sriram-PR"}}`}
	return routes
}

func TestApproveForkWaitsForTheFork(t *testing.T) {
	ft := answers(t, oldPass, "a")
	routes := forkRoutes()
	e, f := reviewEnv(t, ft, routes)
	var slept []time.Duration
	e.sleep = func(d time.Duration) {
		slept = append(slept, d)
		if len(slept) == 2 {
			routes["/repos/Sriram-PR/r"] = ok(`{"full_name":"Sriram-PR/r","fork":true,"parent":{"full_name":"o/r"}}`)
		}
	}
	id := queueDraft(t, e.home, draft.Draft{Repo: "o/r", CreatedAt: at(9), Fields: draft.ForkFields{}})
	if r := runWith(e); r.code != 0 {
		t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
	}
	if !strings.Contains(ft.shown(), "Sent: "+forkURL+"\nWaiting for GitHub to make the fork...\nFork ready: "+forkURL+"\n") || strings.Contains(ft.shown(), "Warning") {
		t.Fatalf("terminal:\n%s", ft.shown())
	}
	if len(slept) != 2 || slept[0] != 3*time.Second || f.count("/repos/Sriram-PR/r") != 4 {
		t.Fatalf("slept %v, %d reads of the fork", slept, f.count("/repos/Sriram-PR/r"))
	}
	if o := states(t, e.home)[id].Outcome; o.FullName != "Sriram-PR/r" || o.Note != "" {
		t.Fatalf("%+v", o)
	}
}

func TestApproveForkNotReadyIn60Seconds(t *testing.T) {
	ft := answers(t, oldPass, "a")
	e, _ := reviewEnv(t, ft, forkRoutes())
	var waited time.Duration
	e.sleep = func(d time.Duration) { waited += d }
	id := queueDraft(t, e.home, draft.Draft{Repo: "o/r", CreatedAt: at(9), Fields: draft.ForkFields{}})
	if r := runWith(e); r.code != 0 {
		t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
	}
	if !strings.Contains(ft.shown(), "Waiting for GitHub to make the fork...\nWarning: GitHub had not finished the fork after 60 seconds.\n") || waited != time.Minute {
		t.Fatalf("waited %v, terminal:\n%s", waited, ft.shown())
	}
	if st := states(t, e.home)[id]; st.State != draft.Applied || st.Outcome.Note != "GitHub had not finished the fork after 60 seconds" {
		t.Fatalf("%+v %+v", st, st.Outcome)
	}
}
