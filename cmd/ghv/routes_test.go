package main

import (
	"io"
	"net/http"
	"sync"
	"testing"
)

// routes is a fake GitHub that answers each request URI ("/repos/o/r?x=1")
// with a fixed JSON body, and 404 otherwise. It records every URI asked.
type routes struct {
	t      *testing.T
	mu     sync.Mutex
	bodies map[string]string
	header map[string]http.Header
	asked  []string
}

func newRoutes(t *testing.T, bodies map[string]string) *routes {
	return &routes{t: t, bodies: bodies, header: map[string]http.Header{}}
}

func (rt *routes) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	rt.mu.Lock()
	rt.asked = append(rt.asked, r.URL.RequestURI())
	body, ok := rt.bodies[r.URL.RequestURI()]
	h := rt.header[r.URL.RequestURI()]
	rt.mu.Unlock()
	for k, v := range h {
		w.Header()[k] = v
	}
	if !ok {
		w.WriteHeader(http.StatusNotFound)
		io.WriteString(w, `{"message":"Not Found"}`)
		return
	}
	io.WriteString(w, body)
}

func (rt *routes) uris() []string {
	rt.mu.Lock()
	defer rt.mu.Unlock()
	return append([]string(nil), rt.asked...)
}
