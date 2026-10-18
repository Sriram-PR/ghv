package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Sriram-PR/ghv/internal/draft"
)

func TestDraftsEmpty(t *testing.T) {
	e, _, _ := testEnv(t, noNetwork(t))
	if r := runWith(t, registry, e, "drafts"); r.code != 0 || r.stdout != "no drafts\n" {
		t.Fatalf("exit %d, stdout %q", r.code, r.stdout)
	}
	if r := runWith(t, registry, e, "drafts", "--json"); r.code != 0 || r.stdout != "[]\n" {
		t.Fatalf("exit %d, stdout %q", r.code, r.stdout)
	}
}

func TestDrafts(t *testing.T) {
	e, _, _ := testEnv(t, noNetwork(t))
	q := e.draftQueue()
	at := func(h int) time.Time { return time.Date(2026, 10, 18, h, 0, 0, 0, time.UTC) }
	write := func(h int, repo string, f draft.Fields) string {
		id, err := q.Write(draft.Draft{Repo: repo, CreatedAt: at(h), Fields: f})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	move := func(id string, from, to draft.State, o draft.Outcome) {
		if err := q.Move(id, from, to, o); err != nil {
			t.Fatal(err)
		}
	}
	fork := write(9, "cli/cli", draft.ForkFields{})
	issue := write(10, "o/r", draft.IssueCreateFields{Title: "Bug", Body: "b"})
	move(issue, draft.Pending, draft.Unknown, draft.Outcome{At: at(12)})
	move(issue, draft.Unknown, draft.Applied, draft.Outcome{At: at(12), Status: 201, URL: "https://github.com/o/r/issues/5"})
	comment := write(11, "o/r", draft.CommentFields{Number: 12, Body: "c"})
	move(comment, draft.Pending, draft.Rejected, draft.Outcome{At: at(12), Reason: "not now"})
	title := "evil\u202e"
	edit := write(13, "o/r", draft.PREditFields{Number: 7, Title: &title})
	sent := write(14, "o/r", draft.ReviewReplyFields{Number: 7, CommentID: 101, Body: "r"})
	move(sent, draft.Pending, draft.Unknown, draft.Outcome{At: at(14)})
	if err := os.WriteFile(filepath.Join(q.Dir(draft.Pending), "d-000000000000.json"), []byte("{"), 0o600); err != nil {
		t.Fatal(err)
	}

	r := runWith(t, registry, e, "drafts")
	lines := strings.Split(strings.TrimSuffix(r.stdout, "\n"), "\n")
	want := []struct{ id, parts string }{
		{fork, "pending|fork|cli/cli|waiting for ghv-apply"},
		{issue, "applied|issue.create|o/r|Bug|https://github.com/o/r/issues/5"},
		{comment, "rejected|comment|#12|rejected: not now"},
		{edit, "pending|pr.edit|#7 evil<U+202E>|waiting for ghv-apply"},
		{sent, "unknown|review.reply|#7|sent; ghv-apply checks the result next session"},
		{"d-000000000000", "pending|unreadable: invalid draft"},
	}
	if r.code != 0 || len(lines) != len(want) {
		t.Fatalf("exit %d, stderr %q, stdout:\n%s", r.code, r.stderr, r.stdout)
	}
	for i, w := range want {
		if !strings.HasPrefix(lines[i], w.id+"  ") {
			t.Fatalf("line %d is %q, want draft %s first in:\n%s", i, lines[i], w.id, r.stdout)
		}
		for _, p := range strings.Split(w.parts, "|") {
			if !strings.Contains(lines[i], p) {
				t.Fatalf("line %d %q lacks %q", i, lines[i], p)
			}
		}
	}

	r = runWith(t, registry, e, "drafts", "--json")
	for _, s := range []string{
		`"id": "` + issue + `",
    "state": "applied",
    "kind": "issue.create",
    "repo": "o/r",
    "title": "Bug",
    "created_at": "2026-10-18T10:00:00Z",
    "outcome": {
      "at": "2026-10-18T12:00:00Z",
      "status": 201,
      "url": "https://github.com/o/r/issues/5"
    }`,
		`"id": "` + fork + `",
    "state": "pending",
    "kind": "fork",
    "repo": "cli/cli",
    "created_at": "2026-10-18T09:00:00Z",
    "outcome": null`,
		`"number": 7,
    "title": "evil\u202e"`,
		`"id": "d-000000000000",
    "state": "pending",
    "outcome": null,
    "error": "invalid draft:`,
	} {
		if !strings.Contains(r.stdout, s) {
			t.Fatalf("missing\n%s\nin:\n%s", s, r.stdout)
		}
	}
}
