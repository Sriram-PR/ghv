package read

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	APIVersion     = "2022-11-28"
	defaultAccept  = "application/vnd.github+json"
	requestTimeout = 30 * time.Second
	maxResponse    = 64 << 20
)

var ErrTooLarge = errors.New("GitHub's response is too large")

// TokenError means GitHub reported scopes on the read token, so it may be
// able to write.
type TokenError struct {
	Info TokenInfo
}

func (e *TokenError) Error() string {
	return fmt.Sprintf("the read token has scopes (%s), so it may be able to write; ghv only reads with a classic token with no scopes or a read-only fine-grained token",
		strings.Join(e.Info.Scopes, ", "))
}

type Options struct {
	Token string
	// Transport sends requests after the guard and the token are applied;
	// nil means a clone of http.DefaultTransport. Tests use readtest.Transport.
	Transport http.RoundTripper
	UserAgent string
	// CacheDir holds ETag-validated responses for authenticated reads;
	// "" turns the cache off.
	CacheDir string
}

type Client struct {
	kind    TokenKind
	token   string
	hc      *http.Client
	ua      string
	maxBody int64
	sleep   func(context.Context, time.Duration) error
	now     func() time.Time

	cacheDir      string
	maxCacheEntry int64
}

type Response struct {
	Status int
	Header http.Header
	Body   []byte
}

func NewClient(o Options) *Client {
	base := o.Transport
	if base == nil {
		base = http.DefaultTransport.(*http.Transport).Clone()
	}
	ua := o.UserAgent
	if ua == "" {
		ua = "ghv"
	}
	return &Client{
		kind:    KindOf(o.Token),
		token:   o.Token,
		ua:      ua,
		maxBody: maxResponse,
		sleep:   sleepCtx,
		now:     time.Now,

		cacheDir:      o.CacheDir,
		maxCacheEntry: maxCacheEntry,
		hc: &http.Client{
			Transport: Guard{Next: authTransport{token: o.Token, next: base}},
			Timeout:   requestTimeout,
		},
	}
}

func (c *Client) TokenKind() TokenKind { return c.kind }

func (c *Client) Authenticated() bool { return c.kind != TokenNone }

// Get sends GET https://api.github.com/<path>; path may carry a query.
// An empty accept means GitHub's JSON media type. Authenticated reads are
// conditional when the cache holds an ETag: a 304 costs no rate limit and
// is answered from the cache. Anonymous 304s save nothing, so anonymous
// reads skip the cache.
func (c *Client) Get(ctx context.Context, path, accept string) (*Response, error) {
	u, err := APIURL(path)
	if err != nil {
		return nil, err
	}
	var key string
	var cached *cacheEntry
	if c.cacheDir != "" && c.Authenticated() {
		key = cacheKey(c.token, accept, u.String())
		cached = c.cacheLoad(key)
	}
	resp, err := c.do(ctx, func() (*http.Request, error) {
		req, err := c.newRequest(ctx, u.String(), accept)
		if err == nil && cached != nil {
			req.Header.Set("If-None-Match", cached.ETag)
		}
		return req, err
	})
	if err != nil {
		return nil, err
	}
	switch {
	case resp.Status == http.StatusNotModified && cached != nil:
		c.cacheTouch(key)
		return cached.response(resp.Header), nil
	case resp.Status == http.StatusOK && key != "":
		c.cacheStore(key, resp)
	}
	return resp, nil
}

func (c *Client) newRequest(ctx context.Context, u, accept string) (*http.Request, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, err
	}
	c.setHeaders(req, accept)
	return req, nil
}

func (c *Client) setHeaders(req *http.Request, accept string) {
	if accept == "" {
		accept = defaultAccept
	}
	req.Header.Set("Accept", accept)
	req.Header.Set("X-GitHub-Api-Version", APIVersion)
	req.Header.Set("User-Agent", c.ua)
}

func (c *Client) send(req *http.Request) (*Response, error) {
	resp, err := c.hc.Do(req)
	if err != nil {
		return nil, unwrapURLError(err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, c.maxBody+1))
	if err != nil {
		return nil, err
	}
	if int64(len(body)) > c.maxBody {
		return nil, fmt.Errorf("%w (over %d bytes)", ErrTooLarge, c.maxBody)
	}
	if err := errorFor(resp.StatusCode, resp.Header, body, c.Authenticated()); err != nil {
		return nil, err
	}
	return &Response{Status: resp.StatusCode, Header: resp.Header, Body: body}, nil
}

// unwrapURLError drops net/http's `Get "URL": ` prefix from ghv's own
// refusals, so the message starts with the reason.
func unwrapURLError(err error) error {
	var ue *url.Error
	if !errors.As(err, &ue) {
		return err
	}
	var te *TokenError
	if errors.As(ue.Err, &te) {
		return te
	}
	if errors.Is(ue.Err, ErrReadOnly) {
		return ue.Err
	}
	return err
}

// authTransport adds the read token to api.github.com requests only, and
// refuses any response that shows the token has scopes.
type authTransport struct {
	token string
	next  http.RoundTripper
}

func (a authTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if a.token == "" || req.URL.Host != apiHost {
		return a.next.RoundTrip(req)
	}
	r := req.Clone(req.Context())
	r.Header.Set("Authorization", "Bearer "+a.token)
	resp, err := a.next.RoundTrip(r)
	if err != nil {
		return nil, err
	}
	if info := Inspect(KindOf(a.token), resp.Header); info.Verdict == VerdictRefused {
		resp.Body.Close()
		return nil, &TokenError{Info: info}
	}
	return resp, nil
}
