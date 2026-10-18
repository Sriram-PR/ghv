package write

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
)

// Fate is what one sent request did.
type Fate int

const (
	// Done: GitHub made the change.
	Done Fate = iota + 1
	// Refused: GitHub said no to this draft, and nothing changed.
	Refused
	// NotSent: nothing changed, for a reason that is not the draft's: the
	// token, a rate limit, or a redirect.
	NotSent
	// Unknown: the change may have happened.
	Unknown
)

// Result is what GitHub answered. Message says why the fate is not Done.
type Result struct {
	Fate     Fate
	Status   int
	URL      string // html_url of what GitHub made or changed
	FullName string // a fork's name
	Author   string // user.login, or a fork's owner.login
	HeadSHA  string // a pull request's head.sha
	Message  string
}

// Send makes one request with the write token. It never retries and never
// follows a redirect: c must be harden.Client.
func Send(ctx context.Context, c *http.Client, tok []byte, r Request) Result {
	if err := CheckToken(tok); err != nil {
		return Result{Fate: NotSent, Message: err.Error()}
	}
	req, err := http.NewRequestWithContext(ctx, r.Method, apiBase+r.Path, bytes.NewReader(r.Body))
	if err != nil {
		return Result{Fate: NotSent, Message: err.Error()}
	}
	setHeaders(req, tok)
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.Do(req)
	if err != nil {
		return Result{Fate: Unknown, Message: err.Error()}
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	s := resp.StatusCode
	res := Result{Status: s}
	switch {
	case s >= 200 && s < 300:
		var made struct {
			HTMLURL  string `json:"html_url"`
			FullName string `json:"full_name"`
			User     struct{ Login string }
			Owner    struct{ Login string }
			Head     struct{ SHA string }
		}
		if json.Unmarshal(body, &made) != nil || made.HTMLURL == "" {
			res.Fate, res.Message = Unknown, fmt.Sprintf("GitHub answered %d without the URL of what it made", s)
			return res
		}
		res.Fate, res.URL, res.FullName, res.HeadSHA = Done, made.HTMLURL, made.FullName, made.Head.SHA
		res.Author = made.User.Login
		if res.Author == "" {
			res.Author = made.Owner.Login
		}
		return res
	case s == http.StatusUnauthorized, s >= 300 && s < 400:
		res.Fate, res.Message = NotSent, statusError(resp, body).Error()
	case s == http.StatusTooManyRequests,
		s == http.StatusForbidden && (resp.Header.Get("X-Ratelimit-Remaining") == "0" || resp.Header.Get("Retry-After") != ""):
		res.Fate, res.Message = NotSent, describe(s, body)
	case s >= 400 && s < 500:
		res.Fate, res.Message = Refused, describe(s, body)
	default:
		res.Fate, res.Message = Unknown, describe(s, body)
	}
	return res
}

// describe is GitHub's message with the details of a 422's errors.
func describe(status int, body []byte) string {
	var g struct {
		Message string `json:"message"`
		Errors  []struct {
			Message string `json:"message"`
			Code    string `json:"code"`
			Field   string `json:"field"`
		} `json:"errors"`
	}
	if json.Unmarshal(body, &g) != nil || g.Message == "" {
		return fmt.Sprintf("GitHub answered %d", status)
	}
	var details []string
	for _, e := range g.Errors {
		if e.Message != "" {
			details = append(details, e.Message)
		} else {
			details = append(details, strings.TrimSpace(e.Code+" "+e.Field))
		}
	}
	if len(details) == 0 {
		return fmt.Sprintf("GitHub answered %d: %s", status, g.Message)
	}
	return fmt.Sprintf("GitHub answered %d: %s (%s)", status, g.Message, strings.Join(details, "; "))
}

// Reader sends GET requests through rt with the write token, for review
// context once GitHub's anonymous rate limit is used up. Any other method
// is refused before it leaves.
func Reader(rt http.RoundTripper, tok []byte) http.RoundTripper { return getOnly{rt, tok} }

type getOnly struct {
	rt  http.RoundTripper
	tok []byte
}

func (g getOnly) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.Method != http.MethodGet {
		if r.Body != nil {
			r.Body.Close()
		}
		return nil, fmt.Errorf("ghv-apply reads with the write token only by GET, not %s", r.Method)
	}
	if err := CheckToken(g.tok); err != nil {
		return nil, err
	}
	r = r.Clone(r.Context())
	r.Header.Set("Authorization", "Bearer "+string(g.tok))
	return g.rt.RoundTrip(r)
}
