package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Sriram-PR/ghv/internal/doctor"
)

// fakeProgram writes an executable named name in a new directory that
// records its environment, arguments, and stdin, prints out, and exits
// with code.
func fakeProgram(t *testing.T, name, out string, code int) string {
	t.Helper()
	dir := t.TempDir()
	script := "#!/bin/sh\nd=$(dirname \"$0\")\nenv > \"$d/env.txt\"\necho \"$@\" > \"$d/args.txt\"\ncat > \"$d/stdin.txt\"\nprintf '" + out + "'\nexit " + string(rune('0'+code)) + "\n"
	if err := os.WriteFile(filepath.Join(dir, name), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestRunProgram(t *testing.T) {
	dir := fakeProgram(t, "git", `password=p\n`, 3)
	t.Setenv("PATH", dir+":/usr/bin:/bin")
	t.Setenv("GIT_DIR", "/elsewhere")
	t.Setenv("SSH_ASKPASS", "/usr/bin/ask")
	t.Setenv("GCM_INTERACTIVE", "always")
	out, err := runProgram(context.Background(), "in\n", "git", "credential", "fill")
	if string(out) != "password=p\n" || err != (doctor.ExitError{Code: 3}) {
		t.Fatalf("out %q, err %v", out, err)
	}
	read := func(n string) string { b, _ := os.ReadFile(filepath.Join(dir, n)); return string(b) }
	env := read("env.txt")
	if read("args.txt") != "credential fill\n" || read("stdin.txt") != "in\n" ||
		!strings.Contains(env, "\nGIT_TERMINAL_PROMPT=0\n") || !strings.Contains(env, "\nGCM_INTERACTIVE=never\n") ||
		strings.Contains(env, "GIT_DIR=") || strings.Contains(env, "SSH_ASKPASS=") || strings.Contains(env, "=always") {
		t.Fatalf("args %q, stdin %q, env:\n%s", read("args.txt"), read("stdin.txt"), env)
	}
}
