package fetch

import (
	"context"
	"testing"
	"time"
)

func TestRecoveryLookups(t *testing.T) {
	f, c := newFake(map[string]reply{
		"/repos/o/r/issues/7/comments?per_page=100&since=2026-10-18T11%3A55%3A00Z": {status: 200, body: `[
			{"html_url":"https://github.com/o/r/issues/7#issuecomment-1","body":"hi\r\nthere","user":{"login":"Sriram-PR"},"created_at":"2026-10-18T12:00:03Z"}]`},
		"/repos/o/r/pulls/8/comments?per_page=100&since=2026-10-18T11%3A55%3A00Z": {status: 200, body: `[
			{"html_url":"https://github.com/o/r/pull/8#discussion_r2","body":"yes","user":{"login":"Sriram-PR"},"created_at":"2026-10-18T12:00:04Z","in_reply_to_id":9}]`},
		"/repos/o/r/issues?creator=Sriram-PR&per_page=100&since=2026-10-18T11%3A55%3A00Z&state=all": {status: 200, body: `[
			{"html_url":"https://github.com/o/r/pull/3","title":"PR","body":null,"user":{"login":"Sriram-PR"},"created_at":"2026-10-18T12:00:00Z","pull_request":{"url":"x"}},
			{"html_url":"https://github.com/o/r/issues/4","title":"Bug","body":"b","user":{"login":"Sriram-PR"},"created_at":"2026-10-18T12:00:05Z"}]`},
		"/repos/o/r/pulls?head=Sriram-PR%3Afix%2Fx&per_page=100&state=all": {status: 200, body: `[
			{"html_url":"https://github.com/o/r/pull/5","title":"Fix","user":{"login":"Sriram-PR"},"created_at":"2026-10-18T12:00:06Z","head":{"sha":"0123456789abcdef0123456789abcdef01234567"}}]`},
		"/repos/o/r/forks?per_page=100&sort=newest": {status: 200, body: `[
			{"html_url":"https://github.com/someone/r","full_name":"someone/r","owner":{"login":"someone"},"created_at":"2026-10-18T12:00:07Z"},
			{"html_url":"https://github.com/Sriram-PR/r-1","full_name":"Sriram-PR/r-1","owner":{"login":"Sriram-PR"},"created_at":"2026-10-18T12:00:08Z"}]`},
	})
	ctx := context.Background()
	after := time.Date(2026, 10, 18, 17, 25, 0, 0, time.FixedZone("IST", 5*3600+1800))

	cs, err := c.IssueComments(ctx, "o/r", 7, after)
	if err != nil || len(cs) != 1 || cs[0].Body != "hi\r\nthere" || cs[0].User.Login != "Sriram-PR" ||
		cs[0].HTMLURL != "https://github.com/o/r/issues/7#issuecomment-1" || !cs[0].CreatedAt.Equal(time.Date(2026, 10, 18, 12, 0, 3, 0, time.UTC)) {
		t.Fatalf("IssueComments: %+v, %v", cs, err)
	}
	rs, err := c.ReviewComments(ctx, "o/r", 8, after)
	if err != nil || len(rs) != 1 || rs[0].InReplyToID != 9 || rs[0].Body != "yes" {
		t.Fatalf("ReviewComments: %+v, %v", rs, err)
	}
	is, err := c.IssuesBy(ctx, "o/r", "Sriram-PR", after)
	if err != nil || len(is) != 2 || !is[0].IsPullRequest() || is[1].IsPullRequest() || is[1].Title != "Bug" || is[0].Body != "" {
		t.Fatalf("IssuesBy: %+v, %v", is, err)
	}
	ps, err := c.PullsByHead(ctx, "o/r", "Sriram-PR:fix/x")
	if err != nil || len(ps) != 1 || ps[0].Head.SHA != "0123456789abcdef0123456789abcdef01234567" || ps[0].HTMLURL != "https://github.com/o/r/pull/5" {
		t.Fatalf("PullsByHead: %+v, %v", ps, err)
	}
	fs, err := c.Forks(ctx, "o/r")
	if err != nil || len(fs) != 2 || fs[1].FullName != "Sriram-PR/r-1" || fs[1].Owner.Login != "Sriram-PR" {
		t.Fatalf("Forks: %+v, %v", fs, err)
	}
	if len(f.reqs) != 5 {
		t.Fatalf("%d requests", len(f.reqs))
	}
	for _, r := range f.reqs {
		if r.Method != "GET" || r.Header.Get("Authorization") != "" {
			t.Fatalf("%s %s sent Authorization %q", r.Method, r.URL, r.Header.Get("Authorization"))
		}
	}
}
