package main

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGitHubRepo(t *testing.T) {
	tests := map[string]string{
		"https://github.com/cli/cli.git":        "cli/cli",
		"https://github.com/cli/cli":            "cli/cli",
		"https://github.com/cli/cli/":           "cli/cli",
		"https://user@github.com/Sriram-PR/ghv": "Sriram-PR/ghv",
		"http://github.com/o/r.git":             "o/r",
		"ssh://git@github.com/o/r.git":          "o/r",
		"git@github.com:o/r.git":                "o/r",
		"github.com:o/r":                        "o/r",
		"https://GitHub.com/o/r":                "o/r",
		"https://gitlab.com/o/r":                "",
		"https://github.com.evil/o/r":           "",
		"git@evil.com:github.com/o/r":           "",
		"https://github.com/o":                  "",
		"https://github.com/o/r/extra":          "",
		"https://github.com/-o/r":               "",
		"https://github.com/o/..":               "",
		"/srv/git/o/r":                          "",
	}
	for in, want := range tests {
		got, ok := githubRepo(in)
		if got != want || ok != (want != "") {
			t.Errorf("githubRepo(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}
}

func TestRemoteURL(t *testing.T) {
	cfg := `[core]
	bare = false
[remote "upstream"]
	url = https://github.com/up/r.git
# [remote "origin"]
[Remote "origin"]
	fetch = +refs/heads/*:refs/remotes/origin/*
	URL = "git@github.com:me/r.git" ; comment
	url = https://github.com/second/r
[branch "main"]
	remote = origin
`
	if got := remoteURL([]byte(cfg), "origin"); got != "git@github.com:me/r.git" {
		t.Fatalf("got %q", got)
	}
	if got := remoteURL([]byte(cfg), "ORIGIN"); got != "" {
		t.Fatalf("subsection match must be case-sensitive, got %q", got)
	}
	commented := "[remote \"origin\"] # main remote\n\turl = https://github.com/o/r\n"
	if got := remoteURL([]byte(commented), "origin"); got != "https://github.com/o/r" {
		t.Fatalf("header with a trailing comment: got %q", got)
	}
}

func writeFile(t *testing.T, p, s string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestInferRepo(t *testing.T) {
	root := t.TempDir()
	writeFile(t, filepath.Join(root, "main", ".git", "config"), "[remote \"origin\"]\n\turl = https://github.com/o/r.git\n")
	sub := filepath.Join(root, "main", "a", "b")
	os.MkdirAll(sub, 0o755)
	if got, err := inferRepo(sub); err != nil || got != "o/r" {
		t.Fatalf("subdir: %q, %v", got, err)
	}
	wt := filepath.Join(root, "main", ".git", "worktrees", "wt")
	writeFile(t, filepath.Join(wt, "commondir"), "../..\n")
	writeFile(t, filepath.Join(root, "wt", ".git"), "gitdir: "+wt+"\n")
	if got, err := inferRepo(filepath.Join(root, "wt")); err != nil || got != "o/r" {
		t.Fatalf("worktree: %q, %v", got, err)
	}
	writeFile(t, filepath.Join(root, "rel", ".git"), "gitdir: ../main/.git\n")
	if got, err := inferRepo(filepath.Join(root, "rel")); err != nil || got != "o/r" {
		t.Fatalf("relative gitdir: %q, %v", got, err)
	}
	writeFile(t, filepath.Join(root, "gl", ".git", "config"), "[remote \"origin\"]\n\turl = https://gitlab.com/o/r.git\n")
	if _, err := inferRepo(filepath.Join(root, "gl")); err == nil {
		t.Fatal("gitlab origin accepted")
	}
	writeFile(t, filepath.Join(root, "none", ".git", "config"), "[core]\n")
	if _, err := inferRepo(filepath.Join(root, "none")); err == nil {
		t.Fatal("missing origin accepted")
	}
}

func TestValidRepo(t *testing.T) {
	for s, want := range map[string]bool{
		"o/r": true, "Sriram-PR/ghv": true, "o/r.js": true, "o/.r": true,
		"o": false, "o/r/x": false, "-o/r": false, "o/..": false, "o/.": false, "o/r r": false, "/r": false, "o/": false,
	} {
		if validRepo(s) != want {
			t.Errorf("validRepo(%q) = %v", s, !want)
		}
	}
}
