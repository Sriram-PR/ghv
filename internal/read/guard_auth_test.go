package read

import (
	"io"
	"net/http"
	"strings"
	"testing"
)

var downloadURLs = []string{
	"https://github.com/o/r/releases/download/v1/a.tgz",
	"https://objects.githubusercontent.com/github-production-release-asset/1",
	"https://release-assets.githubusercontent.com/github-production-release-asset/1",
	"https://results-receiver.actions.githubusercontent.com/rest/runs/1/logs",
	"https://productionresultssa12.blob.core.windows.net/actions-results/x/job-logs.txt",
}

func TestGuardDropsAuthorizationOffAPIHost(t *testing.T) {
	for _, u := range downloadURLs {
		t.Run(u, func(t *testing.T) {
			req, _ := http.NewRequest("GET", u, nil)
			req.Header.Set("Authorization", "Bearer ghp_secret")
			rec := &recorder{}
			if _, err := (Guard{Next: rec}).RoundTrip(req); err != nil {
				t.Fatal(err)
			}
			if got := rec.reqs[0].Header.Get("Authorization"); got != "" {
				t.Fatalf("download host got Authorization %q", got)
			}
			if req.Header.Get("Authorization") == "" {
				t.Fatal("Guard modified the caller's request")
			}
		})
	}
}

func TestGuardDropsAuthorizationInAnyCase(t *testing.T) {
	req, _ := http.NewRequest("GET", downloadURLs[2], nil)
	req.Header["authorization"] = []string{"Bearer ghp_secret"}
	req.Header["AUTHORIZATION"] = []string{"Bearer ghp_secret"}
	rec := &recorder{}
	if _, err := (Guard{Next: rec}).RoundTrip(req); err != nil {
		t.Fatal(err)
	}
	if len(rec.reqs[0].Header) != 0 {
		t.Fatalf("headers left: %v", rec.reqs[0].Header)
	}
}

func TestGuardKeepsAuthorizationOnAPIHost(t *testing.T) {
	req, _ := http.NewRequest("GET", "https://api.github.com/repos/o/r", nil)
	req.Header.Set("Authorization", "Bearer ghp_secret")
	rec := &recorder{}
	if _, err := (Guard{Next: rec}).RoundTrip(req); err != nil {
		t.Fatal(err)
	}
	if rec.reqs[0].Header.Get("Authorization") != "Bearer ghp_secret" {
		t.Fatal("api.github.com lost Authorization")
	}
}

// hopper redirects the first request to target and records every hop.
type hopper struct {
	target string
	reqs   []*http.Request
}

func (h *hopper) RoundTrip(req *http.Request) (*http.Response, error) {
	h.reqs = append(h.reqs, req)
	if len(h.reqs) == 1 {
		return &http.Response{
			StatusCode: http.StatusFound,
			Header:     http.Header{"Location": {h.target}},
			Body:       http.NoBody,
			Request:    req,
		}, nil
	}
	return &http.Response{StatusCode: http.StatusOK, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("x")), Request: req}, nil
}

func TestRedirectToDownloadHostCarriesNoAuthorization(t *testing.T) {
	for _, u := range downloadURLs {
		t.Run(u, func(t *testing.T) {
			h := &hopper{target: u}
			client := &http.Client{
				Transport: Guard{Next: h},
				// Copy every header, so the test does not rely on net/http's own stripping.
				CheckRedirect: func(req *http.Request, via []*http.Request) error {
					req.Header = via[0].Header.Clone()
					return nil
				},
			}
			req, _ := http.NewRequest("GET", "https://api.github.com/repos/o/r/releases/assets/1", nil)
			req.Header.Set("Authorization", "Bearer ghp_secret")
			resp, err := client.Do(req)
			if err != nil {
				t.Fatal(err)
			}
			resp.Body.Close()
			if len(h.reqs) != 2 {
				t.Fatalf("%d hops, want 2", len(h.reqs))
			}
			if got := h.reqs[1].Header.Get("Authorization"); got != "" {
				t.Fatalf("redirect hop to %s got Authorization %q", h.reqs[1].URL.Host, got)
			}
		})
	}
}
