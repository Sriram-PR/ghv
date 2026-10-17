// Package read is ghv's GitHub client. Every request passes Guard.
package read

import (
	"errors"
	"fmt"
	"net/http"
	"path"
	"strings"
)

var ErrReadOnly = errors.New("ghv is read-only; draft this with 'ghv <command>'")

const (
	apiHost    = "api.github.com"
	blobSuffix = ".blob.core.windows.net"
)

var downloadHosts = map[string]bool{
	"github.com":                    true,
	"objects.githubusercontent.com": true,
}

var refusedHeaders = []string{"X-HTTP-Method-Override", "X-HTTP-Method", "X-Method-Override", "Content-Encoding"}

// Guard admits GETs to GitHub's hosts and refuses everything else before
// it reaches Next. http.Client calls it again for every redirect hop.
type Guard struct {
	Next http.RoundTripper
}

func (g Guard) RoundTrip(req *http.Request) (*http.Response, error) {
	out, err := admit(req)
	if err != nil {
		if req.Body != nil {
			req.Body.Close()
		}
		target := "<no URL>"
		if req.URL != nil {
			target = req.URL.Redacted()
		}
		return nil, fmt.Errorf("%w (%s %s: %v)", ErrReadOnly, req.Method, target, err)
	}
	return g.Next.RoundTrip(out)
}

func admit(req *http.Request) (*http.Request, error) {
	u := req.URL
	switch {
	case u == nil:
		return nil, errors.New("no URL")
	case u.Scheme != "https":
		return nil, errors.New("scheme is not https")
	case u.User != nil || u.Opaque != "" || u.Port() != "":
		return nil, errors.New("URL carries userinfo, an opaque part, or a port")
	case req.Host != "" && req.Host != u.Host:
		return nil, errors.New("host header differs from the URL")
	}
	for k := range req.Header {
		for _, h := range refusedHeaders {
			if strings.EqualFold(k, h) {
				return nil, fmt.Errorf("header %s is not allowed", k)
			}
		}
	}
	switch req.Method {
	case "", http.MethodGet:
		return admitGet(req)
	}
	return nil, errors.New("method not allowed")
}

func admitGet(req *http.Request) (*http.Request, error) {
	host := req.URL.Host
	if !allowedGetHost(host) {
		return nil, fmt.Errorf("host %q is not allowed", host)
	}
	if host == apiHost && strings.EqualFold(path.Clean("/"+req.URL.Path), "/graphql") {
		return nil, errors.New("GET /graphql is not allowed")
	}
	if req.Body != nil && req.Body != http.NoBody {
		return nil, errors.New("GET with a body")
	}
	return req, nil
}

func allowedGetHost(host string) bool {
	if host == apiHost || downloadHosts[host] {
		return true
	}
	label, ok := strings.CutSuffix(host, blobSuffix)
	return ok && isDNSLabel(label)
}

func isDNSLabel(s string) bool {
	if s == "" || len(s) > 63 || s[0] == '-' || s[len(s)-1] == '-' {
		return false
	}
	for i := 0; i < len(s); i++ {
		c := s[i]
		if !(c >= 'a' && c <= 'z' || c >= '0' && c <= '9' || c == '-') {
			return false
		}
	}
	return true
}
