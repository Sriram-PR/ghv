package main

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Sriram-PR/ghv/internal/draft"
)

func TestEditTextRoundTrip(t *testing.T) {
	title, body, empty := "T", "line 1\n\nline 3\n", ""
	for _, f := range []draft.Fields{
		draft.PRCreateFields{Base: "main", Head: "me:x", HeadSHA: headSHA, Title: "Fix", Body: body},
		draft.PRCreateFields{Base: "main", Head: "me:x", HeadSHA: headSHA, Title: "Fix", Body: ""},
		draft.PREditFields{Number: 1, Title: &title},
		draft.PREditFields{Number: 1, Body: &empty},
		draft.PREditFields{Number: 1, Title: &title, Body: &body},
		draft.CommentFields{Number: 1, Body: "hi"},
		draft.ReviewReplyFields{Number: 1, CommentID: 2, Body: "yes\n"},
		draft.IssueCreateFields{Title: "Bug", Body: "b"},
	} {
		d := draft.Draft{Repo: "o/r", CreatedAt: at(9), Fields: f}
		got, err := applyEdit(d, editText(d))
		if err != nil || !reflect.DeepEqual(got, d) {
			t.Errorf("%s %q: got %+v, %v", d.Kind(), editText(d), got.Fields, err)
		}
	}
	if editable(draft.Draft{Repo: "o/r", Fields: draft.ForkFields{}}) {
		t.Error("a fork has no text to edit")
	}
}

func TestApplyEdit(t *testing.T) {
	issue := draft.Draft{Repo: "o/r", CreatedAt: at(9), Fields: draft.IssueCreateFields{Title: "Bug", Body: "b"}}
	title := "T"
	prTitle := draft.Draft{Repo: "o/r", CreatedAt: at(9), Fields: draft.PREditFields{Number: 1, Title: &title}}
	c := draft.Draft{Repo: "o/r", CreatedAt: at(9), Fields: draft.CommentFields{Number: 1, Body: "hi"}}
	for _, tt := range []struct {
		d    draft.Draft
		text string
		want draft.Fields
		err  string
	}{
		{issue, "New\r\n\r\nfirst\r\nsecond\r\n", draft.IssueCreateFields{Title: "New", Body: "first\nsecond"}, ""},
		{issue, "New\n", draft.IssueCreateFields{Title: "New", Body: ""}, ""},
		{issue, "New\nbody", nil, "leave the second line empty: the title is the first line, and the body starts on the third"},
		{issue, "\n\nbody\n", nil, "invalid draft: title is empty"},
		{prTitle, "two\nlines\n", nil, "the title must be one line"},
		{c, " \n\n", nil, "the body is empty"},
		{c, "a\r\n", draft.CommentFields{Number: 1, Body: "a"}, ""},
	} {
		got, err := applyEdit(tt.d, tt.text)
		switch {
		case tt.err != "":
			if err == nil || err.Error() != tt.err {
				t.Errorf("%q: error %v, want %q", tt.text, err, tt.err)
			}
		case err != nil || !reflect.DeepEqual(got.Fields, tt.want):
			t.Errorf("%q: got %+v, %v", tt.text, got.Fields, err)
		}
	}
}

func TestEditorCommand(t *testing.T) {
	t.Setenv("VISUAL", "")
	t.Setenv("EDITOR", "")
	if _, err := editorCommand(); err != errNoEditor {
		t.Fatalf("no editor: %v", err)
	}
	t.Setenv("EDITOR", "/usr/bin/vi -n")
	if argv, err := editorCommand(); err != nil || !reflect.DeepEqual(argv, []string{"/usr/bin/vi", "-n"}) {
		t.Fatalf("EDITOR: %q, %v", argv, err)
	}
	t.Setenv("VISUAL", "/opt/ed")
	if argv, err := editorCommand(); err != nil || !reflect.DeepEqual(argv, []string{"/opt/ed"}) {
		t.Fatalf("VISUAL first: %q, %v", argv, err)
	}
	t.Setenv("VISUAL", "vi")
	if _, err := editorCommand(); err != errNoEditor {
		t.Fatalf("relative editor: %v", err)
	}
}

func TestRunEditor(t *testing.T) {
	dir := t.TempDir()
	script := filepath.Join(dir, "ed")
	if err := os.WriteFile(script, []byte("#!/bin/sh\nprintf '%s\\n' \"$1\" > \"$2\"\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	null, err := os.OpenFile(os.DevNull, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	defer null.Close()
	path := filepath.Join(dir, "d.md")
	if err := runEditor([]string{script, "-x"}, null, path); err != nil {
		t.Fatal(err)
	}
	if data, _ := os.ReadFile(path); string(data) != "-x\n" {
		t.Fatalf("the editor got %q", data)
	}
	if err := runEditor([]string{"/bin/false"}, null, path); err == nil || !strings.HasPrefix(err.Error(), "the editor failed: ") {
		t.Fatalf("a failing editor: %v", err)
	}
}

// writes returns an editor that replaces the file with text and records
// what it was given.
func writes(text string, opened *string) func(string) error {
	return func(path string) error {
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		*opened = string(data)
		return os.WriteFile(path, []byte(text), 0o600)
	}
}

func TestReviewEdit(t *testing.T) {
	ft := answers(t, oldPass, "e", "a")
	e, f := reviewEnv(t, ft, sendRoutes(created(`{"html_url":"`+commentURL+`","user":{"login":"Sriram-PR"}}`)))
	var opened string
	e.edit = writes("better words\n", &opened)
	id := queueDraft(t, e.home, comment("hi"))
	before, err := os.ReadFile(filepath.Join(queueRoot(e.home), "queue", id+".json"))
	if err != nil {
		t.Fatal(err)
	}
	if r := runWith(e); r.code != 0 {
		t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
	}
	shown := ft.shown()
	if opened != "hi\n" {
		t.Fatalf("the editor opened %q", opened)
	}
	i := strings.Index(shown, "Edited. ghv-apply sends this version; the queue keeps the agent's file.\nContext from GitHub:")
	if i < 0 || !strings.Contains(shown[i:], "body (1 line, 12 characters):\n    better words\n") {
		t.Fatalf("terminal:\n%s", shown)
	}
	if len(f.bodies) != 1 || f.bodies[0] != `{"body":"better words"}` {
		t.Fatalf("sent %q", f.bodies)
	}
	st := states(t, e.home)
	if st[id].State != draft.Applied || !st[id].Outcome.Edited {
		t.Fatalf("%+v", st[id])
	}
	after, err := os.ReadFile(filepath.Join(queueRoot(e.home), "applied", id+".json"))
	if err != nil || string(after) != string(before) {
		t.Fatalf("the queue's file changed: %v", err)
	}
}

func TestReviewEditNotUsed(t *testing.T) {
	ft := answers(t, oldPass, "e", "e", "e", "s")
	e, f := reviewEnv(t, ft, sendRoutes(created(`{"html_url":"`+commentURL+`"}`)))
	results := []error{errNoEditor, nil, nil}
	texts := []string{"", "hi\n", "  \n"}
	n := 0
	e.edit = func(path string) error {
		defer func() { n++ }()
		if results[n] != nil {
			return results[n]
		}
		return os.WriteFile(path, []byte(texts[n]), 0o600)
	}
	id := queueDraft(t, e.home, comment("hi"))
	if r := runWith(e); r.code != 0 {
		t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
	}
	for _, s := range []string{
		"? \n" + errNoEditor.Error() + "\n[a]pprove",
		"? \nNo changes.\n[a]pprove",
		"? \nThe edit was not used: the body is empty\n[a]pprove",
	} {
		if !strings.Contains(ft.shown(), s) {
			t.Errorf("terminal lacks %q", s)
		}
	}
	if strings.Contains(ft.shown(), "Edited.") || len(f.bodies) != 0 || states(t, e.home)[id].State != draft.Pending {
		t.Fatalf("terminal:\n%s", ft.shown())
	}
}

// An edit can clear what blocked a draft: here, a control character.
func TestReviewEditUnblocks(t *testing.T) {
	ft := answers(t, oldPass, "e", "a")
	e, f := reviewEnv(t, ft, sendRoutes(created(`{"html_url":"`+commentURL+`","user":{"login":"Sriram-PR"}}`)))
	var opened string
	e.edit = writes("clean\n", &opened)
	queueDraft(t, e.home, comment("bell \a"))
	if r := runWith(e); r.code != 0 {
		t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
	}
	if !strings.Contains(ft.shown(), "ghv-apply will not send this draft:") || !strings.Contains(ft.shown(), "[e]dit, [r]eject, [s]kip, or [q]uit? ") ||
		len(f.bodies) != 1 || f.bodies[0] != `{"body":"clean"}` {
		t.Fatalf("sent %q, terminal:\n%s", f.bodies, ft.shown())
	}
}
