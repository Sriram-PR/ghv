package main

import (
	"io"
	"net/http"
	"path/filepath"
	"time"

	"github.com/Sriram-PR/ghv/internal/read"
)

// env is everything ghv takes from its process, so tests can supply it.
type env struct {
	stdout, stderr io.Writer
	home           string
	getenv         func(string) string
	dir            string
	transport      http.RoundTripper
	now            func() time.Time
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
