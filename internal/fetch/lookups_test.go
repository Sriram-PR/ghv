package fetch

import (
	"context"
	"errors"
	"slices"
	"testing"
	"time"
)

func TestLookups(t *testing.T) {
	f, c := newFake(map[string]reply{
		"/repos/o/r": {status: 200, body: `{"full_name":"o/r","created_at":"2019-01-02T03:04:05Z","fork":true,
			"owner":{"login":"o","type":"Organization"},"parent":{"full_name":"up/r","owner":{"login":"up"}}}`},
		"/users/o": {status: 200, body: `{"login":"o","type":"Organization","created_at":"2015-03-04T00:00:00Z"}`},
		"/repos/o/r/issues/7": {status: 200, body: `{"number":7,"title":"Crash","state":"open","user":{"login":"u"},
			"pull_request":{"url":"x"}}`},
		"/repos/o/r/pulls/8":           {status: 200, body: `{"number":8,"title":"Fix","body":null,"state":"closed","user":{"login":"u"}}`},
		"/repos/o/r/pulls/comments/9":  {status: 200, body: `{"id":9,"body":"why?","user":{"login":"rev"},"path":"a.go","line":12,"original_line":10,"in_reply_to_id":0,"pull_request_url":"https://api.github.com/repos/o/r/pulls/8"}`},
		"/repos/me/r/branches/fix%2Fx": {status: 200, body: `{"name":"fix/x","commit":{"sha":"0123456789abcdef0123456789abcdef01234567"}}`},
		"/repos/o/r/compare/release%2F1...0123456789abcdef0123456789abcdef01234567": {status: 200, body: `{"status":"ahead","ahead_by":1,"behind_by":0,"total_commits":1,
			"commits":[{"sha":"0123456789abcdef0123456789abcdef01234567","commit":{"author":{"name":"Me"},"message":"fix: x\n\nbody"},"author":{"login":"me"}}],
			"files":[{"filename":".github/workflows/ci.yml","status":"modified","additions":2,"deletions":1}]}`},
		"/search/issues?q=repo%3Ao%2Fr+involves%3Ame&per_page=1": {status: 200, body: `{"total_count":3,"items":[]}`},
		"/search/issues?q=repo%3Ao%2Fx+involves%3Ame&per_page=1": {status: 200, body: `{"total_count":0,"items":[]}`},
		"/users/me/repos?type=owner&per_page=100":                {status: 200, body: `[{"full_name":"me/a"},{"full_name":"me/b"}]`},
		"/users/me/events/public?per_page=100":                   {status: 200, body: `[{"repo":{"name":"o/r"}},{"repo":{"name":"me/a"}},{"repo":{"name":"o/r"}}]`},
	})
	ctx := context.Background()

	repo, err := c.Repo(ctx, "o/r")
	if err != nil || repo.FullName != "o/r" || repo.Owner.Login != "o" || repo.Owner.Type != "Organization" ||
		!repo.CreatedAt.Equal(time.Date(2019, 1, 2, 3, 4, 5, 0, time.UTC)) || !repo.Fork || repo.Parent == nil || repo.Parent.FullName != "up/r" {
		t.Fatalf("Repo: %+v, %v", repo, err)
	}
	user, err := c.User(ctx, "o")
	if err != nil || user.Login != "o" || !user.CreatedAt.Equal(time.Date(2015, 3, 4, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("User: %+v, %v", user, err)
	}
	iss, err := c.Issue(ctx, "o/r", 7)
	if err != nil || iss.Title != "Crash" || iss.State != "open" || iss.User.Login != "u" || !iss.IsPullRequest() {
		t.Fatalf("Issue: %+v, %v", iss, err)
	}
	pr, err := c.PullRequest(ctx, "o/r", 8)
	if err != nil || pr.Title != "Fix" || pr.Body != "" || pr.State != "closed" || pr.User.Login != "u" {
		t.Fatalf("PullRequest: %+v, %v", pr, err)
	}
	rc, err := c.ReviewComment(ctx, "o/r", 9)
	if err != nil || rc.Body != "why?" || rc.User.Login != "rev" || rc.Path != "a.go" || rc.Line == nil || *rc.Line != 12 ||
		rc.InReplyToID != 0 || rc.PullNumber() != 8 {
		t.Fatalf("ReviewComment: %+v, %v", rc, err)
	}
	sha, err := c.BranchHead(ctx, "me/r", "fix/x")
	if err != nil || sha != "0123456789abcdef0123456789abcdef01234567" {
		t.Fatalf("BranchHead: %q, %v", sha, err)
	}
	cmp, err := c.Compare(ctx, "o/r", "release/1", "0123456789abcdef0123456789abcdef01234567")
	if err != nil || cmp.TotalCommits != 1 || len(cmp.Commits) != 1 || cmp.Commits[0].Author.Login != "me" ||
		cmp.Commits[0].Commit.Message != "fix: x\n\nbody" || cmp.Commits[0].Commit.Author.Name != "Me" ||
		len(cmp.Files) != 1 || cmp.Files[0].Filename != ".github/workflows/ci.yml" || cmp.Files[0].Additions != 2 {
		t.Fatalf("Compare: %+v, %v", cmp, err)
	}
	if ok, err := c.Involved(ctx, "o/r", "me"); err != nil || !ok {
		t.Fatalf("Involved o/r: %v, %v", ok, err)
	}
	if ok, err := c.Involved(ctx, "o/x", "me"); err != nil || ok {
		t.Fatalf("Involved o/x: %v, %v", ok, err)
	}
	owned, err := c.OwnedRepos(ctx, "me")
	if err != nil || !slices.Equal(owned, []string{"me/a", "me/b"}) {
		t.Fatalf("OwnedRepos: %v, %v", owned, err)
	}
	recent, err := c.RecentRepos(ctx, "me")
	if err != nil || !slices.Equal(recent, []string{"o/r", "me/a"}) {
		t.Fatalf("RecentRepos: %v, %v", recent, err)
	}
	if _, err := c.Repo(ctx, "o/missing"); !errors.Is(err, ErrNotFound) {
		t.Fatalf("missing repo: %v", err)
	}
	for _, r := range f.reqs {
		if r.Header.Get("Authorization") != "" {
			t.Fatalf("%s sent Authorization", r.URL)
		}
	}
}

func TestCompareDiff(t *testing.T) {
	f, c := newFake(map[string]reply{
		"/repos/o/r/compare/main...0123456789abcdef0123456789abcdef01234567": {status: 200, body: "diff --git a/x b/x\n"},
	})
	d, err := c.CompareDiff(context.Background(), "o/r", "main", "0123456789abcdef0123456789abcdef01234567")
	if err != nil || d != "diff --git a/x b/x\n" || f.reqs[0].Header.Get("Accept") != "application/vnd.github.diff" {
		t.Fatalf("%q, %v, Accept %q", d, err, f.reqs[0].Header.Get("Accept"))
	}
}
