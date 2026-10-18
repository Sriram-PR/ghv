package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"

	"github.com/Sriram-PR/ghv/internal/draft"
	"github.com/Sriram-PR/ghv/internal/read"
	"github.com/Sriram-PR/ghv/internal/sanitize"
)

func init() {
	register(&command{
		path: "pr create",
		flags: []flag{
			draftRepoFlag,
			{name: "head", kind: stringFlag, arg: "OWNER:BRANCH", help: "the branch to merge, in OWNER's repository of the same name (required)"},
			{name: "base", kind: stringFlag, arg: "BRANCH", help: "the branch to merge into; default: the repository's default branch"},
			titleFlag, bodyFileFlag,
			{name: "draft", kind: boolFlag, help: "open the pull request as a draft"},
			{name: "no-maintainer-edit", kind: boolFlag, help: "don't let maintainers push to the head branch"},
		},
		help: "Draft a pull request, pinned to the head branch's current commit",
		run:  runPRCreate,
	})
}

func runPRCreate(ctx context.Context, a *app) error {
	if err := a.requireFlags("head", "title", "body-file"); err != nil {
		return err
	}
	repo, err := a.draftRepo()
	if err != nil {
		return err
	}
	head := a.inv.str("head")
	owner, branch, ok := strings.Cut(head, ":")
	if !ok || !ownerRE.MatchString(owner) || !draft.ValidBranch(branch) {
		return usagef("--head %q is not OWNER:BRANCH", head)
	}
	base := a.inv.str("base")
	_, baseSet := a.inv.values["base"]
	if baseSet && !draft.ValidBranch(base) {
		return usagef("--base %q is not a branch name", base)
	}
	body, err := a.body()
	if err != nil {
		return err
	}
	c, err := a.read()
	if err != nil {
		return err
	}
	if !baseSet {
		if base, err = defaultBranch(ctx, c, repo); err != nil {
			return err
		}
	}
	_, name, _ := strings.Cut(repo, "/")
	sha, err := branchHead(ctx, c, owner+"/"+name, branch)
	if err != nil {
		return err
	}
	err = a.submit(draft.Draft{Repo: repo, Fields: draft.PRCreateFields{
		Base:                base,
		Head:                head,
		HeadSHA:             sha,
		Title:               a.inv.str("title"),
		Body:                body,
		Draft:               a.inv.bool("draft"),
		MaintainerCanModify: !a.inv.bool("no-maintainer-edit"),
	}})
	if err != nil {
		return err
	}
	fmt.Fprintf(a.stderr, "ghv: note: the draft pins %s at %s; ghv-apply refuses it if the branch moves\n", sanitize.Line(head), sha)
	return nil
}

func defaultBranch(ctx context.Context, c *read.Client, repo string) (string, error) {
	resp, err := c.Get(ctx, "repos/"+repo, "")
	if err != nil {
		return "", err
	}
	var g struct {
		DefaultBranch string `json:"default_branch"`
	}
	if err := json.Unmarshal(resp.Body, &g); err != nil {
		return "", fmt.Errorf("GitHub's repository: %v", err)
	}
	if !draft.ValidBranch(g.DefaultBranch) {
		return "", fmt.Errorf("GitHub sent %q as the default branch", g.DefaultBranch)
	}
	return g.DefaultBranch, nil
}

// branchHead returns the commit branch points to in repo. The branches
// endpoint matches branch names only, where commits/{ref} would also take
// a tag or a SHA.
func branchHead(ctx context.Context, c *read.Client, repo, branch string) (string, error) {
	resp, err := c.Get(ctx, fmt.Sprintf("repos/%s/branches/%s", repo, pathSegment(branch)), "")
	var ae *read.APIError
	if errors.As(err, &ae) && ae.Status == 404 {
		return "", &read.APIError{Status: 404, Code: read.CodeNotFound, Message: fmt.Sprintf("no branch %q in %s; push it there first", branch, repo)}
	}
	if err != nil {
		return "", err
	}
	var g struct {
		Commit struct {
			SHA string `json:"sha"`
		} `json:"commit"`
	}
	if err := json.Unmarshal(resp.Body, &g); err != nil {
		return "", fmt.Errorf("GitHub's branch: %v", err)
	}
	if !draft.ValidSHA(g.Commit.SHA) {
		return "", fmt.Errorf("GitHub sent %q as the head commit", g.Commit.SHA)
	}
	return g.Commit.SHA, nil
}
