package fetch

import (
	"context"
	"fmt"
	"net/url"
	"time"
)

// Made is something GitHub shows a user made: a comment, an issue, a pull
// request, or a fork. Fields a kind does not have stay empty.
type Made struct {
	HTMLURL     string    `json:"html_url"`
	Title       string    `json:"title"`
	Body        string    `json:"body"`
	User        Account   `json:"user"`
	CreatedAt   time.Time `json:"created_at"`
	InReplyToID int64     `json:"in_reply_to_id"`
	PullRequest *struct {
		URL string `json:"url"`
	} `json:"pull_request"`
	Head struct {
		SHA string `json:"sha"`
	} `json:"head"`
	FullName string  `json:"full_name"`
	Owner    Account `json:"owner"`
}

// IsPullRequest tells a pull request from an issue in GitHub's issue list.
func (m Made) IsPullRequest() bool { return m.PullRequest != nil }

// Each lookup reads one page of 100: the time bound or the head keeps the
// list short.
func (c *Client) list(ctx context.Context, path string, q url.Values) ([]Made, error) {
	q.Set("per_page", "100")
	var ms []Made
	err := c.GetJSON(ctx, path+"?"+q.Encode(), &ms)
	return ms, err
}

func stamp(t time.Time) string { return t.UTC().Format(time.RFC3339) }

// IssueComments lists the comments on issue or pull request n updated
// since after.
func (c *Client) IssueComments(ctx context.Context, repo string, n int, after time.Time) ([]Made, error) {
	return c.list(ctx, fmt.Sprintf("/repos/%s/issues/%d/comments", repo, n), url.Values{"since": {stamp(after)}})
}

// ReviewComments lists pull request n's review comments updated since
// after.
func (c *Client) ReviewComments(ctx context.Context, repo string, n int, after time.Time) ([]Made, error) {
	return c.list(ctx, fmt.Sprintf("/repos/%s/pulls/%d/comments", repo, n), url.Values{"since": {stamp(after)}})
}

// IssuesBy lists the issues and pull requests creator opened, open or
// closed, updated since after.
func (c *Client) IssuesBy(ctx context.Context, repo, creator string, after time.Time) ([]Made, error) {
	return c.list(ctx, "/repos/"+repo+"/issues", url.Values{"creator": {creator}, "since": {stamp(after)}, "state": {"all"}})
}

// PullsByHead lists the pull requests, open or closed, from head
// (OWNER:BRANCH).
func (c *Client) PullsByHead(ctx context.Context, repo, head string) ([]Made, error) {
	return c.list(ctx, "/repos/"+repo+"/pulls", url.Values{"head": {head}, "state": {"all"}})
}

// Forks lists repo's newest 100 forks.
func (c *Client) Forks(ctx context.Context, repo string) ([]Made, error) {
	return c.list(ctx, "/repos/"+repo+"/forks", url.Values{"sort": {"newest"}})
}
