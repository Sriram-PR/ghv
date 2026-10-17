// Package readtest is a fake GitHub for tests. Install Transport below
// read.Guard, so the guard still checks the real GitHub URLs.
package readtest

import (
	"net/http"
	"net/http/httptest"
)

// Transport answers every request in process from Handler.
type Transport struct {
	Handler http.Handler
}

func (t Transport) RoundTrip(req *http.Request) (*http.Response, error) {
	rec := httptest.NewRecorder()
	t.Handler.ServeHTTP(rec, req)
	resp := rec.Result()
	resp.Request = req
	return resp, nil
}
