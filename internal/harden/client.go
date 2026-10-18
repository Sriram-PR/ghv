package harden

import (
	"crypto/tls"
	"fmt"
	"net/http"
	"time"
)

const APIHost = "api.github.com"

// Transport ignores proxy variables. System roots load on the first TLS
// handshake, after Init has dropped SSL_CERT_FILE and SSL_CERT_DIR.
func Transport() *http.Transport {
	return &http.Transport{
		Proxy:                 nil,
		TLSClientConfig:       &tls.Config{MinVersion: tls.VersionTLS12},
		ForceAttemptHTTP2:     true,
		TLSHandshakeTimeout:   10 * time.Second,
		ResponseHeaderTimeout: 30 * time.Second,
		MaxIdleConns:          4,
		IdleConnTimeout:       90 * time.Second,
	}
}

// Client sends through rt to https://api.github.com only, and returns a
// redirect as the response instead of following it.
func Client(rt http.RoundTripper) *http.Client {
	return &http.Client{
		Transport:     apiOnly{rt},
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		Timeout:       30 * time.Second,
	}
}

type apiOnly struct{ rt http.RoundTripper }

func (a apiOnly) RoundTrip(r *http.Request) (*http.Response, error) {
	if r.URL.Scheme != "https" || r.URL.Host != APIHost || r.URL.User != nil {
		if r.Body != nil {
			r.Body.Close()
		}
		return nil, fmt.Errorf("ghv-apply talks to https://%s only, not %s://%s", APIHost, r.URL.Scheme, r.URL.Host)
	}
	return a.rt.RoundTrip(r)
}
