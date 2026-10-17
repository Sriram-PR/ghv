package main

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"
	"time"
)

func init() {
	register(&command{
		path:  "run list",
		flags: []flag{repoFlag, commitFlag, runLimitFlag},
		help:  "List workflow runs, most recent first",
		run:   runRunList,
	})
}

var (
	commitFlag   = flag{name: "commit", kind: stringFlag, arg: "SHA", help: "only runs for this commit (the full 40-character SHA)"}
	runLimitFlag = flag{name: "limit", kind: intFlag, arg: "N", help: "stop after N runs", def: "20"}
	fullSHARE    = regexp.MustCompile(`^[0-9a-f]{40}$`)
)

type githubRun struct {
	ID           int64     `json:"id"`
	Name         string    `json:"name"`
	DisplayTitle string    `json:"display_title"`
	Status       string    `json:"status"`
	Conclusion   *string   `json:"conclusion"`
	Event        string    `json:"event"`
	HeadBranch   string    `json:"head_branch"`
	HeadSHA      string    `json:"head_sha"`
	RunNumber    int       `json:"run_number"`
	RunAttempt   int       `json:"run_attempt"`
	CreatedAt    time.Time `json:"created_at"`
	HTMLURL      string    `json:"html_url"`
}

// runSummary is one item of ghv run list --json. State is the conclusion
// once the run has completed, else its status.
type runSummary struct {
	ID         int64     `json:"id"`
	Workflow   string    `json:"workflow"`
	Title      string    `json:"title"`
	State      string    `json:"state"`
	Status     string    `json:"status"`
	Conclusion string    `json:"conclusion"`
	Event      string    `json:"event"`
	Branch     string    `json:"branch"`
	HeadSHA    string    `json:"head_sha"`
	RunNumber  int       `json:"run_number"`
	Attempt    int       `json:"attempt"`
	CreatedAt  time.Time `json:"created_at"`
	URL        string    `json:"url"`
}

func (g githubRun) summary() runSummary {
	conclusion := ""
	if g.Conclusion != nil {
		conclusion = *g.Conclusion
	}
	state := g.Status
	if g.Status == "completed" && conclusion != "" {
		state = conclusion
	}
	return runSummary{g.ID, g.Name, g.DisplayTitle, state, g.Status, conclusion, g.Event, g.HeadBranch, g.HeadSHA,
		g.RunNumber, g.RunAttempt, g.CreatedAt, g.HTMLURL}
}

func runRunList(ctx context.Context, a *app) error {
	path := "actions/runs"
	if sha := strings.ToLower(a.inv.str("commit")); sha != "" {
		if !fullSHARE.MatchString(sha) {
			return usagef("--commit needs the full 40-character commit SHA; GitHub matches a short one to nothing")
		}
		path += "?head_sha=" + sha
	}
	repo, err := a.repo()
	if err != nil {
		return err
	}
	c, err := a.read()
	if err != nil {
		return err
	}
	items, err := c.Paginate(ctx, fmt.Sprintf("repos/%s/%s", repo, path), "", a.inv.int("limit"))
	if err != nil {
		return err
	}
	list := []runSummary{}
	for _, raw := range items {
		var g githubRun
		if err := json.Unmarshal(raw, &g); err != nil {
			return fmt.Errorf("GitHub's workflow runs: %v", err)
		}
		list = append(list, g.summary())
	}
	if a.json() {
		return writeJSON(a.stdout, list)
	}
	rows := make([][]string, len(list))
	for i, r := range list {
		rows[i] = []string{strconv.FormatInt(r.ID, 10), r.State, r.Workflow, r.Branch, r.Title}
	}
	writeTable(a.stdout, rows)
	return nil
}
