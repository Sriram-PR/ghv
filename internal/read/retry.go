package read

import (
	"context"
	"errors"
	"net/http"
	"time"
)

const (
	maxRetries = 3
	maxWait    = time.Minute
)

// do sends the request build returns, and builds and sends it again after
// a 5xx, a network error, or a rate limit that lifts within a minute.
func (c *Client) do(ctx context.Context, build func() (*http.Request, error)) (*Response, error) {
	for attempt := 0; ; attempt++ {
		req, err := build()
		if err != nil {
			return nil, err
		}
		resp, err := c.send(req)
		if err == nil || ctx.Err() != nil {
			return resp, err
		}
		wait, ok := retryDelay(err, attempt, c.now())
		if !ok {
			return nil, err
		}
		if err := c.sleep(ctx, wait); err != nil {
			return nil, err
		}
	}
}

func retryDelay(err error, attempt int, now time.Time) (time.Duration, bool) {
	if attempt >= maxRetries {
		return 0, false
	}
	backoff := time.Second << attempt
	var ae *APIError
	if errors.As(err, &ae) {
		switch {
		case ae.Code == CodeServer:
			return backoff, true
		case ae.Code == CodeSecondaryLimit && ae.RetryAfter <= maxWait:
			return ae.RetryAfter, true
		case ae.Code == CodeRateLimited && !ae.Reset.IsZero() && ae.Reset.Sub(now) <= maxWait:
			// Reset has whole seconds; wait one more.
			return max(ae.Reset.Sub(now)+time.Second, time.Second), true
		}
		return 0, false
	}
	var te *TokenError
	if errors.As(err, &te) || errors.Is(err, ErrReadOnly) || errors.Is(err, ErrTooLarge) {
		return 0, false
	}
	return backoff, true
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
