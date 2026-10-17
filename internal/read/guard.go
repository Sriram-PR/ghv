// Package read is ghv's GitHub client. Every request passes Guard.
package read

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"path"
	"strings"
)

var ErrReadOnly = errors.New("ghv is read-only; draft this with 'ghv <command>'")

const (
	apiHost        = "api.github.com"
	blobSuffix     = ".blob.core.windows.net"
	maxGraphQLBody = 1 << 20
)

var downloadHosts = map[string]bool{
	"github.com":                    true,
	"objects.githubusercontent.com": true,
}

var refusedHeaders = []string{"X-HTTP-Method-Override", "X-HTTP-Method", "X-Method-Override", "Content-Encoding"}

// Guard admits GETs to GitHub's hosts and query-only GraphQL POSTs, and
// refuses everything else before it reaches Next. http.Client calls it
// again for every redirect hop.
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
	case http.MethodPost:
		return admitGraphQL(req)
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

type graphQLBody struct {
	Query     string          `json:"query"`
	Variables json.RawMessage `json:"variables,omitempty"`
}

func encodeGraphQLBody(query string, variables map[string]any) ([]byte, error) {
	b := graphQLBody{Query: query}
	if len(variables) > 0 {
		v, err := json.Marshal(variables)
		if err != nil {
			return nil, err
		}
		b.Variables = v
	}
	return json.Marshal(b)
}

// NewGraphQLRequest builds the only POST ghv sends.
func NewGraphQLRequest(ctx context.Context, query string, variables map[string]any) (*http.Request, error) {
	if err := classifyQuery(query); err != nil {
		return nil, fmt.Errorf("%w (%v)", ErrReadOnly, err)
	}
	body, err := encodeGraphQLBody(query, variables)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "https://"+apiHost+"/graphql", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	return req, nil
}

// admitGraphQL accepts a body only if it is byte-for-byte what
// encodeGraphQLBody produces, so duplicate keys, case-folded keys, and
// extra fields cannot smuggle in a second document. The bytes classified
// are the bytes sent.
func admitGraphQL(req *http.Request) (*http.Request, error) {
	if req.URL.Host != apiHost || req.URL.Path != "/graphql" || req.URL.RawQuery != "" {
		return nil, errors.New("POST is only allowed to https://api.github.com/graphql")
	}
	if req.Body == nil || req.Body == http.NoBody {
		return nil, errors.New("POST without a body")
	}
	raw, err := io.ReadAll(io.LimitReader(req.Body, maxGraphQLBody+1))
	req.Body.Close()
	if err != nil {
		return nil, err
	}
	if len(raw) > maxGraphQLBody {
		return nil, fmt.Errorf("GraphQL body over %d bytes", maxGraphQLBody)
	}
	var b graphQLBody
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&b); err != nil {
		return nil, fmt.Errorf("GraphQL body: %v", err)
	}
	canon, err := json.Marshal(b)
	if err != nil || !bytes.Equal(canon, raw) {
		return nil, errors.New("GraphQL body is not in ghv's encoding")
	}
	if err := classifyQuery(b.Query); err != nil {
		return nil, err
	}
	out := req.Clone(req.Context())
	out.Body = io.NopCloser(bytes.NewReader(raw))
	out.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(raw)), nil }
	out.ContentLength = int64(len(raw))
	return out, nil
}
