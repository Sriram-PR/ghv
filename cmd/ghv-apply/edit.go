package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/Sriram-PR/ghv/internal/draft"
)

var errNoEditor = errors.New("no editor: set VISUAL or EDITOR to an absolute path, such as /usr/bin/vi, before running ghv-apply")

// editable reports whether a draft has text the reviewer can change.
func editable(d draft.Draft) bool { return d.Kind() != draft.KindFork && d.Kind() != "" }

// textFields says which of title and body a draft carries.
func textFields(d draft.Draft) (title, body *string) {
	switch f := d.Fields.(type) {
	case draft.PRCreateFields:
		return &f.Title, &f.Body
	case draft.PREditFields:
		return f.Title, f.Body
	case draft.CommentFields:
		return nil, &f.Body
	case draft.ReviewReplyFields:
		return nil, &f.Body
	case draft.IssueCreateFields:
		return &f.Title, &f.Body
	}
	return nil, nil
}

// editText is what the editor opens: the title on the first line, a blank
// line, then the body, with only the fields the draft has. A final newline
// is added and taken off again, so an unchanged file gives back the draft.
func editText(d draft.Draft) string {
	title, body := textFields(d)
	switch {
	case title != nil && body != nil:
		return *title + "\n\n" + *body + "\n"
	case title != nil:
		return *title + "\n"
	case body != nil:
		return *body + "\n"
	}
	return ""
}

// applyEdit puts the edited text back into a copy of d and checks the
// result as ghv checks a new draft.
func applyEdit(d draft.Draft, text string) (draft.Draft, error) {
	text = strings.TrimSuffix(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
	hasTitle, hasBody := textFields(d)
	var title, body string
	switch {
	case hasTitle != nil && hasBody != nil:
		var rest string
		title, rest, _ = strings.Cut(text, "\n")
		if rest != "" && !strings.HasPrefix(rest, "\n") {
			return d, errors.New("leave the second line empty: the title is the first line, and the body starts on the third")
		}
		body = strings.TrimPrefix(rest, "\n")
	case hasTitle != nil:
		if strings.Contains(text, "\n") {
			return d, errors.New("the title must be one line")
		}
		title = text
	default:
		body = text
	}
	switch f := d.Fields.(type) {
	case draft.PRCreateFields:
		f.Title, f.Body = title, body
		d.Fields = f
	case draft.PREditFields:
		if f.Title != nil {
			f.Title = &title
		}
		if f.Body != nil {
			f.Body = &body
		}
		d.Fields = f
	case draft.CommentFields:
		f.Body = body
		d.Fields = f
	case draft.ReviewReplyFields:
		f.Body = body
		d.Fields = f
	case draft.IssueCreateFields:
		f.Title, f.Body = title, body
		d.Fields = f
	}
	if (d.Kind() == draft.KindComment || d.Kind() == draft.KindReviewReply) && strings.TrimSpace(body) == "" {
		return d, errors.New("the body is empty")
	}
	return d, draft.Validate(d)
}

// editFile writes text to a private file, opens it in the editor, and
// reads the result once.
func editFile(edit func(path string) error, id, text string) (string, error) {
	dir, err := os.MkdirTemp("", "ghv-apply-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, id+".md")
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		return "", err
	}
	if err := edit(path); err != nil {
		return "", err
	}
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return "", err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, draft.MaxSize+1))
	if err != nil {
		return "", err
	}
	if len(data) > draft.MaxSize {
		return "", fmt.Errorf("the edited text is larger than %d bytes", draft.MaxSize)
	}
	return string(data), nil
}

// editorCommand is VISUAL, else EDITOR. harden keeps them only when they
// start with an absolute path, since PATH is gone.
func editorCommand() ([]string, error) {
	ed := os.Getenv("VISUAL")
	if ed == "" {
		ed = os.Getenv("EDITOR")
	}
	argv := strings.Fields(ed)
	if len(argv) == 0 || !filepath.IsAbs(argv[0]) {
		return nil, errNoEditor
	}
	return argv, nil
}

// runEditor runs argv with path on the terminal tty.
func runEditor(argv []string, tty *os.File, path string) error {
	cmd := exec.Command(argv[0], append(argv[1:], path)...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = tty, tty, tty
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("the editor failed: %v", err)
	}
	return nil
}

// editOnTTY is env.edit in main.
func editOnTTY(path string) error {
	argv, err := editorCommand()
	if err != nil {
		return err
	}
	tty, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return err
	}
	defer tty.Close()
	return runEditor(argv, tty, path)
}
