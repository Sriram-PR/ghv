package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeProgram writes an executable named name in a new directory that
// records its environment, arguments, stdin, and working directory, prints
// out, and exits with code.
func fakeProgram(t *testing.T, name, out string, code int) string {
	t.Helper()
	dir := t.TempDir()
	script := "#!/bin/sh\nd=$(dirname \"$0\")\nenv > \"$d/env.txt\"\npwd > \"$d/cwd.txt\"\necho \"$@\" > \"$d/args.txt\"\ncat > \"$d/stdin.txt\"\nprintf '" + out + "'\nexit " + string(rune('0'+code)) + "\n"
	if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestProgramRunner(t *testing.T) {
	dir := fakeProgram(t, "git", `password=p\n`, 0)
	home, runtimeDir := t.TempDir(), t.TempDir()
	os.Mkdir(filepath.Join(runtimeDir, "1001"), 0o700)
	t.Setenv("GHV_TEST_LEAK", "1")
	run := programRunner(home, runtimeDir, 1001, []string{"/nonexistent/git", filepath.Join(dir, "git")})
	out, err := run(context.Background(), "in\n", "git", "credential", "fill")
	if string(out) != "password=p\n" || err != nil {
		t.Fatalf("out %q, err %v", out, err)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "env.txt"))
	env := "\n" + string(b)
	for _, want := range []string{"HOME=" + home, "PATH=/usr/bin:/bin", "GIT_TERMINAL_PROMPT=0", "GCM_INTERACTIVE=never", "XDG_RUNTIME_DIR=" + filepath.Join(runtimeDir, "1001")} {
		if !strings.Contains(env, "\n"+want+"\n") {
			t.Errorf("no %s in:%s", want, env)
		}
	}
	if strings.Contains(env, "GHV_TEST_LEAK") {
		t.Errorf("the parent's environment leaked:%s", env)
	}
	run = programRunner(home, "", 1001, []string{"/nonexistent/git"})
	if _, err := run(context.Background(), "", "git", "version"); err == nil || err.Error() != "no git at /nonexistent/git" {
		t.Fatalf("err %v", err)
	}
	if _, err := programRunner(home, "", 1001, nil)(context.Background(), "", "rm", "-rf", "/"); err == nil || err.Error() != `doctor runs only git and security, not "rm"` {
		t.Fatalf("err %v", err)
	}
}

// A repository in the working directory could name a credential.helper
// that git would run, so git runs from /.
func TestProgramRunnerLeavesTheWorkingDirectory(t *testing.T) {
	dir := fakeProgram(t, "git", "", 0)
	t.Chdir(t.TempDir())
	if _, err := programRunner(t.TempDir(), "", 1001, []string{filepath.Join(dir, "git")})(context.Background(), "", "git", "version"); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "cwd.txt")); string(b) != "/\n" {
		t.Fatalf("git ran in %q", b)
	}
}
