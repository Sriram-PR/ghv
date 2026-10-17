package read

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"time"
)

const (
	maxCacheEntry = 8 << 20
	cacheMaxAge   = 30 * 24 * time.Hour
)

// cacheEntry is one ETag-validated response in ~/.cache/ghv. Only ghv
// reads the cache; ghv-apply never does.
type cacheEntry struct {
	ETag        string `json:"etag"`
	ContentType string `json:"content_type,omitempty"`
	Link        string `json:"link,omitempty"`
	Body        []byte `json:"body"`
}

// cacheKey hashes the token in, so two tokens never share an entry, and
// the file name does not reveal the token.
func cacheKey(token, accept, url string) string {
	sum := sha256.Sum256([]byte("ghv-cache\x00" + token + "\x00" + accept + "\x00" + url))
	return hex.EncodeToString(sum[:])
}

func (c *Client) cacheLoad(key string) *cacheEntry {
	f, err := os.Open(filepath.Join(c.cacheDir, key))
	if err != nil {
		return nil
	}
	defer f.Close()
	raw, err := io.ReadAll(io.LimitReader(f, 2*c.maxCacheEntry+4096))
	if err != nil {
		return nil
	}
	var e cacheEntry
	if json.Unmarshal(raw, &e) != nil || e.ETag == "" {
		return nil
	}
	return &e
}

// cacheStore is best effort: a failure costs a rate-limit unit later, not
// a wrong answer, so it is not reported.
func (c *Client) cacheStore(key string, resp *Response) {
	etag := resp.Header.Get("Etag")
	if etag == "" || int64(len(resp.Body)) > c.maxCacheEntry {
		return
	}
	raw, err := json.Marshal(cacheEntry{
		ETag:        etag,
		ContentType: resp.Header.Get("Content-Type"),
		Link:        resp.Header.Get("Link"),
		Body:        resp.Body,
	})
	if err != nil {
		return
	}
	if err := os.MkdirAll(c.cacheDir, 0o700); err != nil {
		return
	}
	os.Chmod(c.cacheDir, 0o700)
	f, err := os.CreateTemp(c.cacheDir, ".tmp-")
	if err != nil {
		return
	}
	_, werr := f.Write(raw)
	cerr := f.Close()
	if werr != nil || cerr != nil || os.Rename(f.Name(), filepath.Join(c.cacheDir, key)) != nil {
		os.Remove(f.Name())
	}
	c.cachePrune()
}

// cacheTouch marks an entry as used, so cachePrune keeps it.
func (c *Client) cacheTouch(key string) {
	now := c.now()
	os.Chtimes(filepath.Join(c.cacheDir, key), now, now)
}

// cachePrune deletes entries unused for cacheMaxAge, at most once a day;
// the mtime of the .pruned stamp records the last run.
func (c *Client) cachePrune() {
	now := c.now()
	stamp := filepath.Join(c.cacheDir, ".pruned")
	if fi, err := os.Lstat(stamp); err == nil && now.Sub(fi.ModTime()) < 24*time.Hour {
		return
	}
	f, err := os.CreateTemp(c.cacheDir, ".tmp-")
	if err != nil {
		return
	}
	f.Close()
	if os.Chtimes(f.Name(), now, now) != nil || os.Rename(f.Name(), stamp) != nil {
		os.Remove(f.Name())
		return
	}
	entries, err := os.ReadDir(c.cacheDir)
	if err != nil {
		return
	}
	cutoff := now.Add(-cacheMaxAge)
	for _, e := range entries {
		if fi, err := e.Info(); err == nil && fi.Mode().IsRegular() && fi.ModTime().Before(cutoff) {
			os.Remove(filepath.Join(c.cacheDir, e.Name()))
		}
	}
}

func (e *cacheEntry) response(h http.Header) *Response {
	h = h.Clone()
	if e.ContentType != "" {
		h.Set("Content-Type", e.ContentType)
	}
	if e.Link != "" {
		h.Set("Link", e.Link)
	}
	return &Response{Status: http.StatusOK, Header: h, Body: e.Body}
}
