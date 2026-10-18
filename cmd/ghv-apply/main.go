package main

import (
	"context"
	"crypto/rand"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/Sriram-PR/ghv/internal/harden"
	"github.com/Sriram-PR/ghv/internal/sanitize"
)

// env is everything a command takes from its process, so tests can
// supply it.
type env struct {
	stdout, stderr io.Writer
	home           string // from the passwd entry, never $HOME
	openTTY        func() (terminal, error)
	edit           func(path string) error // opens path in the reviewer's editor
	client         *http.Client
	rand           io.Reader
	now            func() time.Time
}

type command struct {
	name, help string
	run        func(context.Context, env) error
}

// commands is filled by each command file's init.
var commands []command

func main() {
	if err := harden.Init(); err != nil {
		fmt.Fprintln(os.Stderr, "ghv-apply:", err)
		os.Exit(1)
	}
	if err := harden.Process(); err != nil {
		fmt.Fprintln(os.Stderr, "ghv-apply: warning:", err)
	}
	home, err := harden.Home()
	if err != nil {
		fmt.Fprintln(os.Stderr, "ghv-apply:", err)
		os.Exit(1)
	}
	os.Exit(run(context.Background(), os.Args[1:], env{
		stdout:  os.Stdout,
		stderr:  os.Stderr,
		home:    home,
		openTTY: openDevTTY,
		edit:    editOnTTY,
		client:  harden.Client(harden.Transport()),
		rand:    rand.Reader,
		now:     time.Now,
	}))
}

func run(ctx context.Context, args []string, e env) int {
	if len(args) == 0 {
		return report(e, runReview(ctx, e))
	}
	switch args[0] {
	case "-h", "--help", "help":
		fmt.Fprint(e.stdout, usage())
		return 0
	}
	for _, c := range commands {
		if c.name != args[0] {
			continue
		}
		if len(args) > 1 {
			fmt.Fprintf(e.stderr, "ghv-apply: %s takes no arguments\n", c.name)
			return 2
		}
		return report(e, c.run(ctx, e))
	}
	fmt.Fprintf(e.stderr, "ghv-apply: unknown command %q; run ghv-apply --help\n", sanitize.Line(args[0]))
	return 2
}

func report(e env, err error) int {
	if err != nil {
		fmt.Fprintf(e.stderr, "ghv-apply: %s\n", sanitize.Line(err.Error()))
		return 1
	}
	return 0
}

func usage() string {
	var b strings.Builder
	b.WriteString("Usage: ghv-apply [command]\n\nWith no command, review the drafts in the queue.\n\nCommands:\n")
	width := 0
	for _, c := range commands {
		width = max(width, len(c.name))
	}
	for _, c := range commands {
		fmt.Fprintf(&b, "  %-*s  %s\n", width, c.name, c.help)
	}
	return b.String()
}
