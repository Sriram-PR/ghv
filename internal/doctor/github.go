package doctor

import (
	"context"
	"encoding/base64"
	"errors"
	"regexp"
	"strings"
)

var errNoLogin = errors.New("give --login LOGIN")

var loginRE = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9-]{0,38}$`)

// ValidLogin reports whether s can be a GitHub login: letters, digits,
// and hyphens, up to 39, not starting with a hyphen.
func ValidLogin(s string) bool { return loginRE.MatchString(s) }

// githubKeys are the fingerprints of an account's SSH keys on GitHub, or
// why they could not be read.
type githubKeys struct {
	login string
	fps   map[string]bool
	err   error
}

// gitHub reads the keys once, on first use, so a run without keys sends
// nothing.
func (e *Env) gitHub(ctx context.Context) *githubKeys {
	if e.gh != nil {
		return e.gh
	}
	g := &githubKeys{login: e.Login}
	e.gh = g
	if g.login == "" && e.WhoAmI != nil {
		if g.login, g.err = e.WhoAmI(ctx); g.err != nil {
			return g
		}
	}
	if g.login == "" {
		g.err = errNoLogin
		return g
	}
	lines, err := e.UserKeys(ctx, g.login)
	if err != nil {
		g.err = err
		return g
	}
	g.fps = map[string]bool{}
	for _, l := range lines {
		if f := strings.Fields(l); len(f) >= 2 {
			if blob, err := base64.StdEncoding.DecodeString(f[1]); err == nil {
				g.fps[sshFingerprint(blob)] = true
			}
		}
	}
	return g
}

// onGitHub says whether blob is one of the account's keys, as a clause to
// add to a finding; "" when GitHub reads are not set up here.
func (e *Env) onGitHub(ctx context.Context, blob []byte) string {
	if e.UserKeys == nil || blob == nil {
		return ""
	}
	g := e.gitHub(ctx)
	switch {
	case g.err != nil:
		return "; not compared with GitHub: " + g.err.Error()
	case g.fps[sshFingerprint(blob)]:
		return "; registered on GitHub for " + g.login
	}
	return "; not among " + g.login + "'s keys on GitHub (deploy keys and other accounts' keys cannot be listed)"
}
