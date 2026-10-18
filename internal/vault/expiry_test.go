package vault

import (
	"testing"
	"time"
)

func TestExpiryWarning(t *testing.T) {
	now := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	at := func(d time.Duration) *time.Time { t := now.Add(d); return &t }
	for _, tt := range []struct {
		exp  *time.Time
		want string
	}{
		{nil, ""},
		{at(15 * 24 * time.Hour), ""},
		{at(14 * 24 * time.Hour), "the write token expires on 2026-11-01, in 14 days; replace it with ghv-apply rotate"},
		{at(36 * time.Hour), "the write token expires on 2026-10-20, in 1 day; replace it with ghv-apply rotate"},
		{at(time.Hour), "the write token expires on 2026-10-18, in 0 days; replace it with ghv-apply rotate"},
		{at(-time.Hour), "the write token expired on 2026-10-18; replace it with ghv-apply rotate"},
	} {
		if got := (Secret{Expires: tt.exp}).ExpiryWarning(now); got != tt.want {
			t.Errorf("expiry %v: %q, want %q", tt.exp, got, tt.want)
		}
	}
}
