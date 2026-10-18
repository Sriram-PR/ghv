package main

import (
	"strings"
	"testing"
	"time"

	"github.com/Sriram-PR/ghv/internal/audit"
)

const eventsRoute = "/users/Sriram-PR/events/public?page=1&per_page=100"

func TestLogVerify(t *testing.T) {
	ev := func(typ, at, payload string) string {
		return `{"type":"` + typ + `","repo":{"name":"o/r"},"created_at":"2026-10-18T` + at + `Z","payload":` + payload + `}`
	}
	routes := baseRoutes()
	routes[eventsRoute] = ok("[" + strings.Join([]string{
		ev("IssueCommentEvent", "11:10:00", `{"action":"created","comment":{"html_url":"https://github.com/o/r/issues/7#issuecomment-1"}}`),
		ev("IssueCommentEvent", "11:00:00", `{"action":"created","comment":{"html_url":"https://github.com/o/r/issues/7#issuecomment-3"}}`),
		ev("PullRequestEvent", "11:11:00", `{"action":"opened","number":5,"pull_request":{"number":5}}`),
		ev("PullRequestEvent", "11:01:00", `{"action":"opened","number":6,"pull_request":{"number":6}}`),
		ev("PullRequestEvent", "11:12:00", `{"action":"closed","number":5,"pull_request":{"number":5}}`),
		ev("PullRequestReviewCommentEvent", "11:13:00", `{"action":"created","comment":{"html_url":"https://github.com/o/r/pull/8#discussion_r2","pull_request_review_id":77}}`),
		ev("PullRequestReviewEvent", "11:13:01", `{"action":"created","review":{"id":77,"html_url":"https://github.com/o/r/pull/8#pullrequestreview-77"}}`),
		ev("PullRequestReviewEvent", "11:02:00", `{"action":"created","review":{"id":78,"html_url":"https://github.com/o/r/pull/8#pullrequestreview-78"}}`),
		ev("ForkEvent", "11:14:00", `{"action":"forked","forkee":{"html_url":"https://github.com/Sriram-PR/r"}}`),
		ev("PushEvent", "11:15:00", `{}`),
		ev("PushEvent", "11:16:00", `{}`),
		ev("WatchEvent", "11:17:00", `{"action":"started"}`),
	}, ",") + "]")
	ft := answers(t, oldPass)
	e, f := reviewEnv(t, ft, routes)
	day := func(d, h int) time.Time { return time.Date(2026, 10, d, h, 0, 0, 0, time.UTC) }
	for _, en := range []audit.Entry{
		{Time: day(18, 11), Draft: "d-00000000000a", Kind: "comment", Repo: "o/r", URL: "https://github.com/o/r/issues/7#issuecomment-1", Status: 201},
		{Time: day(18, 11), Draft: "d-00000000000b", Kind: "pr.create", Repo: "o/r", URL: "https://github.com/o/r/pull/5", Status: 201},
		{Time: day(18, 11), Draft: "d-00000000000c", Kind: "review.reply", Repo: "o/r", URL: "https://github.com/o/r/pull/8#discussion_r2", Status: 201},
		{Time: day(18, 11), Draft: "d-00000000000d", Kind: "fork", Repo: "o/r", URL: "https://github.com/Sriram-PR/r", Status: 202},
		{Time: day(17, 12), Draft: "d-00000000000e", Kind: "issue.create", Repo: "o/r", URL: "https://github.com/o/r/issues/9", Found: true},
		{Time: day(17, 13), Draft: "d-00000000000f", Kind: "pr.edit", Repo: "o/r", URL: "https://github.com/o/r/pull/5", Status: 200},
		{Time: time.Date(2026, 9, 1, 12, 0, 0, 0, time.UTC), Draft: "d-000000000010", Kind: "comment", Repo: "o/r", URL: "https://github.com/o/r/issues/1#issuecomment-9", Status: 201},
		{Time: day(17, 10), Draft: "d-000000000011", Kind: "issue.create", Repo: "o/r", URL: "https://github.com/o/r/issues/10", Found: true},
	} {
		if err := audit.Append(audit.Path(e.home), en); err != nil {
			t.Fatal(err)
		}
	}
	r := runWith(e, "log", "--verify")
	want := "Public events of Sriram-PR on GitHub: 12, since 2026-09-18 12:00 UTC.\n" +
		"\nMade outside ghv-apply (3):\n" +
		"  2026-10-18 11:00 UTC  comment       o/r  https://github.com/o/r/issues/7#issuecomment-3\n" +
		"  2026-10-18 11:01 UTC  pull request  o/r  https://github.com/o/r/pull/6\n" +
		"  2026-10-18 11:02 UTC  review        o/r  https://github.com/o/r/pull/8#pullrequestreview-78\n" +
		"\nOther events, of kinds ghv-apply does not make: PushEvent 2, PullRequestEvent/closed 1, WatchEvent/started 1.\n" +
		"\nIn the audit log but not in GitHub's events, which can lag by hours and drop a deleted repository's (2):\n" +
		"  2026-10-17 10:00 UTC  d-000000000011  issue.create  o/r  https://github.com/o/r/issues/10 (found later)\n" +
		"  2026-10-17 12:00 UTC  d-00000000000e  issue.create  o/r  https://github.com/o/r/issues/9 (found later)\n" +
		"\nNot checked: 1 pr.edit line (GitHub has no event for an edit), 1 line older than the events GitHub keeps.\n"
	if r.code != 1 || r.stdout != want || r.stderr != "ghv-apply: 3 events were made outside ghv-apply\n" {
		t.Fatalf("exit %d, stderr %q, stdout:\n%s\nwant:\n%s", r.code, r.stderr, r.stdout, want)
	}
	if !strings.HasPrefix(ft.shown(), "Passphrase: ") {
		t.Fatalf("terminal %q", ft.shown())
	}
	for _, rq := range f.reqs {
		if rq.Method != "GET" || rq.Header.Get("Authorization") != "" || rq.URL.Path == "/user" {
			t.Fatalf("%s %s, Authorization %q", rq.Method, rq.URL, rq.Header.Get("Authorization"))
		}
	}
}

func TestLogVerifyAllMadeByGhvApply(t *testing.T) {
	routes := baseRoutes()
	routes[eventsRoute] = ok(`[{"type":"IssueCommentEvent","repo":{"name":"o/r"},"created_at":"2026-10-18T11:10:00Z",
		"payload":{"action":"created","comment":{"html_url":"https://github.com/o/r/issues/7#issuecomment-1"}}}]`)
	e, _ := reviewEnv(t, answers(t, oldPass), routes)
	if err := audit.Append(audit.Path(e.home), audit.Entry{Time: time.Date(2026, 10, 18, 11, 10, 0, 0, time.UTC), Draft: "d-00000000000a",
		Kind: "comment", Repo: "o/r", URL: "https://github.com/o/r/issues/7#issuecomment-1", Status: 201}); err != nil {
		t.Fatal(err)
	}
	r := runWith(e, "log", "--verify")
	want := "Public events of Sriram-PR on GitHub: 1, since 2026-09-18 12:00 UTC.\n\nNothing made outside ghv-apply.\n"
	if r.code != 0 || r.stdout != want || r.stderr != "" {
		t.Fatalf("exit %d, stderr %q, stdout:\n%s", r.code, r.stderr, r.stdout)
	}
}

func TestLogVerifyWrongPassphrase(t *testing.T) {
	e, f := reviewEnv(t, answers(t, "not the passphrase at all"), baseRoutes())
	r := runWith(e, "log", "--verify")
	if r.code != 1 || !strings.Contains(r.stderr, "wrong passphrase") || len(f.reqs) != 0 {
		t.Fatalf("exit %d, stderr %q, %d requests", r.code, r.stderr, len(f.reqs))
	}
}

func TestLogVerifySanitizesOtherEvents(t *testing.T) {
	routes := baseRoutes()
	routes[eventsRoute] = ok(`[{"type":"Evil\u001b]52;c;x\u0007Event","repo":{"name":"o/r"},"created_at":"2026-10-18T11:10:00Z",
		"payload":{"action":"st\u001b[2Jarted"}}]`)
	e, _ := reviewEnv(t, answers(t, oldPass), routes)
	r := runWith(e, "log", "--verify")
	if r.code != 0 || strings.ContainsRune(r.stdout, '\x1b') || strings.ContainsRune(r.stdout, '\a') ||
		!strings.Contains(r.stdout, "Evil<U+001B>]52;c;x<U+0007>Event/st<U+001B>[2Jarted 1.") {
		t.Fatalf("exit %d, stderr %q, stdout %q", r.code, r.stderr, r.stdout)
	}
}
