package read

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestErrorMapping(t *testing.T) {
	tests := []struct {
		name    string
		token   string
		status  int
		header  http.Header
		body    string
		code    ErrorCode
		message string
	}{
		{"bad token", "ghp_x", 401, nil, `{"message":"Bad credentials"}`,
			CodeTokenInvalid, "the read token is invalid or expired (GitHub: Bad credentials)"},
		{"anonymous needs a token", "", 401, nil, `{"message":"Requires authentication"}`,
			CodeTokenRequired, "this needs a read token (GitHub: Requires authentication)"},
		{"primary limit", "ghp_x", 403, http.Header{"X-Ratelimit-Remaining": {"0"}, "X-Ratelimit-Reset": {"1792246117"}}, `{"message":"API rate limit exceeded"}`,
			CodeRateLimited, "rate limit reached; it resets at 2026-10-17T14:08:37Z"},
		{"primary limit 429", "", 429, http.Header{"X-Ratelimit-Remaining": {"0"}, "X-Ratelimit-Reset": {"1792246117"}}, `{}`,
			CodeRateLimited, "rate limit reached; it resets at 2026-10-17T14:08:37Z"},
		{"primary limit without reset", "", 403, http.Header{"X-Ratelimit-Remaining": {"0"}}, `{}`,
			CodeRateLimited, "rate limit reached"},
		{"secondary with Retry-After", "ghp_x", 403, http.Header{"Retry-After": {"30"}}, `{"message":"You have exceeded a secondary rate limit."}`,
			CodeSecondaryLimit, "secondary rate limit reached; retry after 30s (GitHub: You have exceeded a secondary rate limit.)"},
		{"secondary without Retry-After", "ghp_x", 429, nil, `{"message":"You have exceeded a secondary rate limit"}`,
			CodeSecondaryLimit, "secondary rate limit reached; retry after 1m0s (GitHub: You have exceeded a secondary rate limit)"},
		{"classic token blocked", "ghp_x", 403, nil, `{"message":"` + "`acme` forbids access via a personal access token (classic). Please use a GitHub App" + `"}`,
			CodeClassicBlocked, "this organisation may block classic tokens (GitHub: `acme` forbids access via a personal access token (classic). Please use a GitHub App)"},
		{"plain 403", "", 403, nil, `{"message":"Must have admin rights to Repository."}`,
			CodeForbidden, "forbidden (GitHub: Must have admin rights to Repository.)"},
		{"not found", "ghp_x", 404, nil, `{"message":"Not Found"}`,
			CodeNotFound, "not found, or the token cannot see it"},
		{"422 as given", "ghp_x", 422, nil, `{"message":"Validation Failed","errors":[{"resource":"Search","field":"q","code":"invalid"},{"message":"Query must include 'is:issue' or 'is:pull-request'"},"plain string"]}`,
			CodeValidation, "Validation Failed: Search.q invalid; Query must include 'is:issue' or 'is:pull-request'; plain string"},
		{"server error", "", 502, nil, `<html>bad gateway</html>`,
			CodeServer, "GitHub server error (HTTP 502)"},
		{"other status", "", 409, nil, `{"message":"Git Repository is empty."}`,
			CodeHTTP, "HTTP 409 (GitHub: Git Repository is empty.)"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := fakeClient(tt.token, func(w http.ResponseWriter, r *http.Request) {
				for k, v := range tt.header {
					w.Header()[k] = v
				}
				if tt.token != "" {
					w.Header().Set("X-Oauth-Scopes", "")
				}
				w.WriteHeader(tt.status)
				io.WriteString(w, tt.body)
			})
			_, err := c.Get(context.Background(), "repos/o/r", "")
			var ae *APIError
			if !errors.As(err, &ae) {
				t.Fatalf("err = %v, want *APIError", err)
			}
			if ae.Code != tt.code || ae.Status != tt.status || ae.Error() != tt.message {
				t.Fatalf("got %s %d %q\nwant %s %d %q", ae.Code, ae.Status, ae.Error(), tt.code, tt.status, tt.message)
			}
		})
	}
}

func TestErrorRateLimitFields(t *testing.T) {
	c := fakeClient("", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "45")
		w.WriteHeader(403)
		io.WriteString(w, `{"message":"secondary rate limit"}`)
	})
	_, err := c.Get(context.Background(), "repos/o/r", "")
	var ae *APIError
	if !errors.As(err, &ae) || ae.RetryAfter != 45*time.Second {
		t.Fatalf("err = %v", err)
	}
	c = fakeClient("", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Ratelimit-Remaining", "0")
		w.Header().Set("X-Ratelimit-Reset", "1792246117")
		w.WriteHeader(403)
	})
	_, err = c.Get(context.Background(), "repos/o/r", "")
	if !errors.As(err, &ae) || !ae.Reset.Equal(time.Unix(1792246117, 0)) {
		t.Fatalf("err = %v", err)
	}
}

func TestSuccessIsNotAnError(t *testing.T) {
	for _, status := range []int{200, 201, 204} {
		c := fakeClient("", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(status) })
		if _, err := c.Get(context.Background(), "repos/o/r", ""); err != nil {
			t.Errorf("%d: %v", status, err)
		}
	}
}

func TestGraphQLError(t *testing.T) {
	tests := []struct {
		body, code, message string
	}{
		{`{"data":{"viewer":{"login":"x"}}}`, "", ""},
		{`{"data":null,"errors":[{"type":"NOT_FOUND","message":"Could not resolve to a Repository with the name 'o/r'."},{"message":"second"}]}`,
			"graphql_error", "GraphQL: Could not resolve to a Repository with the name 'o/r'.; second"},
		{`{"errors":[{"type":"RATE_LIMITED","message":"API rate limit exceeded"}]}`,
			"rate_limited", "GraphQL: API rate limit exceeded"},
		{`not json`, "", ""},
	}
	for _, tt := range tests {
		err := graphQLError([]byte(tt.body), http.Header{})
		if tt.code == "" {
			if err != nil {
				t.Errorf("%s: err = %v", tt.body, err)
			}
			continue
		}
		var ae *APIError
		if !errors.As(err, &ae) || string(ae.Code) != tt.code || ae.Error() != tt.message {
			t.Errorf("%s: err = %v", tt.body, err)
		}
	}
}

func TestErrorMessageKeepsGitHubTextRaw(t *testing.T) {
	c := fakeClient("", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(403)
		io.WriteString(w, `{"message":"\u001b]52;c;aGk=\u0007"}`)
	})
	_, err := c.Get(context.Background(), "repos/o/r", "")
	if err == nil || !strings.Contains(err.Error(), "\x1b]52") {
		t.Fatalf("err = %q; the caller sanitises, so the raw text must survive", err)
	}
}
