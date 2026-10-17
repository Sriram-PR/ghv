package main

import (
	"context"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/Sriram-PR/ghv/internal/read"
	"github.com/Sriram-PR/ghv/internal/sanitize"
)

func init() {
	register(&command{
		path: "release download", args: []string{"TAG"},
		flags: []flag{repoFlag,
			{name: "asset", kind: multiFlag, arg: "NAME", help: "only this asset; repeatable"},
			{name: "dir", kind: stringFlag, arg: "DIR", help: "save into DIR, created if missing", def: "."},
		},
		help: "Download a release's assets; an existing file is never overwritten",
		run:  runReleaseDownload,
	})
}

// assetSaved is one item of ghv release download --json. Verified is true
// when the file matched GitHub's sha256 digest; older assets have none.
type assetSaved struct {
	Name     string `json:"name"`
	Path     string `json:"path"`
	Size     int64  `json:"size"`
	Verified bool   `json:"verified"`
}

func runReleaseDownload(ctx context.Context, a *app) error {
	repo, err := a.repo()
	if err != nil {
		return err
	}
	g, err := a.getRelease(ctx, repo, a.inv.args[0])
	if err != nil {
		return err
	}
	pick, err := pickAssets(g, a.inv.list("asset"))
	if err != nil {
		return err
	}
	dir := a.inv.str("dir")
	target := dir
	if !filepath.IsAbs(target) {
		target = filepath.Join(a.dir, target)
	}
	for _, as := range pick {
		if _, err := os.Lstat(filepath.Join(target, as.Name)); err == nil {
			return usagef("%s already exists; ghv never overwrites a file", filepath.Join(dir, as.Name))
		}
	}
	if err := os.MkdirAll(target, 0o755); err != nil {
		return err
	}
	c, err := a.read()
	if err != nil {
		return err
	}
	saved := []assetSaved{}
	for _, as := range pick {
		verified, err := saveAsset(ctx, c, repo, as, filepath.Join(target, as.Name))
		if err != nil {
			return err
		}
		saved = append(saved, assetSaved{as.Name, filepath.Join(dir, as.Name), as.Size, verified})
		if !a.json() {
			check := "no digest from GitHub"
			if verified {
				check = "sha256 checked"
			}
			fmt.Fprintf(a.stdout, "saved %s (%d byte%s, %s)\n", sanitize.Line(filepath.Join(dir, as.Name)), as.Size, plural(int(as.Size)), check)
		}
	}
	if a.json() {
		return writeJSON(a.stdout, saved)
	}
	return nil
}

// pickAssets returns the named assets, or all of them, after checking that
// each name is a plain file name.
func pickAssets(g githubRelease, names []string) ([]githubAsset, error) {
	pick := g.Assets
	if len(names) > 0 {
		pick = nil
		for _, n := range names {
			i := -1
			for j, as := range g.Assets {
				if as.Name == n {
					i = j
				}
			}
			if i < 0 {
				return nil, usagef("no asset named %q in release %s", n, g.TagName)
			}
			pick = append(pick, g.Assets[i])
		}
	}
	if len(pick) == 0 {
		return nil, fmt.Errorf("release %s has no assets", g.TagName)
	}
	for _, as := range pick {
		if !safeName(as.Name) {
			return nil, fmt.Errorf("unsafe asset name %q", as.Name)
		}
	}
	return pick, nil
}

// saveAsset streams the asset into a new file at path, capped at the
// asset's size and checked against its digest; on any error the file is
// removed.
func saveAsset(ctx context.Context, c *read.Client, repo string, as githubAsset, path string) (verified bool, err error) {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return false, err
	}
	defer func() {
		if cerr := f.Close(); err == nil {
			err = cerr
		}
		if err != nil {
			os.Remove(path)
		}
	}()
	h := sha256.New()
	n, err := c.Download(ctx, fmt.Sprintf("repos/%s/releases/assets/%d", repo, as.ID), "application/octet-stream", io.MultiWriter(f, h), as.Size)
	if err != nil {
		return false, err
	}
	if n != as.Size {
		return false, fmt.Errorf("%s: got %d of %d bytes", as.Name, n, as.Size)
	}
	digest := ""
	if as.Digest != nil {
		digest = *as.Digest
	}
	if err := checkDigest(digest, h.Sum(nil)); err != nil {
		return false, fmt.Errorf("%s %w", as.Name, err)
	}
	return strings.HasPrefix(digest, "sha256:"), nil
}
