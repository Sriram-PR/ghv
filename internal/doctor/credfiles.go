package doctor

import (
	"context"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
)

func init() { register("credential-files", credentialFiles) }

const credLimit = 1 << 20

func credentialFiles(_ context.Context, e *Env) []Finding {
	var out []Finding
	for _, home := range e.homes() {
		for _, name := range []string{".git-credentials", ".config/git/credentials"} {
			p := filepath.Join(home, name)
			b, err := readCapped(p, credLimit)
			if err != nil {
				continue
			}
			if n := gitCredentials(string(b)); n > 0 {
				out = append(out, Finding{Severity: High,
					Detail: fmt.Sprintf("%s holds %s for github.com", p, plural(n, "credential", "credentials")),
					Fix:    fmt.Sprintf("delete the github.com lines from %s and revoke those tokens at https://github.com/settings/tokens", p)})
			}
		}
		p := filepath.Join(home, ".netrc")
		if b, err := readCapped(p, credLimit); err == nil {
			if n := netrcEntries(string(b)); n > 0 {
				out = append(out, Finding{Severity: High,
					Detail: fmt.Sprintf("%s holds %s for github.com", p, plural(n, "entry", "entries")),
					Fix:    fmt.Sprintf("delete the github.com entries from %s and revoke those tokens at https://github.com/settings/tokens", p)})
			}
		}
	}
	return out
}

// gitCredentials counts the lines of git's store helper file that name a
// GitHub host.
func gitCredentials(s string) int {
	n := 0
	for line := range strings.Lines(s) {
		u, err := url.Parse(strings.TrimSpace(line))
		if err == nil && isGitHubHost(u.Hostname()) {
			n++
		}
	}
	return n
}

// netrcEntries counts machine entries for a GitHub host, and a default
// entry, which matches every host.
func netrcEntries(s string) int {
	n := 0
	f := strings.Fields(s)
	for i, w := range f {
		switch {
		case w == "default":
			n++
		case w == "machine" && i+1 < len(f) && isGitHubHost(f[i+1]):
			n++
		}
	}
	return n
}
