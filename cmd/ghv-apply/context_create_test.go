package main

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/Sriram-PR/ghv/internal/draft"
)

const (
	headSHA  = "0123456789abcdef0123456789abcdef01234567"
	movedSHA = "fedcba9876543210fedcba9876543210fedcba98"
	branchAt = "/repos/Sriram-PR/r/branches/fix%2Fx"
	compare  = "/repos/o/r/compare/main..." + headSHA
)

func createDraft(head string) draft.Draft {
	return draft.Draft{Repo: "o/r", Fields: draft.PRCreateFields{
		Base: "main", Head: head, HeadSHA: headSHA, Title: "t", Body: "b", MaintainerCanModify: true,
	}}
}

func createRoutes() map[string]apiReply {
	routes := baseRoutes()
	routes[branchAt] = ok(`{"commit":{"sha":"` + headSHA + `"}}`)
	routes[compare] = ok(`{"status":"ahead","ahead_by":2,"behind_by":3,"total_commits":2,
		"commits":[
			{"sha":"` + headSHA + `","commit":{"author":{"name":"S"},"message":"fix: x\n\nwhy"},"author":{"login":"Sriram-PR"}},
			{"sha":"` + movedSHA + `","commit":{"author":{"name":"Some One"},"message":"docs: y"},"author":null}],
		"files":[
			{"filename":".github/workflows/ci.yml","status":"modified","additions":2,"deletions":1},
			{"filename":"src/a.go","status":"added","additions":10,"deletions":0},
			{"filename":"b.go","previous_filename":"old.go","status":"renamed","additions":1,"deletions":0}]}`)
	return routes
}

func TestContextPRCreate(t *testing.T) {
	rv, f := newReviewer(createRoutes())
	rc := rv.context(context.Background(), createDraft("Sriram-PR:fix/x"))
	has(t, "lines", rc.Lines,
		"Pull request from Sriram-PR:fix/x at 0123456789ab into o/r:main",
		"Sriram-PR:fix/x still points at 0123456789ab.",
		"Changes: 2 commits, 3 files, +13 -1; the head is 3 commits behind main",
		"Commits:\n  0123456789ab Sriram-PR  fix: x\n  fedcba987654 Some One  docs: y",
		"Files:\n  ! modified  +2 -1  .github/workflows/ci.yml\n    added     +10 -0  src/a.go\n    renamed   +1 -0  old.go -> b.go")
	has(t, "flags", rc.Flags,
		"maintainers of o/r can push to Sriram-PR:fix/x (maintainer_can_modify)",
		"changes CI or build files: .github/workflows/ci.yml")
	if len(rc.Blocks) != 0 || len(rc.Flags) != 3 {
		t.Fatalf("flags %q, blocks %q", rc.Flags, rc.Blocks)
	}
	if rc.Diff == nil {
		t.Fatal("no diff")
	}
	rc.Diff(context.Background())
	last := f.reqs[len(f.reqs)-1]
	if last.URL.RequestURI() != compare || last.Header.Get("Accept") != "application/vnd.github.diff" {
		t.Fatalf("diff request %s, Accept %q", last.URL, last.Header.Get("Accept"))
	}
}

func TestContextPRCreateRefusals(t *testing.T) {
	routes := createRoutes()
	routes[branchAt] = ok(`{"commit":{"sha":"` + movedSHA + `"}}`)
	rc := contextOf(t, routes, createDraft("Sriram-PR:fix/x"))
	has(t, "blocks", rc.Blocks, "Sriram-PR:fix/x has moved from 0123456789ab to fedcba987654 since the draft was made; ask for a new draft")

	routes = createRoutes()
	delete(routes, branchAt)
	rc = contextOf(t, routes, createDraft("Sriram-PR:fix/x"))
	has(t, "blocks", rc.Blocks, "Sriram-PR/r has no branch fix/x")

	routes = createRoutes()
	delete(routes, compare)
	rc = contextOf(t, routes, createDraft("Sriram-PR:fix/x"))
	has(t, "blocks", rc.Blocks, "cannot read the changes from main to 0123456789ab: GET "+compare+": not found")

	routes = createRoutes()
	routes[compare] = ok(`{"status":"identical","total_commits":0,"commits":[],"files":[]}`)
	rc = contextOf(t, routes, createDraft("Sriram-PR:fix/x"))
	has(t, "blocks", rc.Blocks, "the head has no commits that main lacks; GitHub would refuse the pull request")
}

func TestContextPRCreateFlags(t *testing.T) {
	routes := createRoutes()
	routes["/repos/me/r/branches/fix%2Fx"] = routes[branchAt]
	files := make([]string, 300)
	for i := range files {
		files[i] = fmt.Sprintf(`{"filename":"f%d","status":"added","additions":1}`, i)
	}
	routes[compare] = ok(`{"total_commits":300,"commits":[{"sha":"` + headSHA + `","commit":{"message":"m"}}],"files":[` + strings.Join(files, ",") + `]}`)
	rc := contextOf(t, routes, createDraft("me:fix/x"))
	has(t, "flags", rc.Flags,
		"the head branch belongs to me, not you",
		"GitHub lists 1 of the 300 commits; page the diff or read the rest on GitHub",
		"GitHub lists at most 300 files; this pull request may change more")
}

func TestBuildFile(t *testing.T) {
	for name, want := range map[string]bool{
		".github/workflows/ci.yml": true, ".github/CODEOWNERS": true, "Makefile": true, "sub/Dockerfile.dev": true,
		"go.mod": true, "pkg/package.json": true, "rules.bzl": true, "tools/x.mk": true, "action.yml": true,
		"src/main.go": false, "README.md": false, "docs/github/x.md": false, "go.sum": false,
	} {
		if buildFile(name) != want {
			t.Errorf("buildFile(%q) = %v", name, !want)
		}
	}
}
