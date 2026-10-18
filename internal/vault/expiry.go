package vault

import (
	"fmt"
	"time"
)

const expiryNotice = 14 * 24 * time.Hour

// ExpiryWarning is "" unless the token expires within 14 days of now.
func (s Secret) ExpiryWarning(now time.Time) string {
	if s.Expires == nil {
		return ""
	}
	left := s.Expires.Sub(now)
	day := s.Expires.UTC().Format(time.DateOnly)
	switch {
	case left <= 0:
		return fmt.Sprintf("the write token expired on %s; replace it with ghv-apply rotate", day)
	case left <= expiryNotice:
		n := int(left / (24 * time.Hour))
		unit := "days"
		if n == 1 {
			unit = "day"
		}
		return fmt.Sprintf("the write token expires on %s, in %d %s; replace it with ghv-apply rotate", day, n, unit)
	}
	return ""
}
