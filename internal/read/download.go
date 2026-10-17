package read

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync/atomic"
	"time"
)

var ErrStalled = errors.New("the download stalled")

// Download GETs https://api.github.com/<path> and streams the body to w,
// following redirects through the guard, so download hosts never see the
// token. It has no total timeout: it fails when no data arrives for 30
// seconds, or when the body passes max bytes. Downloads are neither cached
// nor retried, since a retry would resend a partial stream.
func (c *Client) Download(ctx context.Context, path, accept string, w io.Writer, max int64) (int64, error) {
	u, err := APIURL(path)
	if err != nil {
		return 0, err
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	var stalled atomic.Bool
	timer := time.AfterFunc(c.stall, func() {
		stalled.Store(true)
		cancel()
	})
	defer timer.Stop()
	fail := func(err error) error {
		if stalled.Load() {
			return fmt.Errorf("%w: no data for %s", ErrStalled, c.stall)
		}
		return err
	}
	req, err := c.newRequest(ctx, u.String(), accept)
	if err != nil {
		return 0, err
	}
	resp, err := c.dl.Do(req)
	if err != nil {
		return 0, fail(unwrapURLError(err))
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		if err := errorFor(resp.StatusCode, resp.Header, body, c.Authenticated()); err != nil {
			return 0, err
		}
		return 0, &APIError{Status: resp.StatusCode, Code: CodeHTTP, Message: fmt.Sprintf("GitHub answered %s", resp.Status)}
	}
	var n int64
	buf := make([]byte, 32<<10)
	for {
		timer.Reset(c.stall)
		k, rerr := resp.Body.Read(buf)
		if n+int64(k) > max {
			return n, fmt.Errorf("%w (over %d bytes)", ErrTooLarge, max)
		}
		if _, err := w.Write(buf[:k]); err != nil {
			return n, err
		}
		n += int64(k)
		if rerr == io.EOF {
			return n, nil
		}
		if rerr != nil {
			return n, fail(rerr)
		}
	}
}
