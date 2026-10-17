package read

import (
	"context"
	"errors"
	"io"
	"net/http"
	"slices"
	"testing"
	"time"

	"github.com/Sriram-PR/ghv/internal/read/readtest"
)

// scripted answers the nth request with statuses[n], repeating the last.
func scripted(t *testing.T, statuses []int, header http.Header) (*Client, *[]time.Duration, *int) {
	t.Helper()
	calls := 0
	c := fakeClient("", func(w http.ResponseWriter, r *http.Request) {
		s := statuses[min(calls, len(statuses)-1)]
		calls++
		if s != 200 {
			for k, v := range header {
				w.Header()[k] = v
			}
		}
		w.WriteHeader(s)
		io.WriteString(w, `{"message":"m"}`)
	})
	var slept []time.Duration
	c.sleep = func(ctx context.Context, d time.Duration) error {
		slept = append(slept, d)
		return nil
	}
	return c, &slept, &calls
}

func TestRetries(t *testing.T) {
	sec := time.Second
	tests := []struct {
		name     string
		statuses []int
		header   http.Header
		ok       bool
		calls    int
		slept    []time.Duration
	}{
		{"5xx then success", []int{502, 503, 200}, nil, true, 3, []time.Duration{sec, 2 * sec}},
		{"5xx gives up after 3 retries", []int{500}, nil, false, 4, []time.Duration{sec, 2 * sec, 4 * sec}},
		{"secondary limit waits Retry-After", []int{403, 200}, http.Header{"Retry-After": {"30"}}, true, 2, []time.Duration{30 * sec}},
		{"secondary limit over a minute is reported", []int{403, 200}, http.Header{"Retry-After": {"120"}}, false, 1, nil},
		{"primary limit is reported", []int{403, 200}, http.Header{"X-Ratelimit-Remaining": {"0"}}, false, 1, nil},
		{"404 is not retried", []int{404, 200}, nil, false, 1, nil},
		{"401 is not retried", []int{401, 200}, nil, false, 1, nil},
		{"422 is not retried", []int{422, 200}, nil, false, 1, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, slept, calls := scripted(t, tt.statuses, tt.header)
			_, err := c.Get(context.Background(), "repos/o/r", "")
			if (err == nil) != tt.ok || *calls != tt.calls || !slices.Equal(*slept, tt.slept) {
				t.Fatalf("err = %v, calls = %d, slept = %v", err, *calls, *slept)
			}
		})
	}
}

type flaky struct {
	fails int
	calls int
	next  http.RoundTripper
}

func (f *flaky) RoundTrip(req *http.Request) (*http.Response, error) {
	f.calls++
	if f.calls <= f.fails {
		return nil, errors.New("connection reset by peer")
	}
	return f.next.RoundTrip(req)
}

func TestRetriesNetworkErrors(t *testing.T) {
	f := &flaky{fails: 2, next: readtest.Transport{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})}}
	c := NewClient(Options{Transport: f})
	c.sleep = func(context.Context, time.Duration) error { return nil }
	if _, err := c.Get(context.Background(), "repos/o/r", ""); err != nil || f.calls != 3 {
		t.Fatalf("err = %v, calls = %d", err, f.calls)
	}
}

func TestNoRetryAfterCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	f := &flaky{fails: 5}
	c := NewClient(Options{Transport: f})
	c.sleep = func(context.Context, time.Duration) error { cancel(); return context.Canceled }
	if _, err := c.Get(ctx, "repos/o/r", ""); !errors.Is(err, context.Canceled) || f.calls != 1 {
		t.Fatalf("err = %v, calls = %d", err, f.calls)
	}
	f = &flaky{fails: 5}
	c = NewClient(Options{Transport: f})
	c.sleep = func(context.Context, time.Duration) error { t.Fatal("slept after cancel"); return nil }
	if _, err := c.Get(ctx, "repos/o/r", ""); err == nil || f.calls > 1 {
		t.Fatalf("err = %v, calls = %d", err, f.calls)
	}
}

func TestNoRetryOfRefusals(t *testing.T) {
	calls := 0
	c := fakeClient("ghp_x", func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("X-Oauth-Scopes", "repo")
	})
	c.sleep = func(context.Context, time.Duration) error { t.Fatal("slept"); return nil }
	if _, err := c.Get(context.Background(), "repos/o/r", ""); err == nil || calls != 1 {
		t.Fatalf("err = %v, calls = %d", err, calls)
	}
	if _, err := c.Get(context.Background(), "graphql", ""); !errors.Is(err, ErrReadOnly) {
		t.Fatalf("err = %v", err)
	}
}

func TestSleepHonoursContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := sleepCtx(ctx, time.Hour); !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
}
