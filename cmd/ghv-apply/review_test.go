package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Sriram-PR/ghv/internal/draft"
	"github.com/Sriram-PR/ghv/internal/harden"
)

func at(hour int) time.Time { return time.Date(2026, 10, 16, hour, 0, 0, 0, time.UTC) }

func queueDraft(t *testing.T, home string, d draft.Draft) string {
	t.Helper()
	id, err := draft.Queue{Root: queueRoot(home)}.Write(d)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

// reviewEnv is a session with a vault for Sriram-PR, the given GitHub, and
// the agent's cache and read token planted where ghv keeps them.
func reviewEnv(t *testing.T, ft *fakeTerm, routes map[string]apiReply) (env, *fakeAPI) {
	t.Helper()
	f := &fakeAPI{routes: routes}
	e := testEnv(t)
	e.openTTY = func() (terminal, error) { return ft, nil }
	e.client = harden.Client(f)
	seedVault(t, e.home)
	for name, data := range map[string]string{
		".config/ghv/read-token":   "ghp_plantedplantedplantedplanted0000",
		".cache/ghv/0123456789abc": `{"full_name":"evil/poisoned"}`,
	} {
		p := filepath.Join(e.home, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return e, f
}

func states(t *testing.T, home string) map[string]draft.Entry {
	t.Helper()
	es, err := draft.Queue{Root: queueRoot(home)}.List()
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]draft.Entry{}
	for _, e := range es {
		out[e.ID] = e
	}
	return out
}

func TestReviewRejectAndSkip(t *testing.T) {
	routes := baseRoutes()
	routes["/repos/o/r/issues/7"] = ok(`{"number":7,"title":"Crash","state":"open","user":{"login":"u"}}`)
	ft := answers(t, "x", "r", "  not wanted ", "s")
	e, f := reviewEnv(t, ft, routes)
	later := queueDraft(t, e.home, draft.Draft{Repo: "o/r", CreatedAt: at(10), Fields: draft.IssueCreateFields{Title: "Bug", Body: "b"}})
	first := queueDraft(t, e.home, draft.Draft{Repo: "o/r", CreatedAt: at(9), Fields: draft.CommentFields{Number: 7, Body: "hi @u"}})

	if r := runWith(e); r.code != 0 || r.stdout != "" || r.stderr != "" {
		t.Fatalf("exit %d, stdout %q, stderr %q, terminal:\n%s", r.code, r.stdout, r.stderr, ft.shown())
	}
	shown := ft.shown()
	for _, s := range []string{
		"2 drafts wait for review.",
		"== Draft 1 of 2 ==\nContext from GitHub:\n  Repository: o/r, created 2019-01-02, 7 years ago",
		"  Comment on issue #7: Crash (open), opened by u",
		"Draft " + first + ": comment to o/r",
		"-- end of " + first + ": body 1 line, 5 characters --\nCheck before deciding:\n  - you have not taken part in o/r before\n  - findings to read: mention 1\n[r]eject, [s]kip, or [q]uit? \nAnswer with one of the letters in brackets.\n",
		"Reason, for the agent to read (Enter for none): \nRejected " + first + ".",
		"== Draft 2 of 2 ==",
		"Left " + later + " in the queue.",
		"Rejected 1, left 1 in the queue.",
	} {
		if !strings.Contains(shown, s) {
			t.Errorf("terminal lacks %q:\n%s", s, shown)
		}
	}
	if strings.Index(shown, first) > strings.Index(shown, later) {
		t.Errorf("the older draft is not first")
	}
	if strings.Contains(shown, "poisoned") {
		t.Errorf("the planted cache reached the terminal")
	}
	st := states(t, e.home)
	if o := st[first].Outcome; st[first].State != draft.Rejected || o == nil || o.Reason != "not wanted" || !o.At.Equal(e.now()) {
		t.Fatalf("first: %+v, outcome %+v", st[first], o)
	}
	if st[later].State != draft.Pending {
		t.Fatalf("later: %+v", st[later])
	}
	for _, r := range f.reqs {
		if r.Method != "GET" || r.Header.Get("Authorization") != "" || r.Header.Get("If-None-Match") != "" {
			t.Fatalf("%s %s with headers %v", r.Method, r.URL, r.Header)
		}
	}
}

func TestReviewQuit(t *testing.T) {
	ft := answers(t, "quit")
	e, _ := reviewEnv(t, ft, baseRoutes())
	a := queueDraft(t, e.home, draft.Draft{Repo: "o/r", CreatedAt: at(9), Fields: draft.IssueCreateFields{Title: "A", Body: ""}})
	b := queueDraft(t, e.home, draft.Draft{Repo: "o/r", CreatedAt: at(10), Fields: draft.IssueCreateFields{Title: "B", Body: ""}})
	if r := runWith(e); r.code != 0 || !strings.Contains(ft.shown(), "Rejected 0, left 2 in the queue.") {
		t.Fatalf("exit %d, stderr %q, terminal:\n%s", r.code, r.stderr, ft.shown())
	}
	st := states(t, e.home)
	if st[a].State != draft.Pending || st[b].State != draft.Pending {
		t.Fatalf("%+v", st)
	}
}

func TestReviewDiff(t *testing.T) {
	routes := createRoutes()
	routes["diff "+compare] = ok("diff --git a/x b/x\n+\x1b[31mred\n")
	ft := answers(t, "d", "s")
	e, _ := reviewEnv(t, ft, routes)
	queueDraft(t, e.home, draft.Draft{Repo: "o/r", CreatedAt: at(9), Fields: draft.PRCreateFields{
		Base: "main", Head: "Sriram-PR:fix/x", HeadSHA: headSHA, Title: "Fix", Body: "b"}})
	if r := runWith(e); r.code != 0 {
		t.Fatalf("exit %d, stderr %q, terminal:\n%s", r.code, r.stderr, ft.shown())
	}
	if !strings.Contains(ft.shown(), "[r]eject, [s]kip, [d]iff, or [q]uit? \ndiff --git a/x b/x\n+<U+001B>[31mred\n-- end of the diff: 2 lines --\n[r]eject, [s]kip, [d]iff, or [q]uit? \n") {
		t.Fatalf("terminal:\n%s", ft.shown())
	}
}

func TestReviewUnreadableDraft(t *testing.T) {
	ft := answers(t, "s", "r", "")
	e, _ := reviewEnv(t, ft, baseRoutes())
	good := queueDraft(t, e.home, draft.Draft{Repo: "o/r", CreatedAt: at(9), Fields: draft.IssueCreateFields{Title: "A", Body: ""}})
	bad := "d-000000000000"
	if err := os.WriteFile(filepath.Join(queueRoot(e.home), "queue", bad+".json"), []byte(`{"schema":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if r := runWith(e); r.code != 0 {
		t.Fatalf("exit %d, stderr %q, terminal:\n%s", r.code, r.stderr, ft.shown())
	}
	shown := ft.shown()
	if !strings.Contains(shown, "== Draft 2 of 2 ==\nDraft "+bad+" cannot be read: invalid draft: ") ||
		!strings.Contains(shown, "ghv-apply will not send it; reject it to clear it from the queue.") {
		t.Fatalf("terminal:\n%s", shown)
	}
	st := states(t, e.home)
	if st[good].State != draft.Pending || st[bad].State != draft.Rejected || st[bad].Outcome.Reason != "rejected in review" {
		t.Fatalf("%+v", st)
	}
}

func TestReviewNothingToDo(t *testing.T) {
	ft := answers(t)
	e, _ := reviewEnv(t, ft, baseRoutes())
	id := queueDraft(t, e.home, draft.Draft{Repo: "o/r", CreatedAt: at(9), Fields: draft.IssueCreateFields{Title: "A", Body: ""}})
	if err := (draft.Queue{Root: queueRoot(e.home)}).Move(id, draft.Pending, draft.Unknown, draft.Outcome{At: at(10)}); err != nil {
		t.Fatal(err)
	}
	if r := runWith(e); r.code != 0 || ft.shown() != "No drafts wait for review.\n1 draft in unknown/ waits for its result to be checked, which this ghv-apply cannot do yet.\n" {
		t.Fatalf("exit %d, stderr %q, terminal %q", r.code, r.stderr, ft.shown())
	}
}

func TestReviewRefusals(t *testing.T) {
	e := testEnv(t)
	e.openTTY = func() (terminal, error) { return answers(t), nil }
	if r := runWith(e); r.code != 1 || !strings.Contains(r.stderr, "no vault at ") {
		t.Fatalf("no vault: exit %d, stderr %q", r.code, r.stderr)
	}

	e, _ = reviewEnv(t, answers(t), baseRoutes())
	lock, err := draft.Queue{Root: queueRoot(e.home)}.Lock()
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Release()
	if r := runWith(e); r.code != 1 || !strings.Contains(r.stderr, "another ghv-apply session holds the queue lock") {
		t.Fatalf("locked: exit %d, stderr %q", r.code, r.stderr)
	}
}
