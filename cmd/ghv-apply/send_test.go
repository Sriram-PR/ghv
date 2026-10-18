package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Sriram-PR/ghv/internal/audit"
	"github.com/Sriram-PR/ghv/internal/draft"
	"github.com/Sriram-PR/ghv/internal/write"
)

const commentURL = "https://github.com/o/r/issues/7#issuecomment-1"

func comment(body string) draft.Draft {
	return draft.Draft{Repo: "o/r", CreatedAt: at(9), Fields: draft.CommentFields{Number: 7, Body: body}}
}

// sendRoutes answer a comment on o/r#7 with reply.
func sendRoutes(reply apiReply) map[string]apiReply {
	routes := baseRoutes()
	routes["/repos/o/r/issues/7"] = ok(`{"number":7,"title":"Crash","state":"open","user":{"login":"u"}}`)
	routes["POST /repos/o/r/issues/7/comments"] = reply
	return routes
}

func created(body string) apiReply { return apiReply{status: 201, body: body} }

func auditLines(t *testing.T, home string) []audit.Entry {
	t.Helper()
	data, err := os.ReadFile(audit.Path(home))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	var out []audit.Entry
	for l := range strings.SplitSeq(strings.TrimSuffix(string(data), "\n"), "\n") {
		var e audit.Entry
		if err := json.Unmarshal([]byte(l), &e); err != nil {
			t.Fatalf("audit line %q: %v", l, err)
		}
		out = append(out, e)
	}
	return out
}

func TestApproveSends(t *testing.T) {
	ft := answers(t, oldPass, "a")
	e, f := reviewEnv(t, ft, sendRoutes(created(`{"html_url":"`+commentURL+`","user":{"login":"Sriram-PR"}}`)))
	d := comment("hi")
	id := queueDraft(t, e.home, d)
	if r := runWith(e); r.code != 0 || r.stderr != "" {
		t.Fatalf("exit %d, stderr %q, terminal:\n%s", r.code, r.stderr, ft.shown())
	}
	req, err := write.Build(d, "o/r")
	if err != nil {
		t.Fatal(err)
	}
	sum := fmt.Sprintf("%x", req.SHA256())
	for _, s := range []string{
		"  request SHA-256 " + sum + "\n-- end of " + id,
		"[a]pprove, [e]dit, [r]eject, [s]kip, or [q]uit? \nSent: " + commentURL + "\n\nSent 1, rejected 0, left 0 in the queue.\n",
	} {
		if !strings.Contains(ft.shown(), s) {
			t.Fatalf("terminal lacks %q:\n%s", s, ft.shown())
		}
	}
	last := f.reqs[len(f.reqs)-1]
	if last.Method != "POST" || last.Header.Get("Authorization") != "Bearer "+testToken || len(f.bodies) != 1 || f.bodies[0] != `{"body":"hi"}` {
		t.Fatalf("sent %s %s with %q", last.Method, last.URL, f.bodies)
	}
	st := states(t, e.home)
	if o := st[id].Outcome; st[id].State != draft.Applied || o == nil || o.Status != 201 || o.URL != commentURL || !o.At.Equal(e.now()) || o.Note != "" {
		t.Fatalf("%+v, outcome %+v", st[id], o)
	}
	want := audit.Entry{Time: e.now(), Draft: id, Kind: "comment", Repo: "o/r", RequestSHA256: sum, Status: 201, URL: commentURL}
	if got := auditLines(t, e.home); len(got) != 1 || got[0] != want {
		t.Fatalf("audit %+v", got)
	}
}

// The draft file changes after ghv-apply read it: what was shown is not
// what the queue holds, so nothing is sent.
func TestApproveChangedDraftIsNotSent(t *testing.T) {
	var path string
	change := func(string) string {
		evil, err := draft.Encode(comment("something else"))
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, evil, 0o600); err != nil {
			t.Fatal(err)
		}
		return "a"
	}
	ft := answers(t, oldPass, change)
	e, f := reviewEnv(t, ft, sendRoutes(created(`{"html_url":"`+commentURL+`"}`)))
	id := queueDraft(t, e.home, comment("hi"))
	path = filepath.Join(queueRoot(e.home), "queue", id+".json")
	if r := runWith(e); r.code != 0 {
		t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
	}
	if !strings.Contains(ft.shown(), "Not sent: draft "+id+": the draft file changed after ghv-apply read it\n\nSent 0, rejected 0, left 1 in the queue.") {
		t.Fatalf("terminal:\n%s", ft.shown())
	}
	if len(f.bodies) != 0 || states(t, e.home)[id].State != draft.Pending || auditLines(t, e.home) != nil {
		t.Fatalf("sent %q", f.bodies)
	}
}

func TestApproveFates(t *testing.T) {
	for _, c := range []struct {
		name  string
		reply apiReply
		state draft.State
		shown string
		check func(t *testing.T, o *draft.Outcome)
	}{
		{"refused", apiReply{status: 422, body: `{"message":"Validation Failed","errors":[{"message":"Body is too long"}]}`}, draft.Rejected,
			"GitHub refused it: GitHub answered 422: Validation Failed (Body is too long)\n\nSent 0, rejected 1, left 0 in the queue.\n",
			func(t *testing.T, o *draft.Outcome) {
				if o.Status != 422 || o.Reason != "GitHub refused it: GitHub answered 422: Validation Failed (Body is too long)" {
					t.Fatalf("outcome %+v", o)
				}
			}},
		{"no answer", apiReply{status: 502, body: `<html>`}, draft.Unknown,
			"Sent, but GitHub's answer does not say what happened: GitHub answered 502\nd-ID stays in unknown/; check GitHub before asking for it again.\n\nSent 0, rejected 0, left 0 in the queue.\n1 draft in unknown/ waits for its result to be checked",
			func(t *testing.T, o *draft.Outcome) {
				if o.Note != "sending" || !o.At.Equal(reviewNow) {
					t.Fatalf("outcome %+v", o)
				}
			}},
		{"someone else", created(`{"html_url":"` + commentURL + `","user":{"login":"other"}}`), draft.Applied,
			"Sent: " + commentURL + "\nWarning: GitHub says other made it, not Sriram-PR.\n",
			func(t *testing.T, o *draft.Outcome) {
				if o.Note != "GitHub says other made it, not Sriram-PR" {
					t.Fatalf("outcome %+v", o)
				}
			}},
	} {
		t.Run(c.name, func(t *testing.T) {
			ft := answers(t, oldPass, "a")
			e, _ := reviewEnv(t, ft, sendRoutes(c.reply))
			id := queueDraft(t, e.home, comment("hi"))
			if r := runWith(e); r.code != 0 {
				t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
			}
			if want := strings.ReplaceAll(c.shown, "d-ID", id); !strings.Contains(ft.shown(), want) {
				t.Fatalf("terminal lacks %q:\n%s", want, ft.shown())
			}
			st := states(t, e.home)
			if st[id].State != c.state || st[id].Outcome == nil {
				t.Fatalf("%+v", st[id])
			}
			c.check(t, st[id].Outcome)
			if n := len(auditLines(t, e.home)); (c.state == draft.Applied) != (n == 1) {
				t.Fatalf("%d audit lines", n)
			}
		})
	}
}

func TestApproveNotSentEndsTheSession(t *testing.T) {
	ft := answers(t, oldPass, "a")
	e, f := reviewEnv(t, ft, sendRoutes(apiReply{status: 401, body: `{"message":"Bad credentials"}`}))
	first := queueDraft(t, e.home, comment("hi"))
	second := queueDraft(t, e.home, draft.Draft{Repo: "o/r", CreatedAt: at(10), Fields: draft.IssueCreateFields{Title: "A", Body: ""}})
	if r := runWith(e); r.code != 0 {
		t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
	}
	want := "Not sent: GitHub refused the write token (401): it is wrong, expired, or revoked\nThe session ends here; " + first + " stays in the queue.\n\nSent 0, rejected 0, left 2 in the queue.\n"
	if !strings.HasSuffix(ft.shown(), want) || strings.Contains(ft.shown(), "== Draft 2") {
		t.Fatalf("terminal:\n%s", ft.shown())
	}
	st := states(t, e.home)
	if st[first].State != draft.Pending || st[first].Outcome.Note != "not sent: GitHub refused the write token (401): it is wrong, expired, or revoked" ||
		st[second].State != draft.Pending || len(f.bodies) != 1 {
		t.Fatalf("%+v", st)
	}
}

func TestApproveNotOfferedWhenBlocked(t *testing.T) {
	routes := sendRoutes(created(`{"html_url":"` + commentURL + `"}`))
	delete(routes, "/repos/o/r/issues/7")
	ft := answers(t, oldPass, "a", "s")
	e, f := reviewEnv(t, ft, routes)
	queueDraft(t, e.home, comment("hi"))
	if r := runWith(e); r.code != 0 {
		t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
	}
	if !strings.Contains(ft.shown(), "  - o/r has no issue or pull request #7\nCheck before deciding:\n  - you have not taken part in o/r before\n[e]dit, [r]eject, [s]kip, or [q]uit? \nAnswer with one of the letters in brackets.\n") || len(f.bodies) != 0 {
		t.Fatalf("terminal:\n%s", ft.shown())
	}
}

func TestApprovePullRequest(t *testing.T) {
	d := createDraft("Sriram-PR:fix/x")
	d.CreatedAt = at(9)
	pr := func(head string) apiReply {
		return created(`{"html_url":"https://github.com/o/r/pull/9","user":{"login":"Sriram-PR"},"head":{"sha":"` + head + `"}}`)
	}

	routes := createRoutes()
	routes["POST /repos/o/r/pulls"] = pr(headSHA)
	ft := answers(t, oldPass, "a")
	e, f := reviewEnv(t, ft, routes)
	id := queueDraft(t, e.home, d)
	if r := runWith(e); r.code != 0 || !strings.Contains(ft.shown(), "Sent: https://github.com/o/r/pull/9\n\n") {
		t.Fatalf("exit %d, stderr %q, terminal:\n%s", r.code, r.stderr, ft.shown())
	}
	want := `{"title":"t","head":"Sriram-PR:fix/x","base":"main","body":"b","draft":false,"maintainer_can_modify":true}`
	if len(f.bodies) != 1 || f.bodies[0] != want || f.count(branchAt) != 2 {
		t.Fatalf("sent %q after %d branch reads", f.bodies, f.count(branchAt))
	}
	if st := states(t, e.home); st[id].State != draft.Applied || st[id].Outcome.Note != "" {
		t.Fatalf("%+v", st[id])
	}

	routes = createRoutes()
	routes["POST /repos/o/r/pulls"] = pr(movedSHA)
	ft = answers(t, oldPass, "a")
	e, _ = reviewEnv(t, ft, routes)
	id = queueDraft(t, e.home, d)
	runWith(e)
	note := "the pull request's head is " + movedSHA + ", not the reviewed " + headSHA
	if st := states(t, e.home); st[id].State != draft.Applied || st[id].Outcome.Note != note || !strings.Contains(ft.shown(), "Warning: "+note+".") {
		t.Fatalf("%+v, terminal:\n%s", st[id].Outcome, ft.shown())
	}
}

// Minutes can pass between the preview and the answer; the branch is read
// again right before sending.
func TestApprovePullRequestWhoseHeadMoved(t *testing.T) {
	d := createDraft("Sriram-PR:fix/x")
	d.CreatedAt = at(9)
	var f *fakeAPI
	push := func(string) string {
		f.routes[branchAt] = ok(`{"commit":{"sha":"` + movedSHA + `"}}`)
		return "a"
	}
	routes := createRoutes()
	routes["POST /repos/o/r/pulls"] = created(`{"html_url":"https://github.com/o/r/pull/9"}`)
	ft := answers(t, oldPass, push)
	var e env
	e, f = reviewEnv(t, ft, routes)
	id := queueDraft(t, e.home, d)
	runWith(e)
	if !strings.Contains(ft.shown(), "Sriram-PR:fix/x has moved from 0123456789ab to fedcba987654 since you reviewed it. Not sent; "+id+" stays in the queue.\n") {
		t.Fatalf("terminal:\n%s", ft.shown())
	}
	if len(f.bodies) != 0 || states(t, e.home)[id].State != draft.Pending {
		t.Fatalf("sent %q", f.bodies)
	}
}
