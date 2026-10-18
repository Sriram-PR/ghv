package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/Sriram-PR/ghv/internal/draft"
)

func TestCommentQueuesADraft(t *testing.T) {
	e, _, _ := testEnv(t, noNetwork(t))
	writeFile(t, filepath.Join(e.dir, "c.md"), "Thanks, fixed in the next push.\n")
	r := runWith(t, registry, e, "comment", "12", "-R", "o/r", "--body-file", "c.md")
	if r.code != 0 || !draftedRE.MatchString(r.stdout) {
		t.Fatalf("exit %d, stdout %q, stderr %q", r.code, r.stdout, r.stderr)
	}
	d := queued(t, e)
	if d.Repo != "o/r" || d.Fields != (draft.CommentFields{Number: 12, Body: "Thanks, fixed in the next push.\n"}) {
		t.Fatalf("draft %+v", d)
	}
}

func TestCommentUsage(t *testing.T) {
	tests := []struct {
		name string
		args []string
		msg  string
	}{
		{"bad number", []string{"012", "-R", "o/r", "--body-file", "c.md"}, `"012" is not a number`},
		{"no body", []string{"12", "-R", "o/r"}, "ghv comment needs --body-file"},
		{"empty body", []string{"12", "-R", "o/r", "--body-file", "empty.md"}, "the body is empty"},
		{"no -R", []string{"12", "--body-file", "c.md"}, "drafts need -R OWNER/REPO"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, _, _ := testEnv(t, noNetwork(t))
			writeFile(t, filepath.Join(e.dir, "c.md"), "c")
			writeFile(t, filepath.Join(e.dir, "empty.md"), " \n\n")
			r := runWith(t, registry, e, append([]string{"comment"}, tt.args...)...)
			if r.code != 2 || !strings.Contains(r.stderr, tt.msg) {
				t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
			}
			emptyQueue(t, e)
		})
	}
}

func TestForkQueuesADraft(t *testing.T) {
	e, _, _ := testEnv(t, noNetwork(t))
	r := runWith(t, registry, e, "fork", "cli/cli", "--json")
	d := queued(t, e)
	if r.code != 0 || !strings.Contains(r.stdout, `"kind": "fork"`) || d.Repo != "cli/cli" || d.Fields != (draft.ForkFields{}) {
		t.Fatalf("exit %d, stdout %q, draft %+v", r.code, r.stdout, d)
	}
}

func TestForkUsage(t *testing.T) {
	for msg, args := range map[string][]string{
		`"cli" is not OWNER/REPO`:        {"fork", "cli"},
		"ghv fork needs R":               {"fork"},
		`unknown flag "-R" for ghv fork`: {"fork", "cli/cli", "-R", "o/r"},
	} {
		e, _, _ := testEnv(t, noNetwork(t))
		r := runWith(t, registry, e, args...)
		if r.code != 2 || !strings.Contains(r.stderr, msg) {
			t.Fatalf("%q: exit %d, stderr %q", args, r.code, r.stderr)
		}
		emptyQueue(t, e)
	}
}
