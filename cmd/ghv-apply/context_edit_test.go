package main

import (
	"slices"
	"strings"
	"testing"

	"github.com/Sriram-PR/ghv/internal/draft"
)

func reply(number int, id int64) draft.Draft {
	return draft.Draft{Repo: "o/r", Fields: draft.ReviewReplyFields{Number: number, CommentID: id, Body: "b"}}
}

func TestContextReviewReply(t *testing.T) {
	routes := baseRoutes()
	routes["/repos/o/r/pulls/comments/9"] = ok(`{"id":9,"body":"why\nthis?","user":{"login":"rev"},"path":"a.go","line":12,
		"pull_request_url":"https://api.github.com/repos/o/r/pulls/8"}`)
	routes["/repos/o/r/pulls/comments/10"] = ok(`{"id":10,"body":"old","user":{"login":"rev"},"path":"b.go","line":null,"original_line":3,
		"in_reply_to_id":9,"pull_request_url":"https://api.github.com/repos/o/r/pulls/8"}`)

	rc := contextOf(t, routes, reply(8, 9))
	has(t, "lines", rc.Lines, "Replying in pull request #8 to rev's comment on a.go line 12:\n    why\n    this?")
	if len(rc.Blocks) != 0 {
		t.Fatalf("blocks %q", rc.Blocks)
	}

	rc = contextOf(t, routes, reply(7, 9))
	has(t, "blocks", rc.Blocks, "review comment 9 belongs to pull request #8, not #7")

	rc = contextOf(t, routes, reply(8, 10))
	has(t, "blocks", rc.Blocks, "review comment 10 is itself a reply; GitHub takes replies to a thread's first comment, 9")
	has(t, "lines", rc.Lines, "on b.go line 3 (outdated):")

	rc = contextOf(t, routes, reply(8, 11))
	has(t, "blocks", rc.Blocks, "o/r has no review comment 11")
}

func TestContextPREdit(t *testing.T) {
	routes := baseRoutes()
	routes["/repos/o/r/pulls/8"] = ok(`{"number":8,"title":"Fix","body":"a\r\nb\r\nc","state":"open","user":{"login":"u"}}`)
	title, body := "Fix it", "a\nB\nc"

	rc := contextOf(t, routes, draft.Draft{Repo: "o/r", Fields: draft.PREditFields{Number: 8, Title: &title, Body: &body}})
	has(t, "lines", rc.Lines, "Editing pull request #8 (open), opened by u\nTitle:\n  - Fix\n  + Fix it\nBody:\n    a\n  - b\n  + B\n    c")
	has(t, "flags", rc.Flags, "pull request #8 was opened by u, not you")

	same := "Fix"
	rc = contextOf(t, routes, draft.Draft{Repo: "o/r", Fields: draft.PREditFields{Number: 8, Title: &same}})
	has(t, "lines", rc.Lines, "Title: unchanged")
	if strings.Contains(strings.Join(rc.Lines, "\n"), "Body") {
		t.Fatalf("a draft without a body shows one:\n%s", strings.Join(rc.Lines, "\n"))
	}

	rc = contextOf(t, routes, draft.Draft{Repo: "o/r", Fields: draft.PREditFields{Number: 9, Title: &title}})
	has(t, "blocks", rc.Blocks, "o/r has no pull request #9")
}

func TestLineDiff(t *testing.T) {
	for _, tt := range []struct {
		old, new string
		want     []string
	}{
		{"", "", nil},
		{"", "a", []string{"+ a"}},
		{"a", "", []string{"- a"}},
		{"a\nb\nc", "a\nc", []string{"  a", "- b", "  c"}},
		{"a\nc", "a\nb\nc", []string{"  a", "+ b", "  c"}},
		{"x\ny", "y\nx", []string{"- x", "  y", "+ x"}},
	} {
		if got := lineDiff(tt.old, tt.new); !slices.Equal(got, tt.want) {
			t.Errorf("lineDiff(%q, %q) = %q, want %q", tt.old, tt.new, got, tt.want)
		}
	}
	big := strings.Repeat("x\n", 3000)
	got := lineDiff(big, big+"y")
	if len(got) != 6002 || got[0] != "- x" || got[6001] != "+ y" {
		t.Fatalf("over the cap: %d lines, first %q, last %q", len(got), got[0], got[len(got)-1])
	}
}
