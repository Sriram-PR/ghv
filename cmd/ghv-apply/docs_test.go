package main

import (
	"strings"
	"testing"

	"github.com/Sriram-PR/ghv/internal/doccmd"
)

// lineError is why ghv-apply refuses args (from "ghv-apply" on), or "".
func lineError(args []string) string {
	if len(args) == 1 || args[1] == "-h" || args[1] == "--help" || args[1] == "help" {
		return ""
	}
	for _, c := range commands {
		if c.name == args[1] {
			return c.checkArgs(args[2:])
		}
	}
	return "unknown command " + args[1]
}

func TestLineError(t *testing.T) {
	for _, c := range []struct {
		line, want string
	}{
		{"ghv-apply", ""},
		{"ghv-apply doctor --human-user me --login me --json", ""},
		{"ghv-apply log --verify", ""},
		{"ghv-apply doctor --human-user", "doctor --human-user needs a value"},
		{"ghv-apply init --json", "init takes no arguments"},
		{"ghv-apply log --bogus", "log takes only --verify"},
		{"ghv-apply frobnicate", "unknown command frobnicate"},
	} {
		if got := lineError(strings.Fields(c.line)); got != c.want {
			t.Errorf("%s: got %q, want %q", c.line, got, c.want)
		}
	}
}

func TestDocCommandLines(t *testing.T) {
	lines, err := doccmd.Shipped("../..")
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range lines {
		if l.Args[0] != "ghv-apply" {
			continue
		}
		if msg := lineError(l.Args); msg != "" {
			t.Errorf("%s:%d: %s: %s", l.File, l.N, strings.Join(l.Args, " "), msg)
		}
	}
}
