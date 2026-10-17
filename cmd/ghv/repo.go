package main

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
)

// The same rules as internal/draft's repo validation.
var (
	ownerRE    = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,37}[A-Za-z0-9])?$`)
	repoNameRE = regexp.MustCompile(`^[A-Za-z0-9._-]{1,100}$`)
	scpRE      = regexp.MustCompile(`^(?:[^@/:]+@)?(?i:github\.com):(.+)$`)
)

func validRepo(s string) bool {
	owner, name, ok := strings.Cut(s, "/")
	return ok && ownerRE.MatchString(owner) && repoNameRE.MatchString(name) && name != "." && name != ".."
}

// repo returns -R, or the GitHub repository of this checkout's origin
// remote. It reads .git/config itself: ghv runs git only for pr checkout.
func (a *app) repo() (string, error) {
	if r := a.inv.str("repo"); r != "" {
		if !validRepo(r) {
			return "", usagef("-R %q is not OWNER/REPO", r)
		}
		return r, nil
	}
	r, err := inferRepo(a.dir)
	if err != nil {
		return "", usagef("no -R given, and %v", err)
	}
	return r, nil
}

func inferRepo(dir string) (string, error) {
	gitDir, err := findGitDir(dir)
	if err != nil {
		return "", err
	}
	f, err := os.Open(filepath.Join(commonDir(gitDir), "config"))
	if err != nil {
		return "", fmt.Errorf("cannot read the git config: %v", err)
	}
	defer f.Close()
	cfg, err := io.ReadAll(io.LimitReader(f, 1<<20))
	if err != nil {
		return "", err
	}
	u := remoteURL(cfg, "origin")
	if u == "" {
		return "", errors.New("this checkout has no origin remote")
	}
	r, ok := githubRepo(u)
	if !ok {
		return "", fmt.Errorf("the origin remote (%s) is not a github.com repository", u)
	}
	return r, nil
}

func findGitDir(dir string) (string, error) {
	for d := filepath.Clean(dir); ; d = filepath.Dir(d) {
		p := filepath.Join(d, ".git")
		if fi, err := os.Stat(p); err == nil {
			if fi.IsDir() {
				return p, nil
			}
			raw, err := os.ReadFile(p)
			if err != nil {
				return "", err
			}
			target, ok := strings.CutPrefix(strings.TrimSpace(string(raw)), "gitdir: ")
			if !ok {
				return "", fmt.Errorf("%s is not a gitdir file", p)
			}
			if !filepath.IsAbs(target) {
				target = filepath.Join(d, target)
			}
			return filepath.Clean(target), nil
		}
		if filepath.Dir(d) == d {
			return "", errors.New("this is not a git checkout")
		}
	}
}

// commonDir follows a linked worktree's commondir file to the main .git
// directory, which holds the config.
func commonDir(gitDir string) string {
	raw, err := os.ReadFile(filepath.Join(gitDir, "commondir"))
	if err != nil {
		return gitDir
	}
	c := strings.TrimSpace(string(raw))
	if !filepath.IsAbs(c) {
		c = filepath.Join(gitDir, c)
	}
	return filepath.Clean(c)
}

// remoteURL returns the first remote.<name>.url in a git config file.
// Section and key names are case-insensitive, the subsection is not.
func remoteURL(cfg []byte, name string) string {
	in := false
	sc := bufio.NewScanner(bytes.NewReader(cfg))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || line[0] == '#' || line[0] == ';' {
			continue
		}
		if strings.HasPrefix(line, "[") {
			header, _, _ := strings.Cut(line[1:], "]")
			section, sub, _ := strings.Cut(header, " ")
			in = strings.EqualFold(section, "remote") && strings.Trim(sub, `"`) == name
			continue
		}
		if !in {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok || !strings.EqualFold(strings.TrimSpace(k), "url") {
			continue
		}
		return configValue(strings.TrimSpace(v))
	}
	return ""
}

func configValue(v string) string {
	if strings.HasPrefix(v, `"`) {
		if end := strings.Index(v[1:], `"`); end >= 0 {
			return v[1 : end+1]
		}
	}
	if i := strings.IndexAny(v, "#;"); i >= 0 {
		v = v[:i]
	}
	return strings.TrimSpace(v)
}

// githubRepo extracts OWNER/REPO from a github.com remote URL, in URL or
// scp-like form.
func githubRepo(remote string) (string, bool) {
	var p string
	if m := scpRE.FindStringSubmatch(remote); m != nil && !strings.Contains(remote, "://") {
		p = m[1]
	} else {
		u, err := url.Parse(remote)
		if err != nil || !strings.EqualFold(u.Hostname(), "github.com") || u.Port() != "" {
			return "", false
		}
		switch u.Scheme {
		case "https", "http", "ssh", "git":
		default:
			return "", false
		}
		p = u.Path
	}
	p = strings.TrimSuffix(strings.Trim(p, "/"), ".git")
	if !validRepo(p) {
		return "", false
	}
	return p, true
}
