package write

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Sriram-PR/ghv/internal/harden"
)

const testToken = "ghp_abcdefghijklmnopqrstuvwxyz0123456789"

type fakeGitHub struct {
	reqs   []*http.Request
	status int
	header http.Header
	body   string
}

func (f *fakeGitHub) RoundTrip(r *http.Request) (*http.Response, error) {
	f.reqs = append(f.reqs, r)
	h := f.header
	if h == nil {
		h = http.Header{}
	}
	return &http.Response{StatusCode: f.status, Header: h, Body: io.NopCloser(strings.NewReader(f.body)), Request: r}, nil
}

func okUser(scopes string) *fakeGitHub {
	return &fakeGitHub{
		status: 200,
		header: http.Header{"X-Oauth-Scopes": {scopes}, "Github-Authentication-Token-Expiration": {"2027-01-16 08:00:00 UTC"}},
		body:   `{"login":"Sriram-PR","id":1}`,
	}
}

func TestWhoami(t *testing.T) {
	gh := okUser("public_repo")
	id, err := Whoami(context.Background(), harden.Client(gh), []byte(testToken))
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2027, 1, 16, 8, 0, 0, 0, time.UTC)
	if id.Login != "Sriram-PR" || id.Expires == nil || !id.Expires.Equal(want) {
		t.Fatalf("identity %+v", id)
	}
	if len(gh.reqs) != 1 {
		t.Fatalf("%d requests", len(gh.reqs))
	}
	r := gh.reqs[0]
	if r.Method != "GET" || r.URL.String() != "https://api.github.com/user" || r.Body != nil ||
		r.Header.Get("Authorization") != "Bearer "+testToken ||
		r.Header.Get("Accept") != "application/vnd.github+json" ||
		r.Header.Get("X-GitHub-Api-Version") != "2022-11-28" {
		t.Fatalf("request %s %s, headers %v", r.Method, r.URL, r.Header)
	}
}

func TestWhoamiWithoutExpiry(t *testing.T) {
	gh := okUser("public_repo")
	gh.header.Del("Github-Authentication-Token-Expiration")
	id, err := Whoami(context.Background(), harden.Client(gh), []byte(testToken))
	if err != nil || id.Expires != nil {
		t.Fatalf("%+v, %v", id, err)
	}
}

func TestWhoamiRefusals(t *testing.T) {
	scopes := func(s string) *fakeGitHub { return okUser(s) }
	noScopes := okUser("")
	noScopes.header.Del("X-Oauth-Scopes")
	badLogin := okUser("public_repo")
	badLogin.body = `{"login":"-bad"}`
	for name, tt := range map[string]struct {
		gh   *fakeGitHub
		want string
	}{
		"broader":   {scopes("public_repo, repo"), "has scopes public_repo, repo; ghv-apply needs a classic token with only public_repo"},
		"other":     {scopes("repo"), "has scopes repo;"},
		"none":      {scopes(""), "has scopes none;"},
		"no header": {noScopes, "no X-OAuth-Scopes header"},
		"bad login": {badLogin, `login "-bad"`},
		"401":       {&fakeGitHub{status: 401, body: `{"message":"Bad credentials"}`}, "wrong, expired, or revoked"},
		"403":       {&fakeGitHub{status: 403, body: `{"message":"Resource protected by organization SAML enforcement."}`}, "GitHub answered 403: Resource protected by organization SAML enforcement."},
		"redirect":  {&fakeGitHub{status: 301, header: http.Header{"Location": {"https://api.github.com/user2"}}}, "redirect"},
		"server":    {&fakeGitHub{status: 502, body: "<html>"}, "GitHub answered 502"},
		"not json":  {&fakeGitHub{status: 200, header: http.Header{"X-Oauth-Scopes": {"public_repo"}}, body: "<html>"}, "GitHub's /user"},
	} {
		_, err := Whoami(context.Background(), harden.Client(tt.gh), []byte(testToken))
		if err == nil || !strings.Contains(err.Error(), tt.want) {
			t.Errorf("%s: %v, want %q", name, err, tt.want)
		}
		if len(tt.gh.reqs) != 1 {
			t.Errorf("%s: %d requests", name, len(tt.gh.reqs))
		}
	}
}

func TestCheckToken(t *testing.T) {
	for tok, ok := range map[string]bool{
		testToken:                                    true,
		"github_pat_11ABCDEFG0123456789":             false,
		"gho_abcdefghijklmnopqrstuvwxyz0123456789":   false,
		"0123456789abcdef0123456789abcdef01234567":   false,
		"ghp_short":                                  false,
		testToken + "\n":                             false,
		"ghp_abc def ghijklmnopqrstuvwxyz0123456789": false,
	} {
		if err := CheckToken([]byte(tok)); (err == nil) != ok {
			t.Errorf("%q: %v", tok, err)
		}
	}
	gh := okUser("public_repo")
	if _, err := Whoami(context.Background(), harden.Client(gh), []byte("github_pat_x")); !errors.Is(err, ErrNotClassic) || len(gh.reqs) != 0 {
		t.Fatalf("fine-grained token: %v, %d requests", err, len(gh.reqs))
	}
}
