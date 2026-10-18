package main

import (
	"context"
	"strings"

	"github.com/Sriram-PR/ghv/internal/draft"
)

func init() {
	register(&command{
		path:  "comment",
		args:  []string{"N"},
		flags: []flag{draftRepoFlag, bodyFileFlag},
		help:  "Draft a comment on issue or pull request N",
		run:   runComment,
	})
	register(&command{
		path: "fork",
		args: []string{"R"},
		help: "Draft a fork of repository R (OWNER/REPO)",
		run:  runFork,
	})
}

// commentBody is body for kinds where GitHub refuses an empty one.
func (a *app) commentBody() (string, error) {
	body, err := a.body()
	if err == nil && strings.TrimSpace(body) == "" {
		return "", usagef("the body is empty")
	}
	return body, err
}

func runComment(ctx context.Context, a *app) error {
	n, err := a.number(0)
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
	return a.submit(draft.Draft{Repo: repo, Fields: draft.CommentFields{Number: n, Body: body}})
}

func runFork(ctx context.Context, a *app) error {
	repo := a.inv.args[0]
	if !validRepo(repo) {
		return usagef("%q is not OWNER/REPO", repo)
	}
	return a.submit(draft.Draft{Repo: repo, Fields: draft.ForkFields{}})
}
