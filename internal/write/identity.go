// Package write is ghv-apply's write-token client. It is the only code
// that sends the write token, and ghv cannot import it.
package write

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"
)

const (
	apiBase    = "https://api.github.com"
	apiVersion = "2022-11-28"
	maxBody    = 1 << 20
)

var (
	ErrNotClassic = errors.New("the write token must be a classic personal access token (ghp_); fine-grained, OAuth, and app tokens are refused")
	ErrScopes     = errors.New("wrong scopes")
)

var (
	tokenRE = regexp.MustCompile(`^ghp_[A-Za-z0-9]{20,251}$`)
	loginRE = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,37}[A-Za-z0-9])?$`)
)

// Identity is who GitHub says the write token belongs to.
type Identity struct {
	Login   string
	Expires *time.Time // nil: the token never expires
}

func CheckToken(tok []byte) error {
	if !tokenRE.Match(tok) {
		return ErrNotClassic
	}
	return nil
}

// Whoami sends GET /user with tok and accepts only a classic token whose
// X-OAuth-Scopes is exactly public_repo.
func Whoami(ctx context.Context, c *http.Client, tok []byte) (Identity, error) {
	if err := CheckToken(tok); err != nil {
		return Identity{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, apiBase+"/user", nil)
	if err != nil {
		return Identity{}, err
	}
	setHeaders(req, tok)
	resp, err := c.Do(req)
	if err != nil {
		return Identity{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return Identity{}, err
	}
	if err := statusError(resp, body); err != nil {
		return Identity{}, err
	}
	vals, ok := resp.Header["X-Oauth-Scopes"]
	if !ok {
		return Identity{}, errors.New("GitHub sent no X-OAuth-Scopes header, so this is not a classic token")
	}
	if scopes := parseScopes(vals); !slices.Equal(scopes, []string{"public_repo"}) {
		shown := strings.Join(scopes, ", ")
		if shown == "" {
			shown = "none"
		}
		return Identity{}, fmt.Errorf("%w: the write token has scopes %s; ghv-apply needs a classic token with only public_repo", ErrScopes, shown)
	}
	var u struct {
		Login string `json:"login"`
	}
	if err := json.Unmarshal(body, &u); err != nil {
		return Identity{}, fmt.Errorf("GitHub's /user: %v", err)
	}
	if !loginRE.MatchString(u.Login) {
		return Identity{}, fmt.Errorf("GitHub's /user sent login %q", u.Login)
	}
	return Identity{Login: u.Login, Expires: expiry(resp.Header)}, nil
}

func setHeaders(req *http.Request, tok []byte) {
	req.Header.Set("Authorization", "Bearer "+string(tok))
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", apiVersion)
	req.Header.Set("User-Agent", "ghv-apply")
}

func statusError(resp *http.Response, body []byte) error {
	switch s := resp.StatusCode; {
	case s >= 200 && s < 300:
		return nil
	case s == http.StatusUnauthorized:
		return errors.New("GitHub refused the write token (401): it is wrong, expired, or revoked")
	case s >= 300 && s < 400:
		return fmt.Errorf("GitHub answered %d, a redirect to %q; ghv-apply does not follow redirects", s, resp.Header.Get("Location"))
	default:
		var g struct {
			Message string `json:"message"`
		}
		if json.Unmarshal(body, &g) == nil && g.Message != "" {
			return fmt.Errorf("GitHub answered %d: %s", s, g.Message)
		}
		return fmt.Errorf("GitHub answered %d", s)
	}
}

func parseScopes(vals []string) []string {
	out := []string{}
	for _, v := range vals {
		for _, s := range strings.Split(v, ",") {
			if s = strings.TrimSpace(s); s != "" {
				out = append(out, s)
			}
		}
	}
	return out
}

func expiry(h http.Header) *time.Time {
	exp := h.Get("Github-Authentication-Token-Expiration")
	for _, layout := range []string{"2006-01-02 15:04:05 MST", "2006-01-02 15:04:05 -0700"} {
		if t, err := time.Parse(layout, exp); err == nil {
			t = t.UTC()
			return &t
		}
	}
	return nil
}
