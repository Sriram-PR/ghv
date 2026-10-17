package main

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"time"

	"github.com/Sriram-PR/ghv/internal/sanitize"
)

func init() {
	register(&command{
		path:  "release list",
		flags: []flag{repoFlag, {name: "limit", kind: intFlag, arg: "N", help: "stop after N releases", def: "30"}},
		help:  "List releases, newest first",
		run:   runReleaseList,
	})
	register(&command{path: "release view", args: []string{"TAG"}, flags: []flag{repoFlag}, help: "Show a release and its assets", run: runReleaseView})
}

type githubRelease struct {
	TagName     string        `json:"tag_name"`
	Name        string        `json:"name"`
	Draft       bool          `json:"draft"`
	Prerelease  bool          `json:"prerelease"`
	Author      githubUser    `json:"author"`
	Body        string        `json:"body"`
	CreatedAt   time.Time     `json:"created_at"`
	PublishedAt *time.Time    `json:"published_at"`
	HTMLURL     string        `json:"html_url"`
	Assets      []githubAsset `json:"assets"`
}

type githubAsset struct {
	ID            int64   `json:"id"`
	Name          string  `json:"name"`
	ContentType   string  `json:"content_type"`
	Size          int64   `json:"size"`
	DownloadCount int     `json:"download_count"`
	Digest        *string `json:"digest"`
	URL           string  `json:"browser_download_url"`
}

func (g githubRelease) state() string {
	switch {
	case g.Draft:
		return "draft"
	case g.Prerelease:
		return "prerelease"
	}
	return "published"
}

// releaseSummary is one item of ghv release list --json.
type releaseSummary struct {
	Tag         string     `json:"tag"`
	Name        string     `json:"name"`
	Draft       bool       `json:"draft"`
	Prerelease  bool       `json:"prerelease"`
	PublishedAt *time.Time `json:"published_at"`
	URL         string     `json:"url"`
}

// releaseView is the stable --json schema of ghv release view.
type releaseView struct {
	Repo        string      `json:"repo"`
	Tag         string      `json:"tag"`
	Name        string      `json:"name"`
	Draft       bool        `json:"draft"`
	Prerelease  bool        `json:"prerelease"`
	Author      string      `json:"author"`
	CreatedAt   time.Time   `json:"created_at"`
	PublishedAt *time.Time  `json:"published_at"`
	URL         string      `json:"url"`
	Body        string      `json:"body"`
	Assets      []assetView `json:"assets"`
}

// assetView is a release asset. Digest is GitHub's "sha256:<hex>", or ""
// for older uploads.
type assetView struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	ContentType string `json:"content_type"`
	Size        int64  `json:"size"`
	Downloads   int    `json:"downloads"`
	Digest      string `json:"digest"`
	URL         string `json:"url"`
}

func runReleaseList(ctx context.Context, a *app) error {
	repo, err := a.repo()
	if err != nil {
		return err
	}
	c, err := a.read()
	if err != nil {
		return err
	}
	items, err := c.Paginate(ctx, fmt.Sprintf("repos/%s/releases", repo), "", a.inv.int("limit"))
	if err != nil {
		return err
	}
	list := []releaseSummary{}
	var rows [][]string
	for _, raw := range items {
		var g githubRelease
		if err := json.Unmarshal(raw, &g); err != nil {
			return fmt.Errorf("GitHub's releases: %v", err)
		}
		list = append(list, releaseSummary{g.TagName, g.Name, g.Draft, g.Prerelease, g.PublishedAt, g.HTMLURL})
		published := "-"
		if g.PublishedAt != nil {
			published = g.PublishedAt.UTC().Format(time.RFC3339)
		}
		rows = append(rows, []string{g.TagName, g.state(), published, g.Name})
	}
	if a.json() {
		return writeJSON(a.stdout, list)
	}
	writeTable(a.stdout, rows)
	return nil
}

// getRelease reads the release tagged tag.
func (a *app) getRelease(ctx context.Context, repo, tag string) (githubRelease, error) {
	var g githubRelease
	if tag == "" {
		return g, usagef("give a release tag")
	}
	c, err := a.read()
	if err != nil {
		return g, err
	}
	resp, err := c.Get(ctx, fmt.Sprintf("repos/%s/releases/tags/%s", repo, pathSegment(tag)), "")
	if err != nil {
		return g, err
	}
	if err := json.Unmarshal(resp.Body, &g); err != nil {
		return g, fmt.Errorf("GitHub's release: %v", err)
	}
	return g, nil
}

func (g githubAsset) view() assetView {
	v := assetView{g.ID, g.Name, g.ContentType, g.Size, g.DownloadCount, "", g.URL}
	if g.Digest != nil {
		v.Digest = *g.Digest
	}
	return v
}

func runReleaseView(ctx context.Context, a *app) error {
	repo, err := a.repo()
	if err != nil {
		return err
	}
	g, err := a.getRelease(ctx, repo, a.inv.args[0])
	if err != nil {
		return err
	}
	v := releaseView{repo, g.TagName, g.Name, g.Draft, g.Prerelease, g.Author.Login, g.CreatedAt, g.PublishedAt, g.HTMLURL, g.Body, []assetView{}}
	for _, as := range g.Assets {
		v.Assets = append(v.Assets, as.view())
	}
	if a.json() {
		return writeJSON(a.stdout, v)
	}
	out := a.stdout
	fmt.Fprintf(out, "%s %s %s\n\n", repo, sanitize.Line(v.Tag), sanitize.Line(v.Name))
	row := rowWriter(out)
	row("state", g.state())
	row("author", v.Author)
	if v.PublishedAt != nil {
		row("published", v.PublishedAt.UTC().Format(time.RFC3339))
	}
	row("assets", strconv.Itoa(len(v.Assets)))
	row("url", v.URL)
	if len(v.Assets) > 0 {
		rows := make([][]string, len(v.Assets))
		for i, as := range v.Assets {
			rows[i] = []string{as.Name, strconv.FormatInt(as.Size, 10)}
		}
		fmt.Fprintln(out)
		writeTable(out, rows)
	}
	writeBody(out, v.Body)
	return nil
}
