package main

import (
	"crypto/sha256"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Sriram-PR/ghv/internal/draft"
	"github.com/Sriram-PR/ghv/internal/sanitize"
	"github.com/Sriram-PR/ghv/internal/write"
)

func loaded(d draft.Draft) draft.Loaded {
	d.ID = "d-0123456789ab"
	d.CreatedAt = time.Date(2026, 10, 16, 5, 5, 12, 0, time.UTC)
	return draft.Loaded{Draft: d, SHA256: sha256.Sum256([]byte("raw"))}
}

func TestRenderComment(t *testing.T) {
	// U+202E, a right-to-left override, as bytes.
	body := "Thanks @someone!\n\tSee https://example.com/x\nok \xe2\x80\xae"
	l := loaded(draft.Draft{Repo: "o/r", Fields: draft.CommentFields{Number: 7, Body: body}})
	got := renderDraft(l, sanitize.CheckDraft(l.Draft), nil)
	want := []string{
		"Draft d-0123456789ab: comment to o/r",
		fmt.Sprintf("Created 2026-10-16 05:05 UTC; file SHA-256 %x", sha256.Sum256([]byte("raw"))),
		"",
		"number: 7",
		"body (3 lines, 48 characters):",
		"    Thanks @someone!",
		"    \tSee https://example.com/x",
		"    ok <U+202E>",
		"",
		"Findings (2):",
		"  body:1:8: mention @someone",
		"  body:3:4: bidi <U+202E>",
		"Links (1):",
		"  https://example.com/x",
		"-- end of d-0123456789ab: body 3 lines, 48 characters --",
	}
	if !slices.Equal(got, want) {
		t.Fatalf("got:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}

func TestRenderEveryKind(t *testing.T) {
	title := "New title"
	for _, tt := range []struct {
		d    draft.Draft
		want []string
	}{
		{draft.Draft{Repo: "o/r", Fields: draft.ForkFields{}}, []string{
			"Draft d-0123456789ab: fork to o/r", "-- end of d-0123456789ab --"}},
		{draft.Draft{Repo: "o/r", Fields: draft.PRCreateFields{Base: "main", Head: "me:fix/x", HeadSHA: headSHA, Title: "Fix x", Body: "", Draft: true}}, []string{
			"base: main", "head: me:fix/x", "head_sha: " + headSHA, "draft: true", "maintainer_can_modify: false",
			"title (5 characters): Fix x", "body: (empty)", "-- end of d-0123456789ab: title 5 characters; body empty --"}},
		{draft.Draft{Repo: "o/r", Fields: draft.PREditFields{Number: 8, Title: &title}}, []string{
			"number: 8", "title (9 characters): New title", "body: (not changed)", "-- end of d-0123456789ab: title 9 characters --"}},
		{draft.Draft{Repo: "o/r", Fields: draft.ReviewReplyFields{Number: 8, CommentID: 9, Body: "yes"}}, []string{
			"number: 8", "comment_id: 9", "body (1 line, 3 characters):", "    yes"}},
		{draft.Draft{Repo: "o/r", Fields: draft.IssueCreateFields{Title: "Bug", Body: "a\nb"}}, []string{
			"title (3 characters): Bug", "body (2 lines, 3 characters):", "    a", "    b", "-- end of d-0123456789ab: title 3 characters; body 2 lines, 3 characters --"}},
	} {
		l := loaded(tt.d)
		got := strings.Join(renderDraft(l, sanitize.CheckDraft(l.Draft), nil), "\n")
		for _, w := range tt.want {
			if !strings.Contains(got, w) {
				t.Errorf("%s lacks %q:\n%s", tt.d.Kind(), w, got)
			}
		}
	}
}

func numbered(n int) []string {
	var out []string
	for i := 1; i <= n; i++ {
		out = append(out, fmt.Sprintf("line %d", i))
	}
	return out
}

func TestPage(t *testing.T) {
	ft := answers(t, "", "q")
	ft.height = 10
	if err := page(ft, numbered(25)); err != nil {
		t.Fatal(err)
	}
	want := strings.Join(numbered(9), "\n") + "\n-- line 9 of 25: Enter for more, q to skip to the end -- \n" +
		strings.Join(numbered(18)[9:], "\n") + "\n-- line 18 of 25: Enter for more, q to skip to the end -- \n" +
		"(6 lines skipped)\nline 25\n"
	if ft.shown() != want {
		t.Fatalf("shown:\n%s\nwant:\n%s", ft.shown(), want)
	}
}

func TestPageFitsOrNoHeight(t *testing.T) {
	for _, h := range []int{0, 10} {
		ft := answers(t)
		ft.height = h
		if err := page(ft, numbered(10)); err != nil || ft.shown() != strings.Join(numbered(10), "\n")+"\n" {
			t.Fatalf("height %d: %v, shown %q", h, err, ft.shown())
		}
	}
	ft := answers(t, "", "")
	ft.height = 10
	if err := page(ft, numbered(19)); err != nil || !strings.HasSuffix(ft.shown(), "line 18\n-- line 18 of 19: Enter for more, q to skip to the end -- \nline 19\n") {
		t.Fatalf("%v, shown %q", err, ft.shown())
	}
}

func TestRenderRequest(t *testing.T) {
	body := "a\nb"
	for _, tt := range []struct {
		d    draft.Draft
		want []string
	}{
		{draft.Draft{Repo: "o/r", Fields: draft.PRCreateFields{Base: "main", Head: "me:fix/x", HeadSHA: headSHA, Title: "Fix x", Body: "", Draft: true}}, []string{
			"Request: POST https://api.github.com/repos/moby/moby/pulls",
			"  title: (shown above, 5 characters)",
			`  head: "me:fix/x"`,
			`  base: "main"`,
			"  body: (empty)",
			"  draft: true",
			"  maintainer_can_modify: false",
		}},
		{draft.Draft{Repo: "o/r", Fields: draft.ForkFields{}}, []string{
			"Request: POST https://api.github.com/repos/moby/moby/forks",
			"  (no fields)",
		}},
		{draft.Draft{Repo: "o/r", Fields: draft.PREditFields{Number: 8, Body: &body}}, []string{
			"Request: PATCH https://api.github.com/repos/moby/moby/pulls/8",
			"  body: (shown above, 2 lines, 3 characters)",
		}},
	} {
		l := loaded(tt.d)
		req, err := write.Build(l.Draft, "moby/moby")
		if err != nil {
			t.Fatal(err)
		}
		got := renderDraft(l, sanitize.CheckDraft(l.Draft), &req)
		want := append([]string{""}, tt.want...)
		want = append(want, "  request SHA-256 "+req.SHA256())
		if end := got[len(got)-1]; !strings.HasPrefix(end, "-- end of ") {
			t.Fatalf("the end marker is not last: %q", end)
		}
		if tail := got[len(got)-1-len(want) : len(got)-1]; !slices.Equal(tail, want) {
			t.Errorf("%s request:\n%s\nwant:\n%s", tt.d.Kind(), strings.Join(tail, "\n"), strings.Join(want, "\n"))
		}
	}
}
