package write

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Sriram-PR/ghv/internal/draft"
	"github.com/Sriram-PR/ghv/internal/harden"
)

func commentRequest(t *testing.T) Request {
	t.Helper()
	d := draft.Draft{Repo: "o/r", CreatedAt: time.Now(), Fields: draft.CommentFields{Number: 7, Body: "hi"}}
	r, err := Build(d, "o/r")
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestSendRequest(t *testing.T) {
	gh := &fakeGitHub{status: 201, body: `{"html_url":"https://github.com/o/r/issues/7#issuecomment-1","user":{"login":"Sriram-PR"}}`}
	res := Send(context.Background(), harden.Client(gh), []byte(testToken), commentRequest(t))
	if res.Fate != Done || res.Status != 201 || res.URL != "https://github.com/o/r/issues/7#issuecomment-1" || res.Author != "Sriram-PR" || res.Message != "" {
		t.Fatalf("result %+v", res)
	}
	if len(gh.reqs) != 1 {
		t.Fatalf("%d requests", len(gh.reqs))
	}
	r := gh.reqs[0]
	body, _ := io.ReadAll(r.Body)
	if r.Method != "POST" || r.URL.String() != "https://api.github.com/repos/o/r/issues/7/comments" || string(body) != `{"body":"hi"}` {
		t.Fatalf("sent %s %s %s", r.Method, r.URL, body)
	}
	for k, v := range map[string]string{
		"Authorization":        "Bearer " + testToken,
		"Content-Type":         "application/json",
		"Accept":               "application/vnd.github+json",
		"X-Github-Api-Version": "2022-11-28",
		"User-Agent":           "ghv-apply",
	} {
		if got := r.Header.Get(k); got != v {
			t.Errorf("%s: %q", k, got)
		}
	}
}

func TestSendReadsWhatGitHubMade(t *testing.T) {
	gh := &fakeGitHub{status: 202, body: `{"full_name":"Sriram-PR/moby","html_url":"https://github.com/Sriram-PR/moby","owner":{"login":"Sriram-PR"}}`}
	d := draft.Draft{Repo: "moby/moby", CreatedAt: time.Now(), Fields: draft.ForkFields{}}
	r, _ := Build(d, "moby/moby")
	res := Send(context.Background(), harden.Client(gh), []byte(testToken), r)
	if res.Fate != Done || res.FullName != "Sriram-PR/moby" || res.Author != "Sriram-PR" {
		t.Fatalf("fork %+v", res)
	}

	gh = &fakeGitHub{status: 201, body: `{"html_url":"https://github.com/o/r/pull/9","user":{"login":"Sriram-PR"},"head":{"sha":"abc"}}`}
	d = draft.Draft{Repo: "o/r", CreatedAt: time.Now(), Fields: draft.PRCreateFields{Base: "main", Head: "Sriram-PR:x", HeadSHA: strings.Repeat("a", 40), Title: "T"}}
	r, _ = Build(d, "o/r")
	if res := Send(context.Background(), harden.Client(gh), []byte(testToken), r); res.Fate != Done || res.HeadSHA != "abc" {
		t.Fatalf("pull request %+v", res)
	}
}

func TestSendFates(t *testing.T) {
	limited := http.Header{"X-Ratelimit-Remaining": {"0"}}
	for _, c := range []struct {
		name   string
		status int
		header http.Header
		body   string
		fate   Fate
		msg    string
	}{
		{"created without a URL", 201, nil, `{}`, Unknown, "GitHub answered 201 without the URL of what it made"},
		{"created, unreadable", 201, nil, `{"html_url":`, Unknown, "GitHub answered 201 without the URL of what it made"},
		{"validation", 422, nil, `{"message":"Validation Failed","errors":[{"resource":"PullRequest","code":"custom","message":"A pull request already exists for Sriram-PR:x."},{"resource":"Issue","field":"title","code":"missing_field"}]}`,
			Refused, "GitHub answered 422: Validation Failed (A pull request already exists for Sriram-PR:x.; missing_field title)"},
		{"not found", 404, nil, `{"message":"Not Found"}`, Refused, "GitHub answered 404: Not Found"},
		{"locked", 403, nil, `{"message":"Unable to create comment because issue is locked."}`, Refused, "GitHub answered 403: Unable to create comment because issue is locked."},
		{"gone", 410, nil, ``, Refused, "GitHub answered 410"},
		{"bad token", 401, nil, `{"message":"Bad credentials"}`, NotSent, "GitHub refused the write token (401): it is wrong, expired, or revoked"},
		{"rate limit", 403, limited, `{"message":"API rate limit exceeded"}`, NotSent, "GitHub answered 403: API rate limit exceeded"},
		{"secondary limit", 403, http.Header{"Retry-After": {"60"}}, `{"message":"You have exceeded a secondary rate limit."}`, NotSent, "GitHub answered 403: You have exceeded a secondary rate limit."},
		{"too many", 429, nil, `{"message":"slow down"}`, NotSent, "GitHub answered 429: slow down"},
		{"redirect", 307, http.Header{"Location": {"https://api.github.com/repositories/1/issues/7/comments"}}, ``, NotSent,
			`GitHub answered 307, a redirect to "https://api.github.com/repositories/1/issues/7/comments"; ghv-apply does not follow redirects`},
		{"server error", 502, nil, `<html>`, Unknown, "GitHub answered 502"},
	} {
		t.Run(c.name, func(t *testing.T) {
			gh := &fakeGitHub{status: c.status, header: c.header, body: c.body}
			res := Send(context.Background(), harden.Client(gh), []byte(testToken), commentRequest(t))
			if res.Fate != c.fate || res.Status != c.status || res.Message != c.msg {
				t.Fatalf("got %+v", res)
			}
			if len(gh.reqs) != 1 {
				t.Fatalf("%d requests: a write is never retried or redirected", len(gh.reqs))
			}
		})
	}
}

type failing struct{ n int }

func (f *failing) RoundTrip(*http.Request) (*http.Response, error) {
	f.n++
	return nil, errors.New("connection reset by peer")
}

func TestSendNetworkErrorIsUnknown(t *testing.T) {
	f := &failing{}
	res := Send(context.Background(), harden.Client(f), []byte(testToken), commentRequest(t))
	if res.Fate != Unknown || !strings.Contains(res.Message, "connection reset by peer") || f.n != 1 {
		t.Fatalf("result %+v after %d tries", res, f.n)
	}
}

func TestSendRefusesAnUnclassicToken(t *testing.T) {
	gh := &fakeGitHub{status: 201}
	res := Send(context.Background(), harden.Client(gh), []byte("github_pat_x"), commentRequest(t))
	if res.Fate != NotSent || res.Message != ErrNotClassic.Error() || len(gh.reqs) != 0 {
		t.Fatalf("result %+v, %d requests", res, len(gh.reqs))
	}
}

func TestReaderSendsTheTokenWithGETsOnly(t *testing.T) {
	gh := &fakeGitHub{status: 200, body: `{}`}
	c := &http.Client{Transport: Reader(harden.Client(gh).Transport, []byte(testToken))}
	req, _ := http.NewRequest("GET", "https://api.github.com/repos/o/r", nil)
	req.Header.Set("Accept", "application/vnd.github.diff")
	resp, err := c.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if r := gh.reqs[0]; r.Header.Get("Authorization") != "Bearer "+testToken || r.Header.Get("Accept") != "application/vnd.github.diff" {
		t.Fatalf("headers %v", r.Header)
	}
	if req.Header.Get("Authorization") != "" {
		t.Fatal("the caller's request was changed")
	}
	for _, m := range []string{"POST", "PATCH", "PUT", "DELETE", "HEAD"} {
		req, _ := http.NewRequest(m, "https://api.github.com/repos/o/r", strings.NewReader("{}"))
		if _, err := c.Do(req); err == nil || !strings.Contains(err.Error(), "ghv-apply reads with the write token only by GET") {
			t.Errorf("%s: %v", m, err)
		}
	}
	c = &http.Client{Transport: Reader(harden.Client(gh).Transport, []byte("github_pat_x"))}
	req, _ = http.NewRequest("GET", "https://api.github.com/repos/o/r", nil)
	if _, err := c.Do(req); err == nil {
		t.Fatal("an unclassic token was sent")
	}
	if len(gh.reqs) != 1 {
		t.Fatalf("%d requests reached GitHub", len(gh.reqs))
	}
}
