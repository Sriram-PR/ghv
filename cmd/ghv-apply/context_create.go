package main

import (
	"context"
	"errors"
	"fmt"
	"path"
	"slices"
	"strings"

	"github.com/Sriram-PR/ghv/internal/draft"
	"github.com/Sriram-PR/ghv/internal/fetch"
)

// GitHub's compare lists at most this many files.
const compareFileCap = 300

// Files that change how a project builds, tests, or ships: a pull request
// touching them runs code in CI or on the maintainer's machine.
var (
	buildDirs  = []string{".github/", ".circleci/", ".buildkite/", ".husky/", ".devcontainer/"}
	buildExts  = []string{".mk", ".gradle", ".bzl", ".cmake"}
	buildNames = []string{
		"Makefile", "makefile", "GNUmakefile", "Dockerfile", "Containerfile", "Jenkinsfile", "Vagrantfile",
		"Rakefile", "Justfile", "justfile", "Taskfile.yml", "Taskfile.yaml", "CMakeLists.txt", "meson.build",
		"BUILD", "BUILD.bazel", "WORKSPACE", "MODULE.bazel", "build.gradle.kts", "settings.gradle.kts",
		"gradlew", "pom.xml", "build.xml", "setup.py", "setup.cfg", "pyproject.toml", "tox.ini", "noxfile.py",
		"conftest.py", "package.json", "Cargo.toml", "build.rs", "go.mod", "Gemfile", "composer.json",
		".gitlab-ci.yml", ".travis.yml", "azure-pipelines.yml", "appveyor.yml", ".pre-commit-config.yaml",
		".gitmodules", ".gitattributes", ".npmrc", ".yarnrc.yml", "action.yml", "action.yaml",
		"docker-compose.yml", "docker-compose.yaml", "compose.yml", "compose.yaml",
	}
)

func buildFile(name string) bool {
	if slices.ContainsFunc(buildDirs, func(d string) bool { return strings.HasPrefix(name, d) }) {
		return true
	}
	base := path.Base(name)
	return slices.Contains(buildNames, base) || strings.HasPrefix(base, "Dockerfile.") ||
		slices.ContainsFunc(buildExts, func(e string) bool { return strings.HasSuffix(base, e) })
}

// headOf is where a pull request's head branch lives: its owner's
// repository of the same name as the draft's.
func headOf(draftRepo string, f draft.PRCreateFields) (owner, repo, branch string) {
	owner, branch, _ = strings.Cut(f.Head, ":")
	return owner, owner + "/" + path.Base(draftRepo), branch
}

func short(sha string) string { return sha[:min(12, len(sha))] }

// createContext shows the code a pull request would propose. The head
// lives in its owner's repository of the same name, as ghv drafted it.
func (rv *reviewer) createContext(ctx context.Context, rc *reviewContext, repo fetch.Repo, draftRepo string, f draft.PRCreateFields) {
	owner, headRepo, branch := headOf(draftRepo, f)
	rc.line("Pull request from %s at %s into %s:%s", f.Head, short(f.HeadSHA), repo.FullName, f.Base)
	if !strings.EqualFold(owner, rv.login) {
		rc.flag("the head branch belongs to %s, not you", owner)
	}
	if f.MaintainerCanModify {
		rc.flag("maintainers of %s can push to %s (maintainer_can_modify)", repo.FullName, f.Head)
	}

	sha, err := rv.fc.BranchHead(ctx, headRepo, branch)
	switch {
	case errors.Is(err, fetch.ErrNotFound):
		rc.block("%s has no branch %s", headRepo, branch)
	case err != nil:
		rc.block("cannot check where %s points: %v", f.Head, err)
	case sha != f.HeadSHA:
		rc.block("%s has moved from %s to %s since the draft was made; ask for a new draft", f.Head, short(f.HeadSHA), short(sha))
	default:
		rc.line("%s still points at %s.", f.Head, short(sha))
	}

	cmp, err := rv.fc.Compare(ctx, repo.FullName, f.Base, f.HeadSHA)
	if err != nil {
		rc.block("cannot read the changes from %s to %s: %v", f.Base, short(f.HeadSHA), err)
		return
	}
	if cmp.TotalCommits == 0 {
		rc.block("the head has no commits that %s lacks; GitHub would refuse the pull request", f.Base)
	}
	adds, dels := 0, 0
	for _, fl := range cmp.Files {
		adds, dels = adds+fl.Additions, dels+fl.Deletions
	}
	summary := fmt.Sprintf("Changes: %s, %s, +%d -%d",
		plural(cmp.TotalCommits, "commit", "commits"), plural(len(cmp.Files), "file", "files"), adds, dels)
	if cmp.BehindBy > 0 {
		summary += fmt.Sprintf("; the head is %s behind %s", plural(cmp.BehindBy, "commit", "commits"), f.Base)
	}
	rc.line("%s", summary)

	rc.line("Commits:")
	for _, c := range cmp.Commits {
		author := c.Commit.Author.Name
		if c.Author != nil {
			author = c.Author.Login
		}
		subject, _, _ := strings.Cut(c.Commit.Message, "\n")
		rc.line("  %s %s  %s", short(c.SHA), author, subject)
	}
	if len(cmp.Commits) < cmp.TotalCommits {
		rc.flag("GitHub lists %d of the %d commits; page the diff or read the rest on GitHub", len(cmp.Commits), cmp.TotalCommits)
	}

	rc.line("Files:")
	var build []string
	for _, fl := range cmp.Files {
		mark, name := "  ", fl.Filename
		if fl.PreviousFilename != "" {
			name = fl.PreviousFilename + " -> " + fl.Filename
		}
		if buildFile(fl.Filename) || (fl.PreviousFilename != "" && buildFile(fl.PreviousFilename)) {
			mark = "! "
			build = append(build, fl.Filename)
		}
		rc.line("  %s%-10s+%d -%d  %s", mark, fl.Status, fl.Additions, fl.Deletions, name)
	}
	if len(cmp.Files) >= compareFileCap {
		rc.flag("GitHub lists at most %d files; this pull request may change more", compareFileCap)
	}
	if len(build) > 0 {
		rc.flag("changes CI or build files: %s", strings.Join(build, ", "))
	}
	rc.Diff = func(ctx context.Context) (string, error) {
		return rv.fc.CompareDiff(ctx, repo.FullName, f.Base, f.HeadSHA)
	}
}
