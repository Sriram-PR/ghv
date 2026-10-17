package read

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"
)

var pruneNow = time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)

func plantEntry(t *testing.T, dir, name string, age time.Duration) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, []byte(`{"etag":"\"e\"","body":"e30="}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(p, pruneNow.Add(-age), pruneNow.Add(-age)); err != nil {
		t.Fatal(err)
	}
	return p
}

func exists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func TestCacheStorePrunesEntriesUnusedFor30Days(t *testing.T) {
	dir := t.TempDir()
	old := plantEntry(t, dir, "old", 31*24*time.Hour)
	fresh := plantEntry(t, dir, "fresh", 29*24*time.Hour)
	c := cachedClient(t, "ghp_x", dir, &etagServer{etag: `"n"`, body: `{}`})
	c.now = func() time.Time { return pruneNow }
	if _, err := c.Get(context.Background(), "repos/o/r", ""); err != nil {
		t.Fatal(err)
	}
	if exists(old) || !exists(fresh) || !exists(filepath.Join(dir, cacheKey("ghp_x", "", "https://api.github.com/repos/o/r"))) {
		t.Fatalf("old kept %v, fresh kept %v", exists(old), exists(fresh))
	}
}

func TestCachePrunesAtMostOnceADay(t *testing.T) {
	dir := t.TempDir()
	stamp := plantEntry(t, dir, ".pruned", 23*time.Hour)
	old := plantEntry(t, dir, "old", 31*24*time.Hour)
	c := cachedClient(t, "ghp_x", dir, &etagServer{etag: `"n"`, body: `{}`})
	c.now = func() time.Time { return pruneNow }
	c.Get(context.Background(), "repos/o/r", "")
	if !exists(old) {
		t.Fatal("pruned within a day of the last prune")
	}
	os.Chtimes(stamp, pruneNow.Add(-25*time.Hour), pruneNow.Add(-25*time.Hour))
	c.Get(context.Background(), "repos/o/r2", "")
	fi, err := os.Stat(stamp)
	if exists(old) || err != nil || !fi.ModTime().Equal(pruneNow) {
		t.Fatalf("old kept %v, stamp %v %v", exists(old), fi, err)
	}
}

func TestCacheHitKeepsEntryAlive(t *testing.T) {
	dir := t.TempDir()
	s := &etagServer{etag: `"e"`, body: `{}`}
	c := cachedClient(t, "ghp_x", dir, s)
	c.now = func() time.Time { return pruneNow }
	c.Get(context.Background(), "repos/o/r", "")
	entry := filepath.Join(dir, cacheKey("ghp_x", "", "https://api.github.com/repos/o/r"))
	os.Chtimes(entry, pruneNow.Add(-31*24*time.Hour), pruneNow.Add(-31*24*time.Hour))
	os.Chtimes(filepath.Join(dir, ".pruned"), pruneNow.Add(-25*time.Hour), pruneNow.Add(-25*time.Hour))
	if _, err := c.Get(context.Background(), "repos/o/r", ""); err != nil || s.matched != 1 {
		t.Fatalf("err %v, matched %d", err, s.matched)
	}
	c.Get(context.Background(), "repos/o/r2", "")
	if fi, err := os.Stat(entry); err != nil || !fi.ModTime().Equal(pruneNow) {
		t.Fatalf("entry after a 304 hit: %v %v", fi, err)
	}
}

func TestCachePruneStampDoesNotFollowSymlinks(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(t.TempDir(), "precious")
	os.WriteFile(target, []byte("keep"), 0o600)
	os.Chtimes(target, pruneNow.Add(-48*time.Hour), pruneNow.Add(-48*time.Hour))
	if err := os.Symlink(target, filepath.Join(dir, ".pruned")); err != nil {
		t.Fatal(err)
	}
	c := cachedClient(t, "ghp_x", dir, &etagServer{etag: `"n"`, body: `{}`})
	c.now = func() time.Time { return pruneNow }
	c.Get(context.Background(), "repos/o/r", "")
	if b, _ := os.ReadFile(target); string(b) != "keep" {
		t.Fatalf("symlink target now %q", b)
	}
}
