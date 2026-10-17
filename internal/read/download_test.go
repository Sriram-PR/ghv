package read

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

const blobURL = "https://productionresultssa1.blob.core.windows.net/actions-results/x/job-logs.txt?sig=s"

func TestDownloadFollowsRedirectWithoutToken(t *testing.T) {
	var accept, blobAuth string
	c := fakeClient("ghp_x", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Oauth-Scopes", "")
		switch r.URL.Host {
		case "api.github.com":
			accept = r.Header.Get("Accept")
			http.Redirect(w, r, blobURL, http.StatusFound)
		default:
			blobAuth = r.Header.Get("Authorization")
			io.WriteString(w, "hello")
		}
	})
	var buf bytes.Buffer
	n, err := c.Download(context.Background(), "repos/o/r/actions/jobs/1/logs", "application/octet-stream", &buf, 10)
	if err != nil || n != 5 || buf.String() != "hello" {
		t.Fatalf("n %d, err %v, body %q", n, err, buf.String())
	}
	if accept != "application/octet-stream" || blobAuth != "" {
		t.Fatalf("Accept %q, blob Authorization %q", accept, blobAuth)
	}
}

func TestDownloadCapsTheBody(t *testing.T) {
	c := fakeClient("", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, strings.Repeat("x", 11))
	})
	var buf bytes.Buffer
	_, err := c.Download(context.Background(), "repos/o/r/releases/assets/1", "", &buf, 10)
	if !errors.Is(err, ErrTooLarge) || buf.Len() > 10 {
		t.Fatalf("err %v, wrote %d bytes", err, buf.Len())
	}
}

func TestDownloadMapsErrors(t *testing.T) {
	tests := []struct {
		status int
		token  string
		code   ErrorCode
	}{
		{404, "", CodeNotFound},
		{401, "", CodeTokenRequired},
		{403, "ghp_x", CodeForbidden},
	}
	for _, tt := range tests {
		c := fakeClient(tt.token, func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Oauth-Scopes", "")
			w.WriteHeader(tt.status)
			io.WriteString(w, `{"message":"m"}`)
		})
		var buf bytes.Buffer
		_, err := c.Download(context.Background(), "repos/o/r/actions/runs/1/logs", "", &buf, 10)
		var ae *APIError
		if !errors.As(err, &ae) || ae.Code != tt.code || buf.Len() != 0 {
			t.Errorf("status %d: err %v, wrote %q", tt.status, err, buf.String())
		}
	}
}

func TestDownloadRefusesUnguardedRedirect(t *testing.T) {
	c := fakeClient("", func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "https://example.com/x", http.StatusFound)
	})
	_, err := c.Download(context.Background(), "repos/o/r/releases/assets/1", "", io.Discard, 10)
	if !errors.Is(err, ErrReadOnly) {
		t.Fatalf("err %v", err)
	}
}

// stallBody sends its data, then blocks until the request is cancelled.
type stallBody struct {
	ctx  context.Context
	data []byte
}

func (b *stallBody) Read(p []byte) (int, error) {
	if len(b.data) > 0 {
		n := copy(p, b.data)
		b.data = b.data[n:]
		return n, nil
	}
	<-b.ctx.Done()
	return 0, b.ctx.Err()
}

func (b *stallBody) Close() error { return nil }

type stallTransport struct{}

func (stallTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return &http.Response{StatusCode: 200, Header: http.Header{}, Request: req,
		Body: &stallBody{ctx: req.Context(), data: []byte("ab")}}, nil
}

func TestDownloadFailsWhenTheBodyStalls(t *testing.T) {
	c := NewClient(Options{Transport: stallTransport{}})
	c.stall = 50 * time.Millisecond
	var buf bytes.Buffer
	start := time.Now()
	n, err := c.Download(context.Background(), "repos/o/r/actions/runs/1/logs", "", &buf, 10)
	if !errors.Is(err, ErrStalled) || n != 2 || buf.String() != "ab" || time.Since(start) > 5*time.Second {
		t.Fatalf("n %d, err %v, after %s", n, err, time.Since(start))
	}
}
