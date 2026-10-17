package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/Sriram-PR/ghv/internal/read"
	"github.com/Sriram-PR/ghv/internal/sanitize"
)

func init() {
	register(&command{path: "rate", help: "Show the rate limits left for this token", run: runRate})
	register(&command{path: "token", help: "Show the read token's type, scopes, expiry, and whether it is verifiably read-only", run: runToken})
}

type rateResource struct {
	Name      string    `json:"name"`
	Limit     int       `json:"limit"`
	Used      int       `json:"used"`
	Remaining int       `json:"remaining"`
	Reset     time.Time `json:"reset"`
}

type rateView struct {
	Authenticated bool           `json:"authenticated"`
	Resources     []rateResource `json:"resources"`
}

func runRate(ctx context.Context, a *app) error {
	c, err := a.read()
	if err != nil {
		return err
	}
	resp, err := c.Get(ctx, "rate_limit", "")
	if err != nil {
		return err
	}
	var raw struct {
		Resources map[string]struct {
			Limit, Used, Remaining int
			Reset                  int64
		} `json:"resources"`
	}
	if err := json.Unmarshal(resp.Body, &raw); err != nil {
		return fmt.Errorf("GitHub's rate_limit response: %v", err)
	}
	v := rateView{Authenticated: c.Authenticated(), Resources: []rateResource{}}
	for name, r := range raw.Resources {
		v.Resources = append(v.Resources, rateResource{name, r.Limit, r.Used, r.Remaining, time.Unix(r.Reset, 0).UTC()})
	}
	slices.SortFunc(v.Resources, func(x, y rateResource) int { return strings.Compare(x.Name, y.Name) })
	if a.json() {
		return writeJSON(a.stdout, v)
	}
	width := len("RESOURCE")
	for _, r := range v.Resources {
		width = max(width, len(sanitize.Line(r.Name)))
	}
	fmt.Fprintf(a.stdout, "%-*s %5s %7s %10s  %s\n", width, "RESOURCE", "USED", "LIMIT", "REMAINING", "RESETS")
	for _, r := range v.Resources {
		fmt.Fprintf(a.stdout, "%-*s %5d %7d %10d  %s (%s)\n", width, sanitize.Line(r.Name), r.Used, r.Limit, r.Remaining,
			until(a.now(), r.Reset), r.Reset.Format(time.RFC3339))
	}
	return nil
}

func until(now, t time.Time) string {
	d := t.Sub(now).Round(time.Second)
	switch {
	case d <= 0:
		return "now"
	case d < time.Minute:
		return fmt.Sprintf("in %ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("in %dm", int(d.Minutes()))
	}
	return fmt.Sprintf("in %dh%dm", int(d.Hours()), int(d.Minutes())%60)
}

type tokenView struct {
	Path string `json:"path"`
	read.TokenInfo
}

var verdictText = map[read.Verdict]string{
	read.VerdictAnonymous: "anonymous: no read token, so reads are anonymous (60 requests an hour, no code search)",
	read.VerdictReadOnly:  "read-only: a classic token with no scopes can only read public data",
	read.VerdictRefused:   "refused: the token has scopes, so it may be able to write; replace it with a classic token with no scopes",
}

// runToken reports a refused token in full and still exits 3. /rate_limit
// does not count against the rate limit.
func runToken(ctx context.Context, a *app) error {
	c, err := a.read()
	if err != nil {
		return err
	}
	info := read.TokenInfo{Kind: c.TokenKind(), Verdict: read.VerdictAnonymous}
	var refused error
	if c.Authenticated() {
		resp, err := c.Get(ctx, "rate_limit", "")
		var te *read.TokenError
		switch {
		case errors.As(err, &te):
			info, refused = te.Info, err
		case err != nil:
			return err
		default:
			info = read.Inspect(c.TokenKind(), resp.Header)
		}
	}
	v := tokenView{Path: a.tokenPath(), TokenInfo: info}
	if a.json() {
		if err := writeJSON(a.stdout, v); err != nil {
			return err
		}
		return refused
	}
	w := func(k, val string) { fmt.Fprintf(a.stdout, "%-8s %s\n", k, val) }
	w("token", sanitize.Line(v.Path))
	w("type", string(v.Kind))
	switch {
	case !c.Authenticated():
	case v.Kind == read.TokenFineGrained && v.Scopes == nil:
		w("scopes", "not shown for fine-grained tokens")
	case v.Scopes == nil:
		w("scopes", "not reported")
	case len(v.Scopes) == 0:
		w("scopes", "none")
	default:
		w("scopes", sanitize.Line(strings.Join(v.Scopes, ", ")))
	}
	if c.Authenticated() {
		if v.Expires == nil {
			w("expires", "never")
		} else {
			days := int(v.Expires.Sub(a.now()).Hours() / 24)
			w("expires", fmt.Sprintf("%s (in %d days)", v.Expires.Format(time.RFC3339), days))
		}
	}
	text, ok := verdictText[v.Verdict]
	if !ok {
		text = "unverified: GitHub does not show a fine-grained token's permissions; check in GitHub's token settings that it is read-only and limited to public repositories"
		if v.Kind == read.TokenClassic {
			text = "unverified: GitHub sent no X-OAuth-Scopes header for this classic token"
		}
	}
	w("verdict", text)
	return refused
}
