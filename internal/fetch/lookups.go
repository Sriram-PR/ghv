package fetch

import (
	"context"
	"fmt"
	"net/url"
	"path"
	"strconv"
	"time"
)

// Repo names and logins come from validated drafts and the vault; branch
// names are escaped, since they may hold "/".

type Account struct {
	Login     string    `json:"login"`
	Type      string    `json:"type"`
	CreatedAt time.Time `json:"created_at"`
}

type Repo struct {
	FullName  string    `json:"full_name"`
	Owner     Account   `json:"owner"`
	CreatedAt time.Time `json:"created_at"`
	Fork      bool      `json:"fork"`
	Parent    *Repo     `json:"parent"`
}

type Issue struct {
	Number      int     `json:"number"`
	Title       string  `json:"title"`
	State       string  `json:"state"`
	User        Account `json:"user"`
	PullRequest *struct {
		URL string `json:"url"`
	} `json:"pull_request"`
}

func (i Issue) IsPullRequest() bool { return i.PullRequest != nil }

type PullRequest struct {
	Number int     `json:"number"`
	Title  string  `json:"title"`
	Body   string  `json:"body"`
	State  string  `json:"state"`
	User   Account `json:"user"`
}

type ReviewComment struct {
	ID             int64   `json:"id"`
	Body           string  `json:"body"`
	User           Account `json:"user"`
	Path           string  `json:"path"`
	Line           *int    `json:"line"`
	OriginalLine   *int    `json:"original_line"`
	InReplyToID    int64   `json:"in_reply_to_id"`
	PullRequestURL string  `json:"pull_request_url"`
}

// PullNumber is the last segment of PullRequestURL, or 0.
func (rc ReviewComment) PullNumber() int {
	n, err := strconv.Atoi(path.Base(rc.PullRequestURL))
	if err != nil {
		return 0
	}
	return n
}

type Commit struct {
	SHA    string `json:"sha"`
	Commit struct {
		Author struct {
			Name string `json:"name"`
		} `json:"author"`
		Message string `json:"message"`
	} `json:"commit"`
	Author *Account `json:"author"`
}

type File struct {
	Filename         string `json:"filename"`
	PreviousFilename string `json:"previous_filename"`
	Status           string `json:"status"`
	Additions        int    `json:"additions"`
	Deletions        int    `json:"deletions"`
}

// Comparison is GitHub's compare of base...head. GitHub lists at most 250
// commits and 300 files.
type Comparison struct {
	Status       string   `json:"status"`
	AheadBy      int      `json:"ahead_by"`
	BehindBy     int      `json:"behind_by"`
	TotalCommits int      `json:"total_commits"`
	Commits      []Commit `json:"commits"`
	Files        []File   `json:"files"`
}

func (c *Client) Repo(ctx context.Context, fullName string) (Repo, error) {
	var r Repo
	err := c.GetJSON(ctx, "/repos/"+fullName, &r)
	return r, err
}

func (c *Client) User(ctx context.Context, login string) (Account, error) {
	var a Account
	err := c.GetJSON(ctx, "/users/"+login, &a)
	return a, err
}

func (c *Client) Issue(ctx context.Context, repo string, n int) (Issue, error) {
	var i Issue
	err := c.GetJSON(ctx, fmt.Sprintf("/repos/%s/issues/%d", repo, n), &i)
	return i, err
}

func (c *Client) PullRequest(ctx context.Context, repo string, n int) (PullRequest, error) {
	var p PullRequest
	err := c.GetJSON(ctx, fmt.Sprintf("/repos/%s/pulls/%d", repo, n), &p)
	return p, err
}

func (c *Client) ReviewComment(ctx context.Context, repo string, id int64) (ReviewComment, error) {
	var rc ReviewComment
	err := c.GetJSON(ctx, fmt.Sprintf("/repos/%s/pulls/comments/%d", repo, id), &rc)
	return rc, err
}

// BranchHead is the commit a branch points at now.
func (c *Client) BranchHead(ctx context.Context, repo, branch string) (string, error) {
	var b struct {
		Commit struct {
			SHA string `json:"sha"`
		} `json:"commit"`
	}
	err := c.GetJSON(ctx, "/repos/"+repo+"/branches/"+url.PathEscape(branch), &b)
	return b.Commit.SHA, err
}

func comparePath(repo, base, head string) string {
	return "/repos/" + repo + "/compare/" + url.PathEscape(base) + "..." + url.PathEscape(head)
}

func (c *Client) Compare(ctx context.Context, repo, base, head string) (Comparison, error) {
	var cmp Comparison
	err := c.GetJSON(ctx, comparePath(repo, base, head), &cmp)
	return cmp, err
}

// CompareDiff is the same compare as a unified diff.
func (c *Client) CompareDiff(ctx context.Context, repo, base, head string) (string, error) {
	b, err := c.Get(ctx, comparePath(repo, base, head), "application/vnd.github.diff")
	return string(b), err
}

// Involved reports whether login authored, was assigned, was mentioned
// in, or commented on any issue or pull request in repo.
func (c *Client) Involved(ctx context.Context, repo, login string) (bool, error) {
	var s struct {
		TotalCount int `json:"total_count"`
	}
	q := url.QueryEscape("repo:" + repo + " involves:" + login)
	err := c.GetJSON(ctx, "/search/issues?q="+q+"&per_page=1", &s)
	return s.TotalCount > 0, err
}

// OwnedRepos lists up to 100 of login's own repositories.
func (c *Client) OwnedRepos(ctx context.Context, login string) ([]string, error) {
	var rs []Repo
	if err := c.GetJSON(ctx, "/users/"+login+"/repos?type=owner&per_page=100", &rs); err != nil {
		return nil, err
	}
	out := make([]string, 0, len(rs))
	for _, r := range rs {
		out = append(out, r.FullName)
	}
	return out, nil
}

// RecentRepos lists, once each, the repositories in login's last 100
// public events.
func (c *Client) RecentRepos(ctx context.Context, login string) ([]string, error) {
	var evs []struct {
		Repo struct {
			Name string `json:"name"`
		} `json:"repo"`
	}
	if err := c.GetJSON(ctx, "/users/"+login+"/events/public?per_page=100", &evs); err != nil {
		return nil, err
	}
	var out []string
	seen := map[string]bool{}
	for _, e := range evs {
		if !seen[e.Repo.Name] {
			seen[e.Repo.Name] = true
			out = append(out, e.Repo.Name)
		}
	}
	return out, nil
}
