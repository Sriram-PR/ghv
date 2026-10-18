package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/Sriram-PR/ghv/internal/draft"
)

func TestPREdit(t *testing.T) {
	tests := []struct {
		name        string
		args        []string
		title, body *string
	}{
		{"title", []string{"--title", "New title"}, ptr("New title"), nil},
		{"body", []string{"--body-file", "b.md"}, nil, ptr("New body\n")},
		{"both", []string{"--title", "New title", "--body-file", "b.md"}, ptr("New title"), ptr("New body\n")},
		{"empty body clears it", []string{"--body-file", "empty.md"}, nil, ptr("")},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, _, _ := testEnv(t, noNetwork(t))
			writeFile(t, filepath.Join(e.dir, "b.md"), "New body\n")
			writeFile(t, filepath.Join(e.dir, "empty.md"), "")
			r := runWith(t, registry, e, append([]string{"pr", "edit", "7", "-R", "o/r"}, tt.args...)...)
			if r.code != 0 || !draftedRE.MatchString(r.stdout) {
				t.Fatalf("exit %d, stdout %q, stderr %q", r.code, r.stdout, r.stderr)
			}
			f := queued(t, e).Fields.(draft.PREditFields)
			if f.Number != 7 || !samePtr(f.Title, tt.title) || !samePtr(f.Body, tt.body) {
				t.Fatalf("fields %+v", f)
			}
		})
	}
}

func TestPREditUsage(t *testing.T) {
	for msg, args := range map[string][]string{
		"ghv pr edit needs --title, --body-file, or both": {"7", "-R", "o/r"},
		"title is empty":            {"7", "-R", "o/r", "--title", ""},
		"drafts need -R OWNER/REPO": {"7", "--title", "T"},
		`"x" is not a number`:       {"x", "-R", "o/r", "--title", "T"},
	} {
		e, _, _ := testEnv(t, noNetwork(t))
		r := runWith(t, registry, e, append([]string{"pr", "edit"}, args...)...)
		if r.code != 2 || !strings.Contains(r.stderr, msg) {
			t.Fatalf("%q: exit %d, stderr %q", args, r.code, r.stderr)
		}
		emptyQueue(t, e)
	}
}

func ptr(s string) *string { return &s }

func samePtr(a, b *string) bool { return (a == nil && b == nil) || (a != nil && b != nil && *a == *b) }
