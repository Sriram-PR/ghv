package read

import (
	"context"
	"net/http"
	"slices"
	"strconv"
	"testing"
	"time"
)

func TestPrimaryLimitResettingWithinAMinuteIsWaitedOut(t *testing.T) {
	now := time.Unix(1792246087, 0)
	tests := []struct {
		name  string
		reset int64
		ok    bool
		slept []time.Duration
	}{
		{"resets in 30s", now.Unix() + 30, true, []time.Duration{31 * time.Second}},
		{"resets in 60s", now.Unix() + 60, true, []time.Duration{61 * time.Second}},
		{"resets in 61s", now.Unix() + 61, false, nil},
		{"reset already passed", now.Unix() - 5, true, []time.Duration{time.Second}},
		{"resets in 2 minutes", now.Unix() + 120, false, nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c, slept, calls := scripted(t, []int{403, 200}, http.Header{
				"X-Ratelimit-Remaining": {"0"},
				"X-Ratelimit-Reset":     {strconv.FormatInt(tt.reset, 10)},
				"X-Ratelimit-Resource":  {"search"},
			})
			c.now = func() time.Time { return now }
			_, err := c.Get(context.Background(), "search/issues?q=is:pr", "")
			if (err == nil) != tt.ok || !slices.Equal(*slept, tt.slept) {
				t.Fatalf("err = %v, calls = %d, slept = %v", err, *calls, *slept)
			}
		})
	}
}
