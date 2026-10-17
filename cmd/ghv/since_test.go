package main

import (
	"testing"
	"time"
)

func TestParseSince(t *testing.T) {
	now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		in   string
		want time.Time
	}{
		{"2026-10-06T07:00:00Z", time.Date(2026, 10, 6, 7, 0, 0, 0, time.UTC)},
		{"2026-10-06T09:00:00+02:00", time.Date(2026, 10, 6, 7, 0, 0, 0, time.UTC)},
		{"2026-10-06", time.Date(2026, 10, 6, 0, 0, 0, 0, time.UTC)},
		{"36h", now.Add(-36 * time.Hour)},
		{"7d", now.AddDate(0, 0, -7)},
		{"90m", now.Add(-90 * time.Minute)},
	}
	for _, tt := range tests {
		got, err := parseSince(tt.in, now)
		if err != nil || !got.Equal(tt.want) {
			t.Errorf("parseSince(%q) = %v, %v; want %v", tt.in, got, err, tt.want)
		}
	}
	for _, bad := range []string{"", "0d", "-1d", "7w", "yesterday", "2026-13-01", "1e3h", "99999999999d"} {
		if _, err := parseSince(bad, now); err == nil {
			t.Errorf("parseSince(%q) accepted", bad)
		}
	}
}
