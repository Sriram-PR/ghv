package read

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
)

const maxCacheEntry = 8 << 20

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
