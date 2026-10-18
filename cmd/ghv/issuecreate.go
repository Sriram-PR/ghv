package main

import (
	"context"

	"github.com/Sriram-PR/ghv/internal/draft"
)

func init() {
	register(&command{
		path:  "issue create",
		flags: []flag{draftRepoFlag, titleFlag, bodyFileFlag},
		help:  "Draft an issue for the human to send with ghv-apply",
		run:   runIssueCreate,
	})
}

func runIssueCreate(ctx context.Context, a *app) error {
	if err := a.requireFlags("title", "body-file"); err != nil {
		return err
	}
	repo, err := a.draftRepo()
	if err != nil {
		return err
	}
	body, err := a.body()
	if err != nil {
		return err
	}
	return a.submit(draft.Draft{Repo: repo, Fields: draft.IssueCreateFields{Title: a.inv.str("title"), Body: body}})
}
