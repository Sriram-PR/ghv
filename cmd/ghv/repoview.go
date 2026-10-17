package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/Sriram-PR/ghv/internal/sanitize"
)

func init() {
	register(&command{path: "repo view", args: []string{"R?"}, help: "Show a repository; R defaults to the origin remote of this checkout", run: runRepoView})
}

// repoView is the stable --json schema of ghv repo view.
type repoView struct {
	FullName      string    `json:"full_name"`
	Description   string    `json:"description"`
	Fork          bool      `json:"fork"`
	Parent        string    `json:"parent,omitempty"`
	Archived      bool      `json:"archived"`
	DefaultBranch string    `json:"default_branch"`
	Stars         int       `json:"stars"`
	Forks         int       `json:"forks"`
	OpenIssues    int       `json:"open_issues"`
	License       string    `json:"license,omitempty"`
	Topics        []string  `json:"topics"`
	Homepage      string    `json:"homepage,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	PushedAt      time.Time `json:"pushed_at"`
	URL           string    `json:"url"`
}

type githubRepoJSON struct {
	FullName    string `json:"full_name"`
	Description string `json:"description"`
	Fork        bool   `json:"fork"`
	Parent      *struct {
		FullName string `json:"full_name"`
	} `json:"parent"`
	Archived        bool   `json:"archived"`
	DefaultBranch   string `json:"default_branch"`
	StargazersCount int    `json:"stargazers_count"`
	ForksCount      int    `json:"forks_count"`
	OpenIssuesCount int    `json:"open_issues_count"`
	License         *struct {
		SPDXID string `json:"spdx_id"`
	} `json:"license"`
	Topics    []string  `json:"topics"`
	Homepage  string    `json:"homepage"`
	CreatedAt time.Time `json:"created_at"`
	PushedAt  time.Time `json:"pushed_at"`
	HTMLURL   string    `json:"html_url"`
}

// repoArg is the positional R if given, else -R or the checkout's origin.
func (a *app) repoArg() (string, error) {
	if len(a.inv.args) > 0 {
		if r := a.inv.args[0]; !validRepo(r) {
			return "", usagef("%q is not OWNER/REPO", r)
		}
		return a.inv.args[0], nil
	}
	return a.repo()
}

func runRepoView(ctx context.Context, a *app) error {
	repo, err := a.repoArg()
	if err != nil {
		return err
	}
	c, err := a.read()
	if err != nil {
		return err
	}
	resp, err := c.Get(ctx, "repos/"+repo, "")
	if err != nil {
		return err
	}
	var g githubRepoJSON
	if err := json.Unmarshal(resp.Body, &g); err != nil {
		return fmt.Errorf("GitHub's repository response: %v", err)
	}
	v := repoView{
		FullName: g.FullName, Description: g.Description, Fork: g.Fork, Archived: g.Archived,
		DefaultBranch: g.DefaultBranch, Stars: g.StargazersCount, Forks: g.ForksCount,
		OpenIssues: g.OpenIssuesCount, Topics: g.Topics, Homepage: g.Homepage,
		CreatedAt: g.CreatedAt, PushedAt: g.PushedAt, URL: g.HTMLURL,
	}
	if g.Parent != nil {
		v.Parent = g.Parent.FullName
	}
	if g.License != nil {
		v.License = g.License.SPDXID
	}
	if v.Topics == nil {
		v.Topics = []string{}
	}
	if a.json() {
		return writeJSON(a.stdout, v)
	}
	out := a.stdout
	fmt.Fprintln(out, sanitize.Line(v.FullName))
	if v.Description != "" {
		fmt.Fprintln(out, sanitize.Line(v.Description))
	}
	fmt.Fprintln(out)
	row := func(k, val string) {
		if val != "" {
			fmt.Fprintf(out, "%-12s %s\n", k, sanitize.Line(val))
		}
	}
	row("fork of", v.Parent)
	if v.Archived {
		row("archived", "yes")
	}
	row("default", v.DefaultBranch)
	row("stars", fmt.Sprint(v.Stars))
	row("forks", fmt.Sprint(v.Forks))
	row("open issues", fmt.Sprint(v.OpenIssues))
	row("license", v.License)
	row("topics", strings.Join(v.Topics, ", "))
	row("homepage", v.Homepage)
	row("created", v.CreatedAt.Format(time.RFC3339))
	row("pushed", v.PushedAt.Format(time.RFC3339))
	row("url", v.URL)
	return nil
}
