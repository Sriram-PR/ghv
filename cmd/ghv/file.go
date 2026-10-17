package main

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/Sriram-PR/ghv/internal/read"
	"github.com/Sriram-PR/ghv/internal/sanitize"
)

const rawAccept = "application/vnd.github.raw+json"

func init() {
	register(&command{
		path: "file", args: []string{"R", "PATH"},
		flags: []flag{{name: "ref", kind: stringFlag, arg: "REF", help: "branch, tag, or commit; default: the default branch"}},
		help:  "Print a file, or list a directory (PATH / for the top level)",
		run:   runFile,
	})
}

// fileView is the stable --json schema of ghv file. Type is file, dir,
// symlink, or submodule. A file that is not UTF-8 comes as ContentBase64.
type fileView struct {
	Repo          string     `json:"repo"`
	Path          string     `json:"path"`
	Ref           string     `json:"ref,omitempty"`
	Type          string     `json:"type"`
	Size          int64      `json:"size"`
	SHA           string     `json:"sha,omitempty"`
	Content       *string    `json:"content,omitempty"`
	ContentBase64 string     `json:"content_base64,omitempty"`
	Target        string     `json:"target,omitempty"`
	SubmoduleURL  string     `json:"submodule_url,omitempty"`
	Entries       []dirEntry `json:"entries,omitempty"`
	raw           []byte
}

type dirEntry struct {
	Type string `json:"type"`
	Name string `json:"name"`
	Path string `json:"path"`
	Size int64  `json:"size"`
	SHA  string `json:"sha"`
}

type githubContent struct {
	Type         string `json:"type"`
	Encoding     string `json:"encoding"`
	Size         int64  `json:"size"`
	Name         string `json:"name"`
	Path         string `json:"path"`
	SHA          string `json:"sha"`
	Content      string `json:"content"`
	Target       string `json:"target"`
	SubmoduleURL string `json:"submodule_git_url"`
}

// contentsPath escapes each segment of p; "", ".", and "/" are the top
// level. Empty, ".", and ".." segments are refused.
func contentsPath(repo, p string) (string, error) {
	p = strings.Trim(p, "/")
	if p == "" || p == "." {
		return "repos/" + repo + "/contents", nil
	}
	segs := strings.Split(p, "/")
	for i, s := range segs {
		if s == "" || s == "." || s == ".." {
			return "", usagef("%q is not a path in the repository", p)
		}
		segs[i] = url.PathEscape(s)
	}
	return "repos/" + repo + "/contents/" + strings.Join(segs, "/"), nil
}

func runFile(ctx context.Context, a *app) error {
	repo := a.inv.args[0]
	if !validRepo(repo) {
		return usagef("%q is not OWNER/REPO", repo)
	}
	p, err := contentsPath(repo, a.inv.args[1])
	if err != nil {
		return err
	}
	ref := a.inv.str("ref")
	if ref != "" {
		p += "?" + url.Values{"ref": {ref}}.Encode()
	}
	c, err := a.read()
	if err != nil {
		return err
	}
	resp, err := c.Get(ctx, p, "")
	if err != nil {
		return err
	}
	v := fileView{Repo: repo, Path: strings.Trim(a.inv.args[1], "/"), Ref: ref}
	var entries []githubContent
	if json.Unmarshal(resp.Body, &entries) == nil {
		v.Type, v.Entries = "dir", []dirEntry{}
		for _, g := range entries {
			v.Entries = append(v.Entries, dirEntry{g.Type, g.Name, g.Path, g.Size, g.SHA})
		}
		return a.printFile(v)
	}
	var g githubContent
	if err := json.Unmarshal(resp.Body, &g); err != nil {
		return fmt.Errorf("GitHub's contents response: %v", err)
	}
	v.Type, v.Path, v.Size, v.SHA, v.Target, v.SubmoduleURL = g.Type, g.Path, g.Size, g.SHA, g.Target, g.SubmoduleURL
	if g.Type == "file" {
		if v.raw, err = fileBytes(ctx, c, p, g); err != nil {
			return err
		}
		if utf8.Valid(v.raw) {
			s := string(v.raw)
			v.Content = &s
		} else {
			v.ContentBase64 = base64.StdEncoding.EncodeToString(v.raw)
		}
	}
	return a.printFile(v)
}

// fileBytes decodes a file's base64 content. Over 1 MB GitHub sends
// encoding "none", and the raw media type gives the bytes.
func fileBytes(ctx context.Context, c *read.Client, p string, g githubContent) ([]byte, error) {
	switch g.Encoding {
	case "base64":
		b, err := base64.StdEncoding.DecodeString(strings.ReplaceAll(g.Content, "\n", ""))
		if err != nil {
			return nil, fmt.Errorf("GitHub's file content: %v", err)
		}
		return b, nil
	case "none", "":
		resp, err := c.Get(ctx, p, rawAccept)
		if err != nil {
			return nil, err
		}
		return resp.Body, nil
	}
	return nil, fmt.Errorf("GitHub sent the file in an unknown encoding %q", g.Encoding)
}

func (a *app) printFile(v fileView) error {
	if a.json() {
		return writeJSON(a.stdout, v)
	}
	out := a.stdout
	switch v.Type {
	case "dir":
		rows := make([][]string, len(v.Entries))
		for i, d := range v.Entries {
			name := d.Name
			if d.Type == "dir" {
				name += "/"
			}
			rows[i] = []string{d.Type, fmt.Sprint(d.Size), name}
		}
		writeTable(out, rows)
	case "symlink":
		fmt.Fprintf(out, "%s is a symlink to %s\n", sanitize.Line(v.Path), sanitize.Line(v.Target))
	case "submodule":
		fmt.Fprintf(out, "%s is a submodule: %s at %s\n", sanitize.Line(v.Path), sanitize.Line(v.SubmoduleURL), sanitize.Line(v.SHA))
	default:
		if v.Content == nil {
			fmt.Fprintf(out, "%s is a binary file of %d bytes; --json gives it as content_base64\n", sanitize.Line(v.Path), len(v.raw))
			return nil
		}
		_, err := io.WriteString(out, sanitize.Terminal(*v.Content))
		return err
	}
	return nil
}
