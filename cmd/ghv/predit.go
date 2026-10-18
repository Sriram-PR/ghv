package main

import (
	"context"

	"github.com/Sriram-PR/ghv/internal/draft"
)

func init() {
	register(&command{
		path:  "pr edit",
		args:  []string{"N"},
		flags: []flag{draftRepoFlag, titleFlag, bodyFileFlag},
		help:  "Draft a new title or body, or both, for pull request N",
		run:   runPREdit,
	})
}

func runPREdit(ctx context.Context, a *app) error {
	n, err := a.number(0)
	if err != nil {
		return err
	}
	_, hasTitle := a.inv.values["title"]
	_, hasBody := a.inv.values["body-file"]
	if !hasTitle && !hasBody {
		return usagef("ghv pr edit needs --title, --body-file, or both")
	}
	repo, err := a.draftRepo()
	if err != nil {
		return err
	}
	f := draft.PREditFields{Number: n}
	if hasTitle {
		title := a.inv.str("title")
		f.Title = &title
	}
	if hasBody {
		body, err := a.body()
		if err != nil {
			return err
		}
		f.Body = &body
	}
	return a.submit(draft.Draft{Repo: repo, Fields: f})
}
