package fetch

import (
	"context"
	"fmt"
	"strings"
	"testing"
)

func TestEvents(t *testing.T) {
	page := func(n, from int) string {
		evs := make([]string, n)
		for i := range evs {
			evs[i] = fmt.Sprintf(`{"type":"PushEvent","repo":{"name":"me/r%d"},"created_at":"2026-10-18T12:00:00Z","payload":{}}`, from+i)
		}
		return "[" + strings.Join(evs, ",") + "]"
	}
	f, c := newFake(map[string]reply{
		"/users/me/events/public?page=1&per_page=100": {status: 200, body: `[
			{"type":"IssueCommentEvent","repo":{"name":"o/r"},"created_at":"2026-10-18T12:00:01Z","payload":{"action":"created",
				"comment":{"html_url":"https://github.com/o/r/issues/7#issuecomment-1"},"issue":{"html_url":"https://github.com/o/r/issues/7"}}},
			{"type":"PullRequestEvent","repo":{"name":"o/r"},"created_at":"2026-10-18T12:00:02Z","payload":{"action":"opened","number":5,
				"pull_request":{"id":1,"number":5,"url":"https://api.github.com/repos/o/r/pulls/5"}}},
			{"type":"PullRequestReviewCommentEvent","repo":{"name":"o/r"},"created_at":"2026-10-18T12:00:03Z","payload":{"action":"created",
				"comment":{"html_url":"https://github.com/o/r/pull/8#discussion_r2","pull_request_review_id":77}}},
			{"type":"PullRequestReviewEvent","repo":{"name":"o/r"},"created_at":"2026-10-18T12:00:04Z","payload":{"action":"created",
				"review":{"id":77,"html_url":"https://github.com/o/r/pull/8#pullrequestreview-77"}}},
			{"type":"ForkEvent","repo":{"name":"o/r"},"created_at":"2026-10-18T12:00:05Z","payload":{"action":"forked",
				"forkee":{"full_name":"me/r","html_url":"https://github.com/me/r"}}},` + page(95, 0)[1:]},
		"/users/me/events/public?page=2&per_page=100": {status: 200, body: page(30, 95)},
	})
	evs, err := c.Events(context.Background(), "me")
	if err != nil || len(evs) != 130 {
		t.Fatalf("%d events, %v", len(evs), err)
	}
	if len(f.reqs) != 2 {
		t.Fatalf("%d requests: a short page is the last", len(f.reqs))
	}
	p := evs[0].Payload
	if evs[0].Type != "IssueCommentEvent" || evs[0].Repo.Name != "o/r" || p.Action != "created" || p.Comment.HTMLURL != "https://github.com/o/r/issues/7#issuecomment-1" {
		t.Fatalf("comment %+v", evs[0])
	}
	if p := evs[1].Payload; p.Action != "opened" || p.Number != 5 {
		t.Fatalf("pull request %+v", evs[1])
	}
	if p := evs[2].Payload; p.Comment.PullRequestReviewID != 77 {
		t.Fatalf("review comment %+v", evs[2])
	}
	if p := evs[3].Payload; p.Review.ID != 77 || p.Review.HTMLURL != "https://github.com/o/r/pull/8#pullrequestreview-77" {
		t.Fatalf("review %+v", evs[3])
	}
	if p := evs[4].Payload; p.Forkee.HTMLURL != "https://github.com/me/r" {
		t.Fatalf("fork %+v", evs[4])
	}
	if evs[129].Repo.Name != "me/r124" || evs[129].CreatedAt.IsZero() {
		t.Fatalf("last %+v", evs[129])
	}
}

func TestEventsStopsAtThreePages(t *testing.T) {
	full := "[" + strings.TrimSuffix(strings.Repeat(`{"type":"WatchEvent","repo":{"name":"o/r"},"payload":{}},`, 100), ",") + "]"
	f, c := newFake(map[string]reply{
		"/users/me/events/public?page=1&per_page=100": {status: 200, body: full},
		"/users/me/events/public?page=2&per_page=100": {status: 200, body: full},
		"/users/me/events/public?page=3&per_page=100": {status: 200, body: full},
		"/users/me/events/public?page=4&per_page=100": {status: 200, body: full},
	})
	evs, err := c.Events(context.Background(), "me")
	if err != nil || len(evs) != 300 || len(f.reqs) != 3 {
		t.Fatalf("%d events in %d requests, %v", len(evs), len(f.reqs), err)
	}
}

func TestEventsEscapesLogin(t *testing.T) {
	f, c := newFake(map[string]reply{
		"/users/a%2Fb%3Fx/events/public?page=1&per_page=100": {status: 200, body: `[]`},
	})
	if _, err := c.Events(context.Background(), "a/b?x"); err != nil || len(f.reqs) != 1 {
		t.Fatalf("%d requests, %v", len(f.reqs), err)
	}
}
