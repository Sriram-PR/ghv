package main

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"
	"time"
)

type result struct {
	code           int
	stdout, stderr string
}

func testEnv(t *testing.T) env {
	t.Helper()
	return env{
		stdout:  &bytes.Buffer{},
		stderr:  &bytes.Buffer{},
		home:    t.TempDir(),
		openTTY: func() (terminal, error) { return nil, errors.New("no terminal in this test") },
		now:     func() time.Time { return time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC) },
	}
}

func runWith(e env, args ...string) result {
	e.stdout.(*bytes.Buffer).Reset()
	e.stderr.(*bytes.Buffer).Reset()
	code := run(context.Background(), args, e)
	return result{code, e.stdout.(*bytes.Buffer).String(), e.stderr.(*bytes.Buffer).String()}
}

func withCommand(t *testing.T, c command) {
	t.Helper()
	commands = append(commands, c)
	t.Cleanup(func() { commands = commands[:len(commands)-1] })
}

func TestUsage(t *testing.T) {
	withCommand(t, command{name: "zz-test", help: "a test command", run: func(context.Context, env) error { return nil }})
	e := testEnv(t)
	for _, arg := range []string{"-h", "--help", "help"} {
		r := runWith(e, arg)
		if r.code != 0 || !strings.HasPrefix(r.stdout, "Usage: ghv-apply <command>\n") ||
			!strings.Contains(r.stdout, "\n  zz-test  a test command\n") || r.stderr != "" {
			t.Fatalf("%s: exit %d, stdout %q, stderr %q", arg, r.code, r.stdout, r.stderr)
		}
	}
	if r := runWith(e); r.code != 2 || r.stdout != "" || !strings.HasPrefix(r.stderr, "Usage: ghv-apply") {
		t.Fatalf("no arguments: exit %d, stdout %q, stderr %q", r.code, r.stdout, r.stderr)
	}
	if r := runWith(e, "nope"); r.code != 2 || r.stderr != "ghv-apply: unknown command \"nope\"; run ghv-apply --help\n" {
		t.Fatalf("unknown: exit %d, stderr %q", r.code, r.stderr)
	}
	if r := runWith(e, "zz-test", "extra"); r.code != 2 || !strings.Contains(r.stderr, "takes no arguments") {
		t.Fatalf("extra argument: exit %d, stderr %q", r.code, r.stderr)
	}
}

func TestCommandErrorsAreSanitized(t *testing.T) {
	withCommand(t, command{name: "zz-fail", run: func(context.Context, env) error {
		return errors.New("GitHub said \x1b]52;c;evil\x07")
	}})
	r := runWith(testEnv(t), "zz-fail")
	if r.code != 1 || strings.ContainsRune(r.stderr, 0x1b) || !strings.HasPrefix(r.stderr, "ghv-apply: GitHub said <U+001B>") {
		t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
	}
}

// TestOpenTTYChild is the process TestOpenTTYNeedsATerminal starts in a
// new session, which has no controlling terminal.
func TestOpenTTYChild(t *testing.T) {
	if flag.Arg(0) != "tty-child" {
		t.Skip("helper process for TestOpenTTYNeedsATerminal")
	}
	tty, err := openDevTTY()
	if err == nil {
		tty.Close()
		fmt.Print("opened")
		os.Exit(0)
	}
	fmt.Print(err)
	os.Exit(0)
}

func TestOpenTTYNeedsATerminal(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestOpenTTYChild$", "--", "tty-child")
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Stdin = strings.NewReader(strings.Repeat("yes\n", 100))
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("child: %v\n%s", err, out)
	}
	if !strings.HasPrefix(string(out), "no terminal: passphrases and decisions come from /dev/tty only") {
		t.Fatalf("child said %q", out)
	}
}
