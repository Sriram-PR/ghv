package main

import (
	"archive/zip"
	"bytes"
	"io"
	"net/http"
	"sync"
	"testing"
)

// fakeHub is a fake GitHub with download hosts. api maps an api.github.com
// request URI to a JSON body, redirect maps one to a download URL, and
// files maps a download URL without its query to the bytes it serves. A
// download host that receives an Authorization header fails the test.
type fakeHub struct {
	t        *testing.T
	api      map[string]string
	redirect map[string]string
	files    map[string][]byte
	mu       sync.Mutex
	asked    []string
	accept   map[string]string
}

func newHub(t *testing.T) *fakeHub {
	return &fakeHub{t: t, api: map[string]string{}, redirect: map[string]string{}, files: map[string][]byte{}, accept: map[string]string{}}
}

func (h *fakeHub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if r.URL.Host != "api.github.com" {
		if r.Header.Get("Authorization") != "" {
			h.t.Errorf("%s received an Authorization header", r.URL.Host)
		}
		key := "https://" + r.URL.Host + r.URL.Path
		h.asked = append(h.asked, key)
		if b, ok := h.files[key]; ok {
			w.Write(b)
			return
		}
		w.WriteHeader(http.StatusNotFound)
		return
	}
	uri := r.URL.RequestURI()
	h.asked = append(h.asked, uri)
	h.accept[uri] = r.Header.Get("Accept")
	w.Header().Set("X-Oauth-Scopes", "")
	if body, ok := h.api[uri]; ok {
		w.Header().Set("Content-Type", "application/json")
		io.WriteString(w, body)
		return
	}
	if u, ok := h.redirect[uri]; ok {
		http.Redirect(w, r, u, http.StatusFound)
		return
	}
	w.WriteHeader(http.StatusNotFound)
	io.WriteString(w, `{"message":"Not Found"}`)
}

func (h *fakeHub) uris() []string {
	h.mu.Lock()
	defer h.mu.Unlock()
	return append([]string(nil), h.asked...)
}

// zipBytes builds a zip archive of name, content pairs.
func zipBytes(t *testing.T, pairs ...string) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for i := 0; i < len(pairs); i += 2 {
		w, err := zw.Create(pairs[i])
		if err != nil {
			t.Fatal(err)
		}
		io.WriteString(w, pairs[i+1])
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}
