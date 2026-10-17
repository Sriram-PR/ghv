package read

import (
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

type recorder struct{ reqs []*http.Request }

func (r *recorder) RoundTrip(req *http.Request) (*http.Response, error) {
	r.reqs = append(r.reqs, req)
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     http.Header{},
		Body:       io.NopCloser(strings.NewReader("{}")),
		Request:    req,
	}, nil
}

func refused(t *testing.T, req *http.Request) {
	t.Helper()
	rec := &recorder{}
	_, err := Guard{Next: rec}.RoundTrip(req)
	if !errors.Is(err, ErrReadOnly) {
		t.Fatalf("err = %v, want ErrReadOnly", err)
	}
	if len(rec.reqs) != 0 {
		t.Fatalf("sent %d requests, want 0", len(rec.reqs))
	}
}

var guardURLs = []struct {
	url    string
	getsOK bool
}{
	{"https://api.github.com/repos/o/r", true},
	{"https://api.github.com/repos/o/r/pulls?state=open", true},
	{"https://github.com/o/r/releases/download/v1/a.tgz", true},
	{"https://objects.githubusercontent.com/github-production-release-asset/1", true},
	{"https://release-assets.githubusercontent.com/github-production-release-asset/1", true},
	{"https://results-receiver.actions.githubusercontent.com/rest/runs/1/logs", true},
	{"https://evil.release-assets.githubusercontent.com/x", false},
	{"https://actions.githubusercontent.com/x", false},
	{"https://productionresultssa0.blob.core.windows.net/actions-results/x", true},
	{"https://api.github.com/graphql", false},
	{"https://api.github.com/graphql/", false},
	{"https://api.github.com/GraphQL", false},
	{"https://api.github.com//graphql", false},
	{"https://api.github.com/x/../graphql", false},
	{"https://api.github.com/%67raphql", false},
	{"http://api.github.com/repos/o/r", false},
	{"https://api.github.com:443/repos/o/r", false},
	{"https://user@api.github.com/repos/o/r", false},
	{"https://api.github.com.evil.example/", false},
	{"https://evilgithub.com/", false},
	{"https://uploads.github.com/repos/o/r/releases/1/assets", false},
	{"https://gist.github.com/", false},
	{"https://API.GITHUB.COM/repos/o/r", false},
	{"https://a.b.blob.core.windows.net/x", false},
	{"https://blob.core.windows.net/x", false},
	{"https://-x.blob.core.windows.net/x", false},
	{"https://127.0.0.1/", false},
}

var allMethods = []string{"GET", "HEAD", "POST", "PUT", "PATCH", "DELETE", "OPTIONS", "CONNECT", "TRACE", "get", "PROPFIND"}

func TestGuardMethodsAndHosts(t *testing.T) {
	for _, m := range allMethods {
		for _, tc := range guardURLs {
			t.Run(m+" "+tc.url, func(t *testing.T) {
				req, err := http.NewRequest(m, tc.url, nil)
				if err != nil {
					t.Fatal(err)
				}
				if !(tc.getsOK && m == "GET") {
					refused(t, req)
					return
				}
				rec := &recorder{}
				if _, err := (Guard{Next: rec}).RoundTrip(req); err != nil || len(rec.reqs) != 1 {
					t.Fatalf("err = %v, sent %d; want admitted", err, len(rec.reqs))
				}
			})
		}
	}
}

func TestGuardRefusalMessage(t *testing.T) {
	req, _ := http.NewRequest("DELETE", "https://api.github.com/repos/o/r", nil)
	_, err := Guard{Next: &recorder{}}.RoundTrip(req)
	if err == nil || !strings.HasPrefix(err.Error(), "ghv is read-only; draft this with 'ghv <command>'") {
		t.Fatalf("err = %v", err)
	}
}

func TestGuardRefusesGETWithBody(t *testing.T) {
	req, _ := http.NewRequest("GET", "https://api.github.com/repos/o/r", strings.NewReader("x"))
	refused(t, req)
}

func TestGuardRefusesHostOverride(t *testing.T) {
	req, _ := http.NewRequest("GET", "https://api.github.com/repos/o/r", nil)
	req.Host = "evil.example"
	refused(t, req)
}

func TestGuardRefusesHeaders(t *testing.T) {
	for _, h := range []string{"X-HTTP-Method-Override", "x-http-method", "X-Method-Override", "content-encoding"} {
		t.Run(h, func(t *testing.T) {
			req, _ := http.NewRequest("GET", "https://api.github.com/repos/o/r", nil)
			req.Header[h] = []string{"DELETE"}
			refused(t, req)
		})
	}
}

type redirector struct{ calls int }

func (r *redirector) RoundTrip(req *http.Request) (*http.Response, error) {
	r.calls++
	return &http.Response{
		StatusCode: http.StatusFound,
		Header:     http.Header{"Location": {"https://evil.example/x"}},
		Body:       http.NoBody,
		Request:    req,
	}, nil
}

func TestGuardChecksEveryRedirectHop(t *testing.T) {
	rt := &redirector{}
	client := &http.Client{Transport: Guard{Next: rt}}
	_, err := client.Get("https://api.github.com/repos/o/r")
	if !errors.Is(err, ErrReadOnly) {
		t.Fatalf("err = %v, want ErrReadOnly", err)
	}
	if rt.calls != 1 {
		t.Fatalf("transport called %d times, want 1", rt.calls)
	}
}
