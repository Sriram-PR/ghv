package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"strconv"

	"github.com/Sriram-PR/ghv/internal/draft"
	"github.com/Sriram-PR/ghv/internal/read"
)

func init() {
	register(&command{
		path:  "review reply",
		args:  []string{"N", "COMMENT_ID"},
		flags: []flag{draftRepoFlag, bodyFileFlag},
		help:  "Draft a reply to inline review comment COMMENT_ID on pull request N",
		run:   runReviewReply,
	})
}

func runReviewReply(ctx context.Context, a *app) error {
	n, err := a.number(0)
	if err != nil {
		return err
	}
	id, err := a.idArg(1, "comment ID")
	if err != nil {
		return err
	}
	if err := a.requireFlags("body-file"); err != nil {
		return err
	}
	repo, err := a.draftRepo()
	if err != nil {
		return err
	}
	body, err := a.commentBody()
	if err != nil {
		return err
	}
	c, err := a.read()
	if err != nil {
		return err
	}
	root, err := threadRoot(ctx, c, repo, n, id)
	if err != nil {
		return err
	}
	if root != id {
		fmt.Fprintf(a.stderr, "ghv: note: comment %d is a reply; the draft replies to the first comment of its thread, %d\n", id, root)
	}
	return a.submit(draft.Draft{Repo: repo, Fields: draft.ReviewReplyFields{Number: n, CommentID: root, Body: body}})
}

// threadRoot returns the first comment of id's thread, checking that the
// thread is on pull request n. GitHub does not take replies to replies,
// and a reply's in_reply_to_id names the root (checked 2026-10-18).
func threadRoot(ctx context.Context, c *read.Client, repo string, n int, id int64) (int64, error) {
	resp, err := c.Get(ctx, fmt.Sprintf("repos/%s/pulls/comments/%d", repo, id), "")
	var ae *read.APIError
	if errors.As(err, &ae) && ae.Status == 404 {
		return 0, &read.APIError{Status: 404, Code: read.CodeNotFound, Message: fmt.Sprintf("no review comment %d in %s; for a conversation comment, use ghv comment", id, repo)}
	}
	if err != nil {
		return 0, err
	}
	var g struct {
		InReplyToID    int64  `json:"in_reply_to_id"`
		PullRequestURL string `json:"pull_request_url"`
	}
	if err := json.Unmarshal(resp.Body, &g); err != nil {
		return 0, fmt.Errorf("GitHub's review comment: %v", err)
	}
	if g.PullRequestURL == "" {
		return 0, fmt.Errorf("GitHub sent no pull request for review comment %d", id)
	}
	if on := path.Base(g.PullRequestURL); on != strconv.Itoa(n) {
		return 0, usagef("review comment %d is on pull request %s, not %d", id, on, n)
	}
	if g.InReplyToID > 0 {
		return g.InReplyToID, nil
	}
	return id, nil
}
