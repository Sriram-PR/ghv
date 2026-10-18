package harden

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

type recorder struct {
	reqs []*http.Request
	resp func(*http.Request) *http.Response
}

func (r *recorder) RoundTrip(req *http.Request) (*http.Response, error) {
	r.reqs = append(r.reqs, req)
	return r.resp(req), nil
}

func reply(status int, h http.Header) func(*http.Request) *http.Response {
	return func(req *http.Request) *http.Response {
		if h == nil {
			h = http.Header{}
		}
		return &http.Response{StatusCode: status, Header: h, Body: io.NopCloser(strings.NewReader("{}")), Request: req}
	}
}

func TestTransportIgnoresProxyVariables(t *testing.T) {
	t.Setenv("HTTPS_PROXY", "http://127.0.0.1:1")
	t.Setenv("HTTP_PROXY", "http://127.0.0.1:1")
	t.Setenv("ALL_PROXY", "http://127.0.0.1:1")
	if Transport().Proxy != nil {
		t.Fatal("Transport().Proxy is set")
	}
}

func TestClientTalksToAPIGitHubOnly(t *testing.T) {
	for _, u := range []string{
		"http://api.github.com/user",
		"https://github.com/user",
		"https://api.github.com.evil.example/user",
		"https://api.github.com:8443/user",
		"https://uploads.github.com/user",
		"https://x:y@api.github.com/user",
	} {
		rec := &recorder{resp: reply(200, nil)}
		resp, err := Client(rec).Get(u)
		if err == nil {
			resp.Body.Close()
		}
		if err == nil || !strings.Contains(err.Error(), "https://api.github.com only") || len(rec.reqs) != 0 {
			t.Errorf("%s: err %v, %d requests sent", u, err, len(rec.reqs))
		}
	}
	rec := &recorder{resp: reply(200, nil)}
	resp, err := Client(rec).Get("https://api.github.com/user")
	if err != nil || resp.StatusCode != 200 || len(rec.reqs) != 1 {
		t.Fatalf("api.github.com: %v, %d requests", err, len(rec.reqs))
	}
	resp.Body.Close()
}

func TestClientDoesNotFollowRedirects(t *testing.T) {
	rec := &recorder{resp: reply(http.StatusTemporaryRedirect, http.Header{"Location": {"https://api.github.com/elsewhere"}})}
	resp, err := Client(rec).Get("https://api.github.com/user")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusTemporaryRedirect || len(rec.reqs) != 1 {
		t.Fatalf("status %d after %d requests; want the 307 itself", resp.StatusCode, len(rec.reqs))
	}
}
