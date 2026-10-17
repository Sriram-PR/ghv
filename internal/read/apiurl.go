package read

import (
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// APIURL turns the PATH argument of `ghv api` into a URL on api.github.com.
func APIURL(p string) (*url.URL, error) {
	if p == "" {
		return nil, errors.New("ghv api: empty path")
	}
	if strings.Contains(p, "://") || strings.HasPrefix(p, "//") || strings.ContainsAny(p, `@\#`) {
		return nil, fmt.Errorf("ghv api: %q: give a path such as repos/OWNER/REPO, not a URL", p)
	}
	for _, r := range p {
		if r <= ' ' || r >= 0x7f {
			return nil, fmt.Errorf("ghv api: %q: percent-encode spaces, control and non-ASCII characters", p)
		}
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	u, err := url.Parse("https://" + apiHost + p)
	if err != nil {
		return nil, fmt.Errorf("ghv api: %w", err)
	}
	if u.Scheme != "https" || u.Host != apiHost || u.User != nil || u.Opaque != "" || u.Fragment != "" {
		return nil, fmt.Errorf("ghv api: %q does not stay on %s", p, apiHost)
	}
	return u, nil
}
