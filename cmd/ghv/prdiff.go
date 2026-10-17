package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/Sriram-PR/ghv/internal/sanitize"
)

func init() {
	register(&command{path: "pr diff", args: []string{"N"}, flags: []flag{repoFlag}, help: "Print a pull request's diff", run: runPRDiff})
	register(&command{path: "pr commits", args: []string{"N"}, flags: []flag{repoFlag, limitFlag}, help: "List a pull request's commits (GitHub returns at most 250)", run: runPRCommits})
}

const diffAccept = "application/vnd.github.diff"

// prDiff is the stable --json schema of ghv pr diff.
type prDiff struct {
	Repo   string `json:"repo"`
	Number int    `json:"number"`
	Diff   string `json:"diff"`
}

func runPRDiff(ctx context.Context, a *app) error {
	repo, n, err := a.repoAndNumber()
	if err != nil {
		return err
	}
	c, err := a.read()
	if err != nil {
		return err
	}
	resp, err := c.Get(ctx, fmt.Sprintf("repos/%s/pulls/%d", repo, n), diffAccept)
	if err != nil {
		return err
	}
	if a.json() {
		return writeJSON(a.stdout, prDiff{repo, n, string(resp.Body)})
	}
	_, err = io.WriteString(a.stdout, sanitize.Terminal(string(resp.Body)))
	return err
}

// commitView is one item of ghv pr commits --json. Author is the GitHub
// login when GitHub links the commit to an account, else the git name.
type commitView struct {
	SHA     string    `json:"sha"`
	Author  string    `json:"author"`
	Date    time.Time `json:"date"`
	Message string    `json:"message"`
}

type githubCommit struct {
	SHA    string      `json:"sha"`
	Author *githubUser `json:"author"`
	Commit struct {
		Author struct {
			Name string    `json:"name"`
			Date time.Time `json:"date"`
		} `json:"author"`
		Message string `json:"message"`
	} `json:"commit"`
}

func runPRCommits(ctx context.Context, a *app) error {
	repo, n, err := a.repoAndNumber()
	if err != nil {
		return err
	}
	c, err := a.read()
	if err != nil {
		return err
	}
	items, err := c.Paginate(ctx, fmt.Sprintf("repos/%s/pulls/%d/commits", repo, n), "", a.inv.int("limit"))
	if err != nil {
		return err
	}
	list := []commitView{}
	for _, raw := range items {
		var g githubCommit
		if err := json.Unmarshal(raw, &g); err != nil {
			return fmt.Errorf("GitHub's commit list: %v", err)
		}
		v := commitView{SHA: g.SHA, Author: g.Commit.Author.Name, Date: g.Commit.Author.Date, Message: g.Commit.Message}
		if g.Author != nil && g.Author.Login != "" {
			v.Author = g.Author.Login
		}
		list = append(list, v)
	}
	if a.json() {
		return writeJSON(a.stdout, list)
	}
	rows := make([][]string, len(list))
	for i, v := range list {
		subject, _, _ := strings.Cut(v.Message, "\n")
		rows[i] = []string{short(v.SHA), v.Date.UTC().Format(time.RFC3339), v.Author, subject}
	}
	writeTable(a.stdout, rows)
	return nil
}
