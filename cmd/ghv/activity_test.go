package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
)

func activityRoutes(t *testing.T, since string) *routes {
	q := "?per_page=100"
	if since != "" {
		q = "?since=" + since + "&per_page=100"
	}
	return newRoutes(t, map[string]string{
		"/repos/o/r/issues/7/comments" + q: `[` +
			`{"id":11,"user":{"login":"alice","type":"User"},"created_at":"2026-10-16T10:00:00Z","html_url":"c11","body":"First\nsecond \u001b[1mline"},` +
			`{"id":12,"user":{"login":"codecov[bot]","type":"Bot"},"created_at":"2026-10-16T12:00:00Z","html_url":"c12","body":"Coverage"}]`,
		"/repos/o/r/pulls/7/reviews?per_page=100": `[` +
			`{"id":21,"user":{"login":"bob","type":"User"},"state":"APPROVED","submitted_at":"2026-10-16T11:00:00Z","html_url":"r21","body":""},` +
			`{"id":22,"user":{"login":"bob","type":"User"},"state":"PENDING","html_url":"r22","body":"draft"},` +
			`{"id":23,"user":{"login":"carol","type":"User"},"state":"CHANGES_REQUESTED","submitted_at":"2026-10-15T09:00:00Z","html_url":"r23","body":"Old"}]`,
		"/repos/o/r/pulls/7/comments" + q: `[` +
			`{"id":31,"user":{"login":"bob","type":"User"},"created_at":"2026-10-16T10:30:00Z","html_url":"p31","path":"cmd/x.go","line":12,"body":"Nit"},` +
			`{"id":32,"user":{"login":"alice","type":"User"},"created_at":"2026-10-16T10:45:00Z","html_url":"p32","path":"cmd/x.go","line":null,"original_line":9,"in_reply_to_id":31,"body":"Done"}]`,
	})
}

func TestPRActivity(t *testing.T) {
	e, _, _ := testEnv(t, activityRoutes(t, ""))
	r := runWith(t, registry, e, "pr", "activity", "7", "-R", "o/r")
	want := "2026-10-15T09:00:00Z  carol  review CHANGES_REQUESTED\n    Old\n\n" +
		"2026-10-16T10:00:00Z  alice  comment\n    First\n    second <U+001B>[1mline\n\n" +
		"2026-10-16T10:30:00Z  bob  review comment 31 on cmd/x.go:12\n    Nit\n\n" +
		"2026-10-16T10:45:00Z  alice  review comment 32 on cmd/x.go:9 (outdated), reply to 31\n    Done\n\n" +
		"2026-10-16T11:00:00Z  bob  review APPROVED\n\n" +
		"2026-10-16T12:00:00Z  codecov[bot]  comment\n    Coverage\n\n"
	if r.code != 0 || r.stdout != want {
		t.Fatalf("exit %d, stderr %q, stdout:\n%s\nwant:\n%s", r.code, r.stderr, r.stdout, want)
	}
}

func TestPRActivitySinceAndNoBots(t *testing.T) {
	rt := activityRoutes(t, "2026-10-16T10%3A15%3A00Z")
	e, _, _ := testEnv(t, rt)
	r := runWith(t, registry, e, "pr", "activity", "7", "-R", "o/r", "--since", "2026-10-16T10:15:00Z", "--no-bots", "--json")
	var v struct {
		Repo   string
		Number int
		Events []struct {
			Kind, Author string
			InReplyTo    int64 `json:"in_reply_to"`
		}
	}
	if err := json.Unmarshal([]byte(r.stdout), &v); err != nil {
		t.Fatalf("exit %d, stderr %q, asked %q: %v", r.code, r.stderr, rt.uris(), err)
	}
	var got []string
	for _, ev := range v.Events {
		got = append(got, fmt.Sprintf("%s:%s:%d", ev.Kind, ev.Author, ev.InReplyTo))
	}
	if v.Repo != "o/r" || v.Number != 7 || strings.Join(got, ",") != "review_comment:bob:0,review_comment:alice:31,review:bob:0" {
		t.Fatalf("events %v", got)
	}
}

func TestIssueActivity(t *testing.T) {
	e, _, _ := testEnv(t, activityRoutes(t, ""))
	r := runWith(t, registry, e, "issue", "activity", "7", "-R", "o/r", "--no-bots")
	want := "2026-10-16T10:00:00Z  alice  comment\n    First\n    second <U+001B>[1mline\n\n"
	if r.code != 0 || r.stdout != want {
		t.Fatalf("exit %d, stderr %q, stdout:\n%s", r.code, r.stderr, r.stdout)
	}
}

func TestIsBot(t *testing.T) {
	for u, want := range map[githubUser]bool{
		{Login: "codecov[bot]", Type: "Bot"}:   true,
		{Login: "renovate[bot]", Type: "User"}: true,
		{Login: "copilot", Type: "Bot"}:        true,
		{Login: "alice", Type: "User"}:         false,
	} {
		if isBot(u) != want {
			t.Errorf("isBot(%v) = %v", u, !want)
		}
	}
}
