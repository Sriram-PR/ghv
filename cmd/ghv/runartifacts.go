package main

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"time"

	"github.com/Sriram-PR/ghv/internal/archive"
	"github.com/Sriram-PR/ghv/internal/read"
	"github.com/Sriram-PR/ghv/internal/sanitize"
)

func init() {
	register(&command{
		path: "run artifacts", args: []string{"ID"},
		flags: []flag{repoFlag,
			{name: "download", kind: stringFlag, arg: "DIR", help: "download and extract into DIR/<name>/; DIR must not exist"},
			{name: "name", kind: multiFlag, arg: "NAME", help: "with --download, only this artifact; repeatable"},
			{name: "max-size", kind: intFlag, arg: "MIB", help: "stop past this many MiB extracted", def: "2048"},
			{name: "max-files", kind: intFlag, arg: "N", help: "stop past this many files extracted", def: "100000"},
		},
		help: "List a workflow run's artifacts, or download them; downloading needs a read token",
		run:  runRunArtifacts,
	})
}

type githubArtifact struct {
	ID        int64      `json:"id"`
	Name      string     `json:"name"`
	Size      int64      `json:"size_in_bytes"`
	Expired   bool       `json:"expired"`
	ExpiresAt *time.Time `json:"expires_at"`
	Digest    *string    `json:"digest"`
}

// artifactView is one item of ghv run artifacts --json. Digest is
// GitHub's "sha256:<hex>" of the zip, or "" when GitHub has none.
type artifactView struct {
	ID        int64      `json:"id"`
	Name      string     `json:"name"`
	Size      int64      `json:"size"`
	Expired   bool       `json:"expired"`
	ExpiresAt *time.Time `json:"expires_at"`
	Digest    string     `json:"digest"`
}

// artifactSaved is one item of ghv run artifacts --download --json.
type artifactSaved struct {
	Name  string   `json:"name"`
	Dir   string   `json:"dir"`
	Files []string `json:"files"`
}

func runRunArtifacts(ctx context.Context, a *app) error {
	id, err := a.runID(0)
	if err != nil {
		return err
	}
	dir, names := a.inv.str("download"), a.inv.list("name")
	if dir == "" && len(names) > 0 {
		return usagef("--name needs --download")
	}
	repo, err := a.repo()
	if err != nil {
		return err
	}
	var lim archive.Limits
	target := dir
	if dir != "" {
		if lim.MaxBytes, err = a.maxSize(); err != nil {
			return err
		}
		if lim.MaxFiles = a.inv.int("max-files"); lim.MaxFiles < 1 {
			return usagef("--max-files must be at least 1")
		}
		if !filepath.IsAbs(target) {
			target = filepath.Join(a.dir, target)
		}
		if _, err := os.Lstat(target); err == nil {
			return usagef("%s already exists; give a new directory", dir)
		}
	}
	c, err := a.read()
	if err != nil {
		return err
	}
	if dir != "" && !c.Authenticated() {
		return &read.APIError{Status: 401, Code: read.CodeTokenRequired, Message: "downloading artifacts needs a read token"}
	}
	items, err := c.Paginate(ctx, fmt.Sprintf("repos/%s/actions/runs/%d/artifacts", repo, id), "", read.DefaultLimit)
	if err != nil {
		return err
	}
	list := []artifactView{}
	for _, raw := range items {
		var g githubArtifact
		if err := json.Unmarshal(raw, &g); err != nil {
			return fmt.Errorf("GitHub's artifacts: %v", err)
		}
		v := artifactView{ID: g.ID, Name: g.Name, Size: g.Size, Expired: g.Expired, ExpiresAt: g.ExpiresAt}
		if g.Digest != nil {
			v.Digest = *g.Digest
		}
		list = append(list, v)
	}
	if dir == "" {
		return printArtifacts(a, list)
	}
	pick, err := a.pickArtifacts(list, names, id)
	if err != nil {
		return err
	}
	saved, err := saveArtifacts(ctx, c, repo, pick, target, lim)
	if err != nil {
		return err
	}
	for i := range saved {
		saved[i].Dir = filepath.Join(dir, saved[i].Name)
	}
	if a.json() {
		return writeJSON(a.stdout, saved)
	}
	for _, s := range saved {
		fmt.Fprintf(a.stdout, "saved %s: %d file%s in %s\n", sanitize.Line(s.Name), len(s.Files), plural(len(s.Files)), sanitize.Line(s.Dir))
	}
	return nil
}

func printArtifacts(a *app, list []artifactView) error {
	if a.json() {
		return writeJSON(a.stdout, list)
	}
	rows := make([][]string, len(list))
	for i, v := range list {
		state := "expired"
		if !v.Expired && v.ExpiresAt != nil {
			state = "expires " + v.ExpiresAt.UTC().Format(time.RFC3339)
		}
		rows[i] = []string{v.Name, strconv.FormatInt(v.Size, 10), state}
	}
	writeTable(a.stdout, rows)
	return nil
}

// pickArtifacts returns the named artifacts, or every unexpired one.
func (a *app) pickArtifacts(list []artifactView, names []string, id int64) ([]artifactView, error) {
	var pick []artifactView
	if len(names) == 0 {
		for _, v := range list {
			if v.Expired {
				fmt.Fprintf(a.stderr, "ghv: skipped %s: it has expired\n", sanitize.Line(v.Name))
				continue
			}
			pick = append(pick, v)
		}
	}
	for _, n := range names {
		i := -1
		for j, v := range list {
			if v.Name == n {
				i = j
			}
		}
		if i < 0 {
			return nil, usagef("no artifact named %q in run %d", n, id)
		}
		if list[i].Expired {
			return nil, fmt.Errorf("artifact %s has expired", n)
		}
		pick = append(pick, list[i])
	}
	if len(pick) == 0 {
		return nil, fmt.Errorf("run %d has no artifacts to download", id)
	}
	for _, v := range pick {
		if !safeName(v.Name) {
			return nil, fmt.Errorf("unsafe artifact name %q", v.Name)
		}
	}
	return pick, nil
}

// saveArtifacts creates target and extracts each artifact into
// target/<name>; on any error it removes target.
func saveArtifacts(ctx context.Context, c *read.Client, repo string, pick []artifactView, target string, lim archive.Limits) (saved []artifactSaved, err error) {
	if err := os.Mkdir(target, 0o755); err != nil {
		return nil, err
	}
	defer func() {
		if err != nil {
			os.RemoveAll(target)
		}
	}()
	for _, v := range pick {
		files, n, err := saveArtifact(ctx, c, repo, v, target, lim)
		if err != nil {
			return nil, err
		}
		lim.MaxBytes -= n
		lim.MaxFiles -= len(files)
		saved = append(saved, artifactSaved{Name: v.Name, Files: files})
	}
	return saved, nil
}

// saveArtifact downloads the zip into a temporary file inside target, so
// nothing is written outside it, checks the digest, and extracts it.
func saveArtifact(ctx context.Context, c *read.Client, repo string, v artifactView, target string, lim archive.Limits) ([]string, int64, error) {
	tmp, err := os.CreateTemp(target, ".download-*.zip")
	if err != nil {
		return nil, 0, err
	}
	defer os.Remove(tmp.Name())
	defer tmp.Close()
	max := v.Size
	if max <= 0 {
		max = lim.MaxBytes
	}
	h := sha256.New()
	n, err := c.Download(ctx, fmt.Sprintf("repos/%s/actions/artifacts/%d/zip", repo, v.ID), "", io.MultiWriter(tmp, h), max)
	if err != nil {
		return nil, 0, err
	}
	if err := checkDigest(v.Digest, h.Sum(nil)); errors.Is(err, errDigest) {
		return nil, 0, fmt.Errorf("artifact %s %w", v.Name, err)
	}
	return archive.Extract(tmp, n, filepath.Join(target, v.Name), lim)
}
