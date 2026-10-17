package read

import (
	"context"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Sriram-PR/ghv/internal/read/readtest"
)

type etagServer struct {
	etag    string
	link    string
	body    string
	status  int
	ifNone  []string
	matched int
}

func (s *etagServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.ifNone = append(s.ifNone, r.Header.Get("If-None-Match"))
	w.Header().Set("X-Oauth-Scopes", "")
	if s.etag != "" {
		w.Header().Set("Etag", s.etag)
	}
	if s.link != "" {
		w.Header().Set("Link", s.link)
	}
	if s.etag != "" && r.Header.Get("If-None-Match") == s.etag {
		s.matched++
		w.WriteHeader(http.StatusNotModified)
		return
	}
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	if s.status != 0 {
		w.WriteHeader(s.status)
	}
	io.WriteString(w, s.body)
}

func cachedClient(t *testing.T, token, dir string, s *etagServer) *Client {
	t.Helper()
	c := NewClient(Options{Token: token, CacheDir: dir, Transport: readtest.Transport{Handler: s}})
	c.sleep = func(context.Context, time.Duration) error { return nil }
	return c
}

func TestCacheServes304FromDisk(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "ghv")
	s := &etagServer{etag: `W/"abc"`, link: `<https://api.github.com/x?page=2>; rel="next"`, body: `{"n":1}`}
	c := cachedClient(t, "ghp_x", dir, s)
	for i := 0; i < 2; i++ {
		resp, err := c.Get(context.Background(), "repos/o/r", "")
		if err != nil {
			t.Fatal(err)
		}
		if resp.Status != 200 || string(resp.Body) != `{"n":1}` || resp.Header.Get("Link") != s.link ||
			resp.Header.Get("Content-Type") != "application/json; charset=utf-8" {
			t.Fatalf("round %d: %d %q %v", i, resp.Status, resp.Body, resp.Header)
		}
	}
	if s.ifNone[0] != "" || s.ifNone[1] != `W/"abc"` || s.matched != 1 {
		t.Fatalf("If-None-Match sent: %q, matched %d", s.ifNone, s.matched)
	}
}

func TestCacheFilesArePrivateAndHoldNoToken(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "ghv")
	c := cachedClient(t, "ghp_secretvalue", dir, &etagServer{etag: `"e"`, body: `{}`})
	if _, err := c.Get(context.Background(), "repos/o/r", ""); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Stat(dir)
	if err != nil || fi.Mode().Perm() != 0o700 {
		t.Fatalf("dir: %v %v", fi, err)
	}
	entries := cacheFiles(t, dir)
	if len(entries) != 1 {
		t.Fatalf("%d cache files, want 1", len(entries))
	}
	if fi, err := os.Stat(filepath.Join(dir, ".pruned")); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("stamp: %v %v", fi, err)
	}
	p := filepath.Join(dir, entries[0].Name())
	fi, _ = os.Stat(p)
	raw, _ := os.ReadFile(p)
	if fi.Mode().Perm() != 0o600 || strings.Contains(string(raw), "secretvalue") || strings.Contains(entries[0].Name(), "secretvalue") {
		t.Fatalf("cache file %s mode %v holds the token", p, fi.Mode())
	}
}

func TestCacheSkips(t *testing.T) {
	tests := []struct {
		name  string
		token string
		s     *etagServer
	}{
		{"anonymous", "", &etagServer{etag: `"e"`, body: `{}`}},
		{"no ETag", "ghp_x", &etagServer{body: `{}`}},
		{"error response", "ghp_x", &etagServer{etag: `"e"`, status: 404, body: `{"message":"Not Found"}`}},
		{"too large", "ghp_x", &etagServer{etag: `"e"`, body: strings.Repeat("x", 101)}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dir := filepath.Join(t.TempDir(), "ghv")
			c := cachedClient(t, tt.token, dir, tt.s)
			c.maxCacheEntry = 100
			c.Get(context.Background(), "repos/o/r", "")
			c.Get(context.Background(), "repos/o/r", "")
			if entries, _ := os.ReadDir(dir); len(entries) != 0 || tt.s.ifNone[1] != "" {
				t.Fatalf("%d cache files, If-None-Match %q", len(entries), tt.s.ifNone)
			}
		})
	}
}

func TestCacheKeyedByTokenAcceptAndURL(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "ghv")
	s := &etagServer{etag: `"e"`, body: `{}`}
	cachedClient(t, "ghp_a", dir, s).Get(context.Background(), "repos/o/r", "")
	cachedClient(t, "ghp_b", dir, s).Get(context.Background(), "repos/o/r", "")
	cachedClient(t, "ghp_a", dir, s).Get(context.Background(), "repos/o/r", "application/vnd.github.diff")
	cachedClient(t, "ghp_a", dir, s).Get(context.Background(), "repos/o/r?page=2", "")
	for i, v := range s.ifNone {
		if v != "" {
			t.Fatalf("request %d sent If-None-Match %q from another key", i, v)
		}
	}
}

func TestCacheIgnoresCorruptEntry(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "ghv")
	s := &etagServer{etag: `"e"`, body: `{"ok":true}`}
	c := cachedClient(t, "ghp_x", dir, s)
	c.Get(context.Background(), "repos/o/r", "")
	os.WriteFile(filepath.Join(dir, cacheFiles(t, dir)[0].Name()), []byte("{not json"), 0o600)
	resp, err := c.Get(context.Background(), "repos/o/r", "")
	if err != nil || string(resp.Body) != `{"ok":true}` || s.ifNone[1] != "" {
		t.Fatalf("err = %v, body %q, If-None-Match %q", err, resp.Body, s.ifNone)
	}
}

// cacheFiles lists the cache entries in dir, leaving out the prune stamp.
func cacheFiles(t *testing.T, dir string) []os.DirEntry {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	return slices.DeleteFunc(entries, func(e os.DirEntry) bool { return e.Name() == ".pruned" })
}
