package fetch

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Sriram-PR/ghv/internal/harden"
)

type reply struct {
	status int
	header http.Header
	body   string
}

// fakeAPI answers by request URI ("/repos/o/r?x=1") and records every request.
type fakeAPI struct {
	routes map[string]reply
	reqs   []*http.Request
}

func (f *fakeAPI) RoundTrip(r *http.Request) (*http.Response, error) {
	f.reqs = append(f.reqs, r)
	rp, ok := f.routes[r.URL.RequestURI()]
	if !ok {
		rp = reply{status: 404, body: `{"message":"Not Found"}`}
	}
	h := rp.header
	if h == nil {
		h = http.Header{}
	}
	return &http.Response{StatusCode: rp.status, Header: h, Body: io.NopCloser(strings.NewReader(rp.body)), Request: r}, nil
}

func newFake(routes map[string]reply) (*fakeAPI, *Client) {
	f := &fakeAPI{routes: routes}
	return f, New(harden.Client(f))
}

// The agent can write ~/.cache/ghv and ~/.config/ghv/read-token. fetch
// takes neither a path nor a token, so planting both changes nothing.
func TestGetIgnoresTheAgentsCacheAndToken(t *testing.T) {
	home := t.TempDir()
	for name, data := range map[string]string{
		".config/ghv/read-token":   "ghp_plantedplantedplantedplanted0000",
		".cache/ghv/0123456789abc": `{"full_name":"evil/poisoned"}`,
	} {
		p := filepath.Join(home, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(home, ".cache"))
	t.Setenv("GITHUB_TOKEN", "ghp_fromtheenvironmentfromtheenvironment")

	f, c := newFake(map[string]reply{"/repos/o/r": {status: 200, body: `{"full_name":"o/r"}`}})
	body, err := c.Get(context.Background(), "/repos/o/r", "application/vnd.github+json")
	if err != nil || string(body) != `{"full_name":"o/r"}` {
		t.Fatalf("%q, %v", body, err)
	}
	r := f.reqs[0]
	if r.Method != "GET" || r.URL.String() != "https://api.github.com/repos/o/r" || r.Body != nil {
		t.Fatalf("request %s %s", r.Method, r.URL)
	}
	for _, h := range []string{"Authorization", "If-None-Match", "If-Modified-Since", "Cookie"} {
		if v := r.Header.Get(h); v != "" {
			t.Errorf("%s: %q", h, v)
		}
	}
	if r.Header.Get("Accept") != "application/vnd.github+json" || r.Header.Get("X-GitHub-Api-Version") != "2022-11-28" {
		t.Fatalf("headers %v", r.Header)
	}
}

func TestGetFollowsRedirectsOnTheAPIHost(t *testing.T) {
	f, c := newFake(map[string]reply{
		"/repos/docker/docker":    {status: 301, header: http.Header{"Location": {"https://api.github.com/repositories/7691631"}}},
		"/repositories/7691631":   {status: 200, body: `{"full_name":"moby/moby"}`},
		"/repos/a/loop":           {status: 302, header: http.Header{"Location": {"/repos/a/loop"}}},
		"/repos/a/elsewhere":      {status: 307, header: http.Header{"Location": {"https://evil.example/x"}}},
		"/repos/a/plain-http":     {status: 308, header: http.Header{"Location": {"http://api.github.com/repos/a/b"}}},
		"/repos/a/no-location":    {status: 301},
		"/repos/a/not-a-redirect": {status: 304},
	})
	body, err := c.Get(context.Background(), "/repos/docker/docker", "application/vnd.github+json")
	if err != nil || string(body) != `{"full_name":"moby/moby"}` || len(f.reqs) != 2 {
		t.Fatalf("%q, %v, %d requests", body, err, len(f.reqs))
	}
	for path, want := range map[string]string{
		"/repos/a/loop":           "more than 3 redirects",
		"/repos/a/elsewhere":      `redirected to "https://evil.example/x"`,
		"/repos/a/plain-http":     `redirected to "http://api.github.com/repos/a/b"`,
		"/repos/a/no-location":    "GitHub answered 301",
		"/repos/a/not-a-redirect": "GitHub answered 304",
	} {
		if _, err := c.Get(context.Background(), path, "application/vnd.github+json"); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: %v, want %q", path, err, want)
		}
	}
}

func TestGetErrors(t *testing.T) {
	limited := http.Header{"X-Ratelimit-Remaining": {"0"}, "X-Ratelimit-Reset": {"1792411200"}}
	_, c := newFake(map[string]reply{
		"/limited":     {status: 403, header: limited, body: `{"message":"API rate limit exceeded"}`},
		"/limited-429": {status: 429, header: http.Header{"X-Ratelimit-Remaining": {"0"}}},
		"/forbidden":   {status: 403, body: `{"message":"Repository access blocked"}`},
		"/server":      {status: 502, body: "<html>"},
		"/huge":        {status: 200, body: strings.Repeat("x", MaxBody+1)},
	})
	ctx := context.Background()
	_, err := c.Get(ctx, "/nope", "application/vnd.github+json")
	if !errors.Is(err, ErrNotFound) || err.Error() != "GET /nope: not found" {
		t.Fatalf("404: %v", err)
	}
	_, err = c.Get(ctx, "/limited", "application/vnd.github+json")
	var rl *RateLimitError
	if !errors.As(err, &rl) || !rl.Reset.Equal(time.Unix(1792411200, 0)) ||
		!strings.Contains(err.Error(), "anonymous rate limit is used up; it resets at 12:00 UTC") {
		t.Fatalf("rate limit: %v", err)
	}
	_, err = c.Get(ctx, "/limited-429", "application/vnd.github+json")
	if !errors.As(err, &rl) || !rl.Reset.IsZero() || strings.Contains(err.Error(), "resets") {
		t.Fatalf("429: %v", err)
	}
	for path, want := range map[string]string{
		"/forbidden": "GET /forbidden: GitHub answered 403: Repository access blocked",
		"/server":    "GET /server: GitHub answered 502",
		"/huge":      "GET /huge: the response is over 16 MiB",
	} {
		if _, err := c.Get(ctx, path, "application/vnd.github+json"); err == nil || err.Error() != want {
			t.Errorf("%s: %v, want %q", path, err, want)
		}
	}
}

func TestGetJSON(t *testing.T) {
	_, c := newFake(map[string]reply{
		"/ok":  {status: 200, body: `{"full_name":"o/r"}`},
		"/bad": {status: 200, body: `<html>`},
	})
	var v struct {
		FullName string `json:"full_name"`
	}
	if err := c.GetJSON(context.Background(), "/ok", &v); err != nil || v.FullName != "o/r" {
		t.Fatalf("%+v, %v", v, err)
	}
	if err := c.GetJSON(context.Background(), "/bad", &v); err == nil || !strings.HasPrefix(err.Error(), "GET /bad: ") {
		t.Fatalf("bad JSON: %v", err)
	}
}
