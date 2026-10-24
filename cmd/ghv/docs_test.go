package main

import (
	"errors"
	"strings"
	"testing"

	"github.com/Sriram-PR/ghv/internal/doccmd"
)

// lineError is why ghv's parser refuses args (from "ghv" on), or "".
func lineError(cmds []*command, args []string) string {
	_, err := parse(cmds, args[1:])
	var hr helpRequest
	if err == nil || errors.As(err, &hr) {
		return ""
	}
	return err.Error()
}

func TestLineError(t *testing.T) {
	for _, c := range []struct {
		line, want string
	}{
		{"ghv pr view 12 -R cli/cli", ""},
		{"ghv search issues is:open --limit 5 --json", ""},
		{"ghv pr view --help", ""},
		{"ghv pr view 12 --bogus", `unknown flag "--bogus" for ghv pr view`},
		{"ghv pr", "ghv pr needs a subcommand: activity, checkout, checks, commits, create, diff, edit, list, view"},
		{"ghv frobnicate", `unknown command "frobnicate"`},
	} {
		if got := lineError(registry, strings.Fields(c.line)); got != c.want {
			t.Errorf("%s: got %q, want %q", c.line, got, c.want)
		}
	}
}

// The docs must show runnable examples, not usage notation such as
// [--json]: each line goes through the same parser as a real command.
func TestDocCommandLines(t *testing.T) {
	lines, err := doccmd.Shipped("../..")
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range lines {
		if l.Args[0] != "ghv" {
			continue
		}
		if msg := lineError(registry, l.Args); msg != "" {
			t.Errorf("%s:%d: %s: %s", l.File, l.N, strings.Join(l.Args, " "), msg)
		}
	}
}
