package main

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Sriram-PR/ghv/internal/draft"
	"github.com/Sriram-PR/ghv/internal/fetch"
	"github.com/Sriram-PR/ghv/internal/write"
)

// limitWatch notes when GitHub answers that the anonymous rate limit is
// used up, and when it resets.
type limitWatch struct {
	rt    http.RoundTripper
	hit   bool
	reset time.Time
}

func (w *limitWatch) RoundTrip(r *http.Request) (*http.Response, error) {
	resp, err := w.rt.RoundTrip(r)
	if err == nil && (resp.StatusCode == http.StatusForbidden || resp.StatusCode == http.StatusTooManyRequests) &&
		resp.Header.Get("X-Ratelimit-Remaining") == "0" {
		w.hit = true
		if n, err := strconv.ParseInt(resp.Header.Get("X-Ratelimit-Reset"), 10, 64); err == nil {
			w.reset = time.Unix(n, 0).UTC()
		}
	}
	return resp, err
}

func withTransport(c *http.Client, rt http.RoundTripper) *http.Client {
	return &http.Client{Transport: rt, CheckRedirect: c.CheckRedirect, Timeout: c.Timeout}
}

// gather reads a draft's context. When the anonymous rate limit runs out,
// it asks once a session whether to read with the write token, by GET
// only, and reads the context again if the reviewer says yes.
func (ss *session) gather(ctx context.Context, d draft.Draft) (reviewContext, error) {
	rc := ss.rv.context(ctx, d)
	if ss.watch == nil || !ss.watch.hit {
		return rc, nil
	}
	w := ss.watch
	ss.watch = nil
	when := ""
	if !w.reset.IsZero() {
		when = " (it resets at " + w.reset.Format("15:04") + " UTC)"
	}
	ss.say("GitHub's anonymous rate limit is used up%s, so some context is missing.", when)
	ans, err := ss.tty.ReadLine("Read the rest with your write token, by GET requests only? [y/N] ")
	if err != nil {
		return rc, err
	}
	if a := strings.ToLower(strings.TrimSpace(ans)); a != "y" && a != "yes" {
		return rc, nil
	}
	ss.rv.fc = fetch.New(withTransport(ss.e.client, write.Reader(ss.e.client.Transport, ss.tok)))
	if ss.rv.knownErr != nil {
		ss.rv.knownDone, ss.rv.knownErr = false, nil
	}
	return ss.rv.context(ctx, d), nil
}
