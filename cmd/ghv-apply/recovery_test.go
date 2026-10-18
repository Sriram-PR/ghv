package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/Sriram-PR/ghv/internal/audit"
	"github.com/Sriram-PR/ghv/internal/draft"
	"github.com/Sriram-PR/ghv/internal/harden"
)

// toUnknown moves a queued draft to unknown/ as a send with no clear
// answer leaves it.
func toUnknown(t *testing.T, home, id string, o draft.Outcome) {
	t.Helper()
	q := draft.Queue{Root: queueRoot(home)}
	l, err := draft.ReadFile(filepath.Join(q.Dir(draft.Pending), id+".json"))
	if err != nil {
		t.Fatal(err)
	}
	if err := q.Move(id, draft.Pending, draft.Unknown, l.SHA256, o); err != nil {
		t.Fatal(err)
	}
}

func commentsRoute(routes map[string]apiReply, body string) {
	routes["/repos/o/r/issues/7/comments?"+since] = ok(`[{"html_url":"` + commentURL + `","body":` + body + `,"user":{"login":"Sriram-PR"},"created_at":"2026-10-18T12:00:02Z"}]`)
}

func TestRecoveryFindsASentDraft(t *testing.T) {
	ft := answers(t, oldPass)
	routes := baseRoutes()
	commentsRoute(routes, `"hi"`)
	e, f := reviewEnv(t, ft, routes)
	id := queueDraft(t, e.home, comment("hi"))
	toUnknown(t, e.home, id, draft.Outcome{At: sentAt, Note: "sending"})
	if r := runWith(e); r.code != 0 {
		t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
	}
	want := "1 draft in unknown/ waits for its result to be checked.\nPassphrase: \n" +
		"\n== Unknown result 1 of 1 ==\n" + id + ": comment to o/r, sent 2026-10-18 12:00 UTC\n" +
		"Found on GitHub: " + commentURL + "\n" +
		"\nChecked 1 unknown result: found 1, queued again 0, rejected 0, kept 0.\n"
	if ft.shown() != want {
		t.Fatalf("terminal:\n%s\nwant:\n%s", ft.shown(), want)
	}
	st := states(t, e.home)
	if o := st[id].Outcome; st[id].State != draft.Applied || o.URL != commentURL || !o.At.Equal(sentAt) || o.Note != "found on GitHub after an unknown send" {
		t.Fatalf("%+v %+v", st[id], o)
	}
	wantAudit := audit.Entry{Time: sentAt, Draft: id, Kind: "comment", Repo: "o/r", URL: commentURL, Found: true}
	if got := auditLines(t, e.home); len(got) != 1 || got[0] != wantAudit {
		t.Fatalf("audit %+v", got)
	}
	for _, r := range f.reqs {
		if r.Method != "GET" || (r.URL.Path != "/user" && r.Header.Get("Authorization") != "") {
			t.Fatalf("%s %s with Authorization %q", r.Method, r.URL, r.Header.Get("Authorization"))
		}
	}
}

func TestRecoveryAsksWhenNothingIsFound(t *testing.T) {
	ft := answers(t, oldPass, "x", "q", "r", "k", "s")
	routes := baseRoutes()
	commentsRoute(routes, `"something else"`)
	routes["/repos/o/r/issues/7"] = ok(`{"number":7,"title":"Crash","state":"open","user":{"login":"u"}}`)
	e, _ := reviewEnv(t, ft, routes)
	var ids []string
	for i, body := range []string{"one", "two", "three"} {
		id := queueDraft(t, e.home, draft.Draft{Repo: "o/r", CreatedAt: at(9 + i), Fields: draft.CommentFields{Number: 7, Body: body}})
		toUnknown(t, e.home, id, draft.Outcome{At: sentAt, Note: "sending"})
		ids = append(ids, id)
	}
	if r := runWith(e); r.code != 0 {
		t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
	}
	shown := ft.shown()
	for _, s := range []string{
		"3 drafts in unknown/ wait for their result to be checked.\n",
		"== Unknown result 1 of 3 ==\n" + ids[0] + ": comment to o/r, sent 2026-10-18 12:00 UTC\n" +
			"Not found on GitHub. Checked your comments on o/r#7 since 2026-10-18 11:55 UTC, matching the draft's text.\n" +
			"[q]ueue it again, [r]eject it, or [k]eep it in unknown/? \nAnswer with one of the letters in brackets.\n" +
			"[q]ueue it again, [r]eject it, or [k]eep it in unknown/? \nQueued " + ids[0] + " again.\n",
		"Rejected " + ids[1] + ".\n",
		"Kept " + ids[2] + " in unknown/.\n",
		"\nChecked 3 unknown results: found 0, queued again 1, rejected 1, kept 1.\n",
		"1 draft waits for review.\n\n== Draft 1 of 1 ==\n",
		"Left " + ids[0] + " in the queue.\n\nSent 0, rejected 0, left 1 in the queue.\n" +
			"1 draft in unknown/ waits for its result to be checked; the next ghv-apply session checks it again.\n",
	} {
		if !strings.Contains(shown, s) {
			t.Errorf("terminal lacks %q", s)
		}
	}
	if t.Failed() {
		t.Fatalf("terminal:\n%s", shown)
	}
	st := states(t, e.home)
	if st[ids[0]].State != draft.Pending || st[ids[0]].Outcome.Note != "not found on GitHub after an unknown send; queued again" {
		t.Fatalf("queued: %+v %+v", st[ids[0]], st[ids[0]].Outcome)
	}
	if st[ids[1]].State != draft.Rejected || st[ids[1]].Outcome.Reason != "not found on GitHub after an unknown send" {
		t.Fatalf("rejected: %+v %+v", st[ids[1]], st[ids[1]].Outcome)
	}
	if st[ids[2]].State != draft.Unknown {
		t.Fatalf("kept: %+v", st[ids[2]])
	}
	if auditLines(t, e.home) != nil {
		t.Fatal("an audit line for a draft not found")
	}
}

func TestRecoveryLookupFailsKeepsTheDraft(t *testing.T) {
	ft := answers(t, oldPass)
	e, _ := reviewEnv(t, ft, baseRoutes())
	id := queueDraft(t, e.home, comment("hi"))
	toUnknown(t, e.home, id, draft.Outcome{At: sentAt, Note: "sending"})
	if r := runWith(e); r.code != 0 {
		t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
	}
	if !strings.Contains(ft.shown(), "Cannot check "+id+": GET /repos/o/r/issues/7/comments?"+since+": not found; it stays in unknown/.\n\nChecked 1 unknown result: found 0, queued again 0, rejected 0, kept 1.\n") {
		t.Fatalf("terminal:\n%s", ft.shown())
	}
	if states(t, e.home)[id].State != draft.Unknown {
		t.Fatal("moved")
	}
}

// An edited draft that gets no clear answer is checked without its text
// next session: the file holds the agent's words, not the ones sent.
func TestRecoveryOfAnEditedSend(t *testing.T) {
	ft := answers(t, oldPass, "e", "a")
	e, _ := reviewEnv(t, ft, sendRoutes(apiReply{status: 502, body: `<html>`}))
	var opened string
	e.edit = writes("better words\n", &opened)
	id := queueDraft(t, e.home, comment("hi"))
	if r := runWith(e); r.code != 0 {
		t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
	}
	st := states(t, e.home)
	if o := st[id].Outcome; st[id].State != draft.Unknown || o.Note != "sending" || !o.Edited {
		t.Fatalf("%+v %+v", st[id], o)
	}

	ft2 := answers(t, oldPass)
	routes := baseRoutes()
	commentsRoute(routes, `"better words"`)
	e.openTTY = func() (terminal, error) { return ft2, nil }
	e.client = harden.Client(&fakeAPI{routes: routes})
	if r := runWith(e); r.code != 0 {
		t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
	}
	if !strings.Contains(ft2.shown(), "Found on GitHub: "+commentURL+"\n") {
		t.Fatalf("terminal:\n%s", ft2.shown())
	}
	if o := states(t, e.home)[id].Outcome; !o.Edited || o.URL != commentURL {
		t.Fatalf("%+v", o)
	}
}
