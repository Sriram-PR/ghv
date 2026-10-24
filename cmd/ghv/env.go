package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/user"
	"path/filepath"
	"runtime/debug"
	"time"

	"github.com/Sriram-PR/ghv/internal/draft"
	"github.com/Sriram-PR/ghv/internal/read"
)

// env is everything ghv takes from its process, so tests can supply it.
type env struct {
	stdin          io.Reader
	stdout, stderr io.Writer
	home           string
	passwdHome     string // "" when the passwd lookup failed
	sharedRoot     string // draft.SharedRoot; "" for none
	getenv         func(string) string
	dir            string
	transport      http.RoundTripper
	now            func() time.Time
	sleep          func(context.Context, time.Duration) error
	git            func(ctx context.Context, dir string, args ...string) ([]byte, error)
	// For doctor: the process, the user, and a terminal to ask on.
	goos       string
	uid, pid   int
	username   string
	proc       string // "/proc", or "" where there is none
	runtimeDir string // "/run/user" on Linux, else ""
	tmpDir     string // "/tmp", or "/private/tmp" on macOS
	systemBin  string // where ghv-apply is installed: /usr/local/bin
	lookupUser func(name string) (*user.User, error)
	confirm    func(prompt string) (bool, error)
	runProgram func(ctx context.Context, stdin, name string, args ...string) ([]byte, error)
	buildInfo  func() (*debug.BuildInfo, bool) // nil: debug.ReadBuildInfo
}

// wait sleeps for d, or until ctx is done.
func (e env) wait(ctx context.Context, d time.Duration) error {
	if e.sleep != nil {
		return e.sleep(ctx, d)
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func (e env) xdg(name, fallback string) string {
	if v := e.getenv(name); filepath.IsAbs(v) {
		return filepath.Join(v, "ghv")
	}
	return filepath.Join(e.home, fallback, "ghv")
}

func (e env) tokenPath() string {
	return filepath.Join(e.xdg("XDG_CONFIG_HOME", ".config"), "read-token")
}

func (e env) cacheDir() string { return e.xdg("XDG_CACHE_HOME", ".cache") }

// draftQueue is the shared queue when there is one. The home queue
// ignores XDG_DATA_HOME: ghv-apply finds it from the passwd entry's home,
// so ghv must put it under the same home. A $HOME pointing elsewhere is
// refused: drafts there would never reach the human.
func (e env) draftQueue() (draft.Queue, error) {
	q, err := draft.Locate(e.sharedRoot, e.passwdHome)
	if err != nil || q.Shared() {
		return q, err
	}
	if e.passwdHome == "" {
		return draft.Queue{}, errors.New("no passwd entry for this user; ghv-apply reads drafts from the passwd home")
	}
	if !sameDir(e.home, e.passwdHome) {
		return draft.Queue{}, fmt.Errorf("$HOME is %s, not the passwd home %s; ghv-apply reads drafts from the passwd home", e.home, e.passwdHome)
	}
	return q, nil
}

func sameDir(a, b string) bool {
	if a == b {
		return true
	}
	ai, err := os.Stat(a)
	if err != nil {
		return false
	}
	bi, err := os.Stat(b)
	return err == nil && os.SameFile(ai, bi)
}

// app is what a command runs with: the environment, its parsed command
// line, and a read client made on first use.
type app struct {
	env
	inv    *invocation
	client *read.Client
}

func (a *app) read() (*read.Client, error) {
	if a.client == nil {
		tok, err := read.LoadToken(a.tokenPath())
		if err != nil {
			return nil, err
		}
		a.client = read.NewClient(read.Options{Token: tok, Transport: a.transport, CacheDir: a.cacheDir()})
	}
	return a.client, nil
}

func (a *app) json() bool { return a.inv.bool("json") }
