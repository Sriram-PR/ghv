package read

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/Sriram-PR/ghv/internal/read/readtest"
)

func fakeClient(token string, h http.HandlerFunc) *Client {
	return NewClient(Options{Token: token, Transport: readtest.Transport{Handler: h}})
}

func TestClientSendsTokenToAPIHostOnly(t *testing.T) {
	for _, target := range downloadURLs {
		t.Run(target, func(t *testing.T) {
			got := map[string]string{}
			c := fakeClient("ghp_x", func(w http.ResponseWriter, r *http.Request) {
				got[r.URL.Host] = r.Header.Get("Authorization")
				if r.URL.Host == "api.github.com" {
					w.Header().Set("X-Oauth-Scopes", "")
					http.Redirect(w, r, target, http.StatusFound)
					return
				}
				io.WriteString(w, "asset")
			})
			resp, err := c.Get(context.Background(), "repos/o/r/releases/assets/1", "application/octet-stream")
			if err != nil {
				t.Fatal(err)
			}
			if string(resp.Body) != "asset" {
				t.Fatalf("body %q", resp.Body)
			}
			if got["api.github.com"] != "Bearer ghp_x" {
				t.Fatalf("api.github.com got Authorization %q", got["api.github.com"])
			}
			for host, auth := range got {
				if host != "api.github.com" && auth != "" {
					t.Fatalf("%s got Authorization %q", host, auth)
				}
			}
			if len(got) != 2 {
				t.Fatalf("hosts seen: %v", got)
			}
		})
	}
}

func TestClientHeaders(t *testing.T) {
	var h http.Header
	c := fakeClient("", func(w http.ResponseWriter, r *http.Request) { h = r.Header.Clone() })
	if _, err := c.Get(context.Background(), "repos/o/r", ""); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"Accept":               "application/vnd.github+json",
		"X-Github-Api-Version": "2022-11-28",
		"User-Agent":           "ghv",
		"Authorization":        "",
	}
	for k, v := range want {
		if h.Get(k) != v {
			t.Errorf("%s = %q, want %q", k, h.Get(k), v)
		}
	}
}

func TestClientRefusesScopedToken(t *testing.T) {
	c := fakeClient("ghp_x", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Oauth-Scopes", "public_repo")
		io.WriteString(w, "{}")
	})
	_, err := c.Get(context.Background(), "repos/o/r", "")
	var te *TokenError
	if !errors.As(err, &te) || te.Info.Verdict != VerdictRefused {
		t.Fatalf("err = %v, want *TokenError", err)
	}
	if !strings.Contains(err.Error(), "public_repo") {
		t.Fatalf("message %q does not name the scope", err)
	}
}

func TestClientReturnsGuardRefusalUnwrapped(t *testing.T) {
	c := fakeClient("", func(w http.ResponseWriter, r *http.Request) { t.Fatal("request reached GitHub") })
	_, err := c.Get(context.Background(), "graphql", "")
	if !errors.Is(err, ErrReadOnly) || !strings.HasPrefix(err.Error(), "ghv is read-only") {
		t.Fatalf("err = %v", err)
	}
}

func TestClientCapsResponseBody(t *testing.T) {
	c := fakeClient("", func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "0123456789x") })
	c.maxBody = 10
	if _, err := c.Get(context.Background(), "repos/o/r", ""); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("err = %v, want ErrTooLarge", err)
	}
}

func TestClientTimeout(t *testing.T) {
	if c := NewClient(Options{}); c.hc.Timeout != requestTimeout || requestTimeout.Seconds() != 30 {
		t.Fatalf("timeout %v", c.hc.Timeout)
	}
}
