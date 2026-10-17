package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

type gitCall struct {
	dir  string
	args []string
}

func fakeGit(calls *[]gitCall, out string, err error) func(context.Context, string, ...string) ([]byte, error) {
	return func(ctx context.Context, dir string, args ...string) ([]byte, error) {
		*calls = append(*calls, gitCall{dir, args})
		return []byte(out), err
	}
}

func checkoutEnv(t *testing.T, calls *[]gitCall, out string, err error) env {
	rt := newRoutes(t, nil)
	e, _, _ := testEnv(t, rt)
	writeFile(t, filepath.Join(e.dir, ".git", "config"), "[remote \"origin\"]\n\turl = https://github.com/o/r\n")
	e.git = fakeGit(calls, out, err)
	t.Cleanup(func() {
		if len(rt.uris()) != 0 {
			t.Errorf("pr checkout called the API: %q", rt.uris())
		}
	})
	return e
}

func TestPRCheckout(t *testing.T) {
	var calls []gitCall
	e := checkoutEnv(t, &calls, "", nil)
	r := runWith(t, registry, e, "pr", "checkout", "7")
	want := []string{
		"-c", "credential.helper=", "-c", "core.askPass=", "-c", "http.extraHeader=",
		"-c", "protocol.allow=never", "-c", "protocol.https.allow=always",
		"fetch", "https://github.com/o/r.git", "pull/7/head:pr-7",
	}
	if r.code != 0 || len(calls) != 1 || !slices.Equal(calls[0].args, want) || calls[0].dir != e.dir {
		t.Fatalf("exit %d, stderr %q, calls %q", r.code, r.stderr, calls)
	}
	if r.stdout != "fetched o/r#7 into branch pr-7; run git switch pr-7\n" {
		t.Fatalf("stdout %q", r.stdout)
	}
	r = runWith(t, registry, e, "pr", "checkout", "7", "-R", "up/r", "--json")
	if calls[1].args[11] != "https://github.com/up/r.git" || !strings.Contains(r.stdout, `"branch": "pr-7"`) {
		t.Fatalf("calls %q, stdout %s", calls, r.stdout)
	}
}

func TestPRCheckoutGitFails(t *testing.T) {
	var calls []gitCall
	e := checkoutEnv(t, &calls, "fatal: refusing to fetch into branch 'refs/heads/pr-7' \x1b[31mchecked out\n", errors.New("exit status 128"))
	r := runWith(t, registry, e, "pr", "checkout", "7")
	if r.code != 1 || r.stdout != "" || !strings.Contains(r.stderr, "git fetch failed: fatal: refusing to fetch into branch 'refs/heads/pr-7' <U+001B>[31mchecked out") {
		t.Fatalf("exit %d, stdout %q, stderr %q", r.code, r.stdout, r.stderr)
	}
}

func TestPRCheckoutNeedsACheckout(t *testing.T) {
	var calls []gitCall
	e := checkoutEnv(t, &calls, "", nil)
	os.RemoveAll(filepath.Join(e.dir, ".git"))
	r := runWith(t, registry, e, "pr", "checkout", "7", "-R", "o/r")
	if r.code != 2 || len(calls) != 0 || !strings.Contains(r.stderr, "run ghv pr checkout inside a git checkout") {
		t.Fatalf("exit %d, calls %q, stderr %q", r.code, calls, r.stderr)
	}
}

func TestRunGitEnvironment(t *testing.T) {
	got := gitEnv([]string{"PATH=/bin", "GIT_ASKPASS=/x", "SSH_ASKPASS=/y", "GIT_TERMINAL_PROMPT=1", "HOME=/h"})
	want := []string{"PATH=/bin", "HOME=/h", "GIT_TERMINAL_PROMPT=0"}
	if !slices.Equal(got, want) {
		t.Fatalf("gitEnv = %q", got)
	}
}
