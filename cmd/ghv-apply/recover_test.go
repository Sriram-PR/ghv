package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Sriram-PR/ghv/internal/draft"
)

// sentAt is when the test drafts were sent; lookups start 5 minutes
// earlier, for clock skew.
var sentAt = time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)

const since = "per_page=100&since=2026-10-18T11%3A55%3A00Z"

func TestFindSent(t *testing.T) {
	sha := strings.Repeat("a", 40)
	title, body, old := "New title", "a\nb", "Old"
	routes := baseRoutes()
	routes["/repos/o/r/issues/7/comments?"+since] = ok(`[
		{"html_url":"https://github.com/o/r/issues/7#issuecomment-1","body":"other","user":{"login":"Sriram-PR"},"created_at":"2026-10-18T12:00:01Z"},
		{"html_url":"https://github.com/o/r/issues/7#issuecomment-2","body":"hi\r\nthere","user":{"login":"someone"},"created_at":"2026-10-18T12:00:02Z"},
		{"html_url":"https://github.com/o/r/issues/7#issuecomment-3","body":"hi\r\nthere\n","user":{"login":"sriram-pr"},"created_at":"2026-10-18T12:00:03Z"}]`)
	routes["/repos/o/r/pulls/8/comments?"+since] = ok(`[
		{"html_url":"https://github.com/o/r/pull/8#discussion_r1","body":"yes","user":{"login":"Sriram-PR"},"created_at":"2026-10-18T12:00:01Z","in_reply_to_id":5},
		{"html_url":"https://github.com/o/r/pull/8#discussion_r2","body":"yes","user":{"login":"Sriram-PR"},"created_at":"2026-10-18T12:00:02Z","in_reply_to_id":9}]`)
	routes["/repos/o/r/issues?creator=Sriram-PR&"+since+"&state=all"] = ok(`[
		{"html_url":"https://github.com/o/r/pull/3","title":"Bug","body":"b","user":{"login":"Sriram-PR"},"created_at":"2026-10-18T12:00:01Z","pull_request":{"url":"x"}},
		{"html_url":"https://github.com/o/r/issues/2","title":"Bug","body":"b","user":{"login":"Sriram-PR"},"created_at":"2026-10-18T11:50:00Z"},
		{"html_url":"https://github.com/o/r/issues/4","title":"Bug","body":"b","user":{"login":"Sriram-PR"},"created_at":"2026-10-18T12:00:05Z"}]`)
	routes["/repos/o/r/pulls?head=Sriram-PR%3Afix%2Fx&per_page=100&state=all"] = ok(`[
		{"html_url":"https://github.com/o/r/pull/4","user":{"login":"Sriram-PR"},"created_at":"2026-10-18T12:00:06Z","head":{"sha":"` + strings.Repeat("b", 40) + `"}},
		{"html_url":"https://github.com/o/r/pull/5","user":{"login":"Sriram-PR"},"created_at":"2026-10-18T12:00:06Z","head":{"sha":"` + sha + `"}}]`)
	routes["/repos/o/r/pulls/12"] = ok(`{"number":12,"title":"New title","body":"a\r\nb","state":"open","user":{"login":"u"}}`)
	routes["/repos/o/r/forks?per_page=100&sort=newest"] = ok(`[
		{"html_url":"https://github.com/someone/r","full_name":"someone/r","owner":{"login":"someone"}},
		{"html_url":"https://github.com/Sriram-PR/r-1","full_name":"Sriram-PR/r-1","owner":{"login":"Sriram-PR"}}]`)
	rv, _ := newReviewer(routes)

	for _, c := range []struct {
		name     string
		fields   draft.Fields
		edited   bool
		url      string
		fullName string
		checked  string
	}{
		{"comment", draft.CommentFields{Number: 7, Body: "hi\nthere"}, false, "https://github.com/o/r/issues/7#issuecomment-3", "", ""},
		{"comment, edited", draft.CommentFields{Number: 7, Body: "the agent's text"}, true, "https://github.com/o/r/issues/7#issuecomment-1", "", ""},
		{"comment, not there", draft.CommentFields{Number: 7, Body: "never sent"}, false, "", "",
			"your comments on o/r#7 since 2026-10-18 11:55 UTC, matching the draft's text"},
		{"review.reply", draft.ReviewReplyFields{Number: 8, CommentID: 9, Body: "yes"}, false, "https://github.com/o/r/pull/8#discussion_r2", "", ""},
		{"review.reply, not there", draft.ReviewReplyFields{Number: 8, CommentID: 6, Body: "yes"}, false, "", "",
			"your replies to review comment 6 on o/r#8 since 2026-10-18 11:55 UTC, matching the draft's text"},
		{"issue.create", draft.IssueCreateFields{Title: "Bug", Body: "b"}, false, "https://github.com/o/r/issues/4", "", ""},
		{"issue.create, edited", draft.IssueCreateFields{Title: "Agent's", Body: "x"}, true, "https://github.com/o/r/issues/4", "", ""},
		{"pr.create", draft.PRCreateFields{Base: "main", Head: "Sriram-PR:fix/x", HeadSHA: sha, Title: "T"}, false, "https://github.com/o/r/pull/5", "", ""},
		{"pr.create, not there", draft.PRCreateFields{Base: "main", Head: "Sriram-PR:fix/x", HeadSHA: strings.Repeat("c", 40), Title: "T"}, false, "", "",
			"your pull requests from Sriram-PR:fix/x at " + strings.Repeat("c", 12)},
		{"pr.edit", draft.PREditFields{Number: 12, Title: &title, Body: &body}, false, "https://github.com/o/r/pull/12", "", ""},
		{"pr.edit, not there", draft.PREditFields{Number: 12, Title: &old}, false, "", "",
			"whether o/r#12 now has the draft's title and body"},
		{"pr.edit, edited", draft.PREditFields{Number: 12, Title: &title}, true, "", "",
			"nothing: an edited pr.edit cannot be matched by its text; see https://github.com/o/r/pull/12"},
		{"fork", draft.ForkFields{}, false, "https://github.com/Sriram-PR/r-1", "Sriram-PR/r-1", ""},
	} {
		t.Run(c.name, func(t *testing.T) {
			d := draft.Draft{Repo: "o/r", CreatedAt: at(9), Fields: c.fields}
			f, err := rv.findSent(context.Background(), d, sentAt, c.edited)
			if err != nil || f.URL != c.url || f.FullName != c.fullName || f.Checked != c.checked {
				t.Fatalf("got %+v, %v", f, err)
			}
		})
	}
}

func TestFindSentLookupFails(t *testing.T) {
	rv, _ := newReviewer(baseRoutes())
	d := draft.Draft{Repo: "o/r", CreatedAt: at(9), Fields: draft.CommentFields{Number: 7, Body: "hi"}}
	if _, err := rv.findSent(context.Background(), d, sentAt, false); err == nil || !strings.Contains(err.Error(), "GET /repos/o/r/issues/7/comments?"+since+": not found") {
		t.Fatalf("err %v", err)
	}
}
