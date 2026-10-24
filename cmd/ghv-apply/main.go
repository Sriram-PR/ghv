package main

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/user"
	"runtime"
	"runtime/debug"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Sriram-PR/ghv/internal/doctor"
	"github.com/Sriram-PR/ghv/internal/draft"
	"github.com/Sriram-PR/ghv/internal/harden"
	"github.com/Sriram-PR/ghv/internal/sanitize"
)

// env is everything a command takes from its process, so tests can
// supply it.
type env struct {
	stdout, stderr io.Writer
	home           string // from the passwd entry, never $HOME
	sharedRoot     string // draft.SharedRoot; "" for none
	openTTY        func() (terminal, error)
	edit           func(path string) error // opens path in the reviewer's editor
	client         *http.Client
	rand           io.Reader
	now            func() time.Time
	afterFunc      func(time.Duration, func()) (stop func() bool)
	sleep          func(time.Duration)
	args           []string // the command's flags, from those it lists
	exit           func(code int)
	// For doctor: the process and the user.
	goos           string
	uid, pid, ppid int
	username       string
	proc           string // "/proc", or "" where there is none
	runtimeDir     string // "/run/user" on Linux, else ""
	tmpDir         string // "/tmp", or "/private/tmp" on macOS
	executable     string
	lookupUser     func(name string) (*user.User, error)
	runProgram     func(ctx context.Context, stdin, name string, args ...string) ([]byte, error)
	buildInfo      func() (*debug.BuildInfo, bool) // nil: debug.ReadBuildInfo
}

type command struct {
	name, help string
	flags      []string // the only arguments it takes
	values     []string // flags that take the next argument as their value
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
	var username string
	if u, err := user.LookupId(strconv.Itoa(os.Getuid())); err == nil {
		username = u.Username
	}
	proc, runtimeDir, tmpDir := "", "", "/private/tmp"
	if runtime.GOOS == "linux" {
		proc, runtimeDir, tmpDir = "/proc", "/run/user", "/tmp"
	}
	exe, _ := os.Executable()
	os.Exit(run(context.Background(), os.Args[1:], env{
		stdout:     os.Stdout,
		stderr:     os.Stderr,
		home:       home,
		sharedRoot: draft.SharedRoot,
		openTTY:    openDevTTY,
		edit:       editOnTTY,
		client:     harden.Client(harden.Transport()),
		rand:       rand.Reader,
		now:        time.Now,
		afterFunc: func(d time.Duration, f func()) func() bool {
			return time.AfterFunc(d, f).Stop
		},
		sleep:      time.Sleep,
		exit:       os.Exit,
		goos:       runtime.GOOS,
		uid:        os.Getuid(),
		pid:        os.Getpid(),
		ppid:       os.Getppid(),
		username:   username,
		proc:       proc,
		runtimeDir: runtimeDir,
		tmpDir:     tmpDir,
		executable: exe,
		lookupUser: user.Lookup,
		runProgram: programRunner(home, runtimeDir, os.Getuid(), gitPaths),
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
		if msg := c.checkArgs(args[1:]); msg != "" {
			fmt.Fprintln(e.stderr, "ghv-apply: "+msg)
			return 2
		}
		e.args = args[1:]
		return report(e, c.run(ctx, e))
	}
	fmt.Fprintf(e.stderr, "ghv-apply: unknown command %q; run ghv-apply --help\n", sanitize.Line(args[0]))
	return 2
}

// checkArgs is the usage error for rest, the arguments after c's name, or
// "": each must be one of c's flags, or a value flag and its value.
func (c command) checkArgs(rest []string) string {
	for i := 0; i < len(rest); i++ {
		a := rest[i]
		if slices.Contains(c.flags, a) {
			continue
		}
		if slices.Contains(c.values, a) {
			if i+1 == len(rest) {
				return fmt.Sprintf("%s %s needs a value", c.name, a)
			}
			i++
			continue
		}
		if len(c.flags)+len(c.values) == 0 {
			return c.name + " takes no arguments"
		}
		return fmt.Sprintf("%s takes only %s", c.name, c.usage())
	}
	return ""
}

// usage lists the command's flags, a value flag as FLAG VALUE.
func (c command) usage() string {
	parts := slices.Clone(c.flags)
	for _, v := range c.values {
		parts = append(parts, v+" VALUE")
	}
	return strings.Join(parts, ", ")
}

// report exits 4 when doctor found something HIGH, else 1 on an error.
func report(e env, err error) int {
	if err == nil {
		return 0
	}
	fmt.Fprintf(e.stderr, "ghv-apply: %s\n", sanitize.Line(err.Error()))
	if errors.As(err, new(doctor.HighError)) {
		return 4
	}
	return 1
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
