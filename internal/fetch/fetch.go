// Package fetch reads GitHub for ghv-apply's review context: anonymous GETs
// to api.github.com, with no cache, no conditional requests, and no token,
// so nothing the agent can write reaches what the human is shown.
package fetch

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/Sriram-PR/ghv/internal/harden"
)

const (
	apiVersion   = "2022-11-28"
	MaxBody      = 16 << 20
	maxRedirects = 3
)

var ErrNotFound = errors.New("not found")

// RateLimitError is GitHub's anonymous limit (60 requests an hour) running
// out. Reset is zero when GitHub did not say when it resets.
type RateLimitError struct{ Reset time.Time }

func (e *RateLimitError) Error() string {
	s := "GitHub's anonymous rate limit is used up"
	if !e.Reset.IsZero() {
		s += "; it resets at " + e.Reset.UTC().Format("15:04") + " UTC"
	}
	return s
}

type StatusError struct {
	Status  int
	Message string
}

func (e *StatusError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("GitHub answered %d: %s", e.Status, e.Message)
	}
	return fmt.Sprintf("GitHub answered %d", e.Status)
}

type Client struct{ hc *http.Client }

// New takes a client built by harden.Client, which reaches api.github.com
// only and hands redirects back instead of following them.
func New(hc *http.Client) *Client { return &Client{hc} }

// Get fetches path (with any query) and returns the body. Redirects are
// followed on api.github.com only: a renamed repository answers 301.
func (c *Client) Get(ctx context.Context, path, accept string) ([]byte, error) {
	body, err := c.get(ctx, "https://"+harden.APIHost+path, accept)
	if err != nil {
		return nil, fmt.Errorf("GET %s: %w", path, err)
	}
	return body, nil
}

func (c *Client) GetJSON(ctx context.Context, path string, v any) error {
	body, err := c.Get(ctx, path, "application/vnd.github+json")
	if err != nil {
		return err
	}
	if err := json.Unmarshal(body, v); err != nil {
		return fmt.Errorf("GET %s: %w", path, err)
	}
	return nil
}

func (c *Client) get(ctx context.Context, u, accept string) ([]byte, error) {
	for range maxRedirects + 1 {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("Accept", accept)
		req.Header.Set("X-GitHub-Api-Version", apiVersion)
		req.Header.Set("User-Agent", "ghv-apply")
		resp, err := c.hc.Do(req)
		if err != nil {
			return nil, err
		}
		body, err := io.ReadAll(io.LimitReader(resp.Body, MaxBody+1))
		resp.Body.Close()
		if err != nil {
			return nil, err
		}
		if len(body) > MaxBody {
			return nil, fmt.Errorf("the response is over %d MiB", MaxBody>>20)
		}
		switch s := resp.StatusCode; {
		case s >= 200 && s < 300:
			return body, nil
		case s == 301 || s == 302 || s == 307 || s == 308:
			next, err := resp.Location()
			if err != nil {
				return nil, &StatusError{Status: s}
			}
			if next.Scheme != "https" || next.Host != harden.APIHost || next.User != nil {
				return nil, fmt.Errorf("GitHub redirected to %q, off %s", next.Redacted(), harden.APIHost)
			}
			u = next.String()
		default:
			return nil, statusError(resp, body)
		}
	}
	return nil, fmt.Errorf("more than %d redirects", maxRedirects)
}

func statusError(resp *http.Response, body []byte) error {
	s := resp.StatusCode
	if (s == http.StatusForbidden || s == http.StatusTooManyRequests) && resp.Header.Get("X-Ratelimit-Remaining") == "0" {
		e := &RateLimitError{}
		if n, err := strconv.ParseInt(resp.Header.Get("X-Ratelimit-Reset"), 10, 64); err == nil {
			e.Reset = time.Unix(n, 0)
		}
		return e
	}
	if s == http.StatusNotFound {
		return ErrNotFound
	}
	var g struct {
		Message string `json:"message"`
	}
	json.Unmarshal(body, &g)
	return &StatusError{Status: s, Message: g.Message}
}
