package main

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/Sriram-PR/ghv/internal/draft"
)

const headSHA = "f53d3e85a0c4b8a6e5d1c2b3a4f5e6d7c8b9a0f1"

func prCreateRoutes(t *testing.T) *routes {
	return newRoutes(t, map[string]string{
		"/repos/o/r":                    `{"full_name":"o/r","default_branch":"main"}`,
		"/repos/me/r/branches/fix%2Fx":  `{"name":"fix/x","commit":{"sha":"` + headSHA + `"}}`,
		"/repos/o/r/branches/same-repo": `{"name":"same-repo","commit":{"sha":"` + headSHA + `"}}`,
		"/repos/me/r/branches/bad-sha":  `{"name":"bad-sha","commit":{"sha":"HEAD"}}`,
	})
}

func TestPRCreate(t *testing.T) {
	rt := prCreateRoutes(t)
	e, _, _ := testEnv(t, rt)
	writeFile(t, filepath.Join(e.dir, "b.md"), "Fixes the late anchor.\n")
	r := runWith(t, registry, e, "pr", "create", "-R", "o/r", "--head", "me:fix/x", "--title", "fix: late anchor", "--body-file", "b.md")
	if r.code != 0 || !draftedRE.MatchString(r.stdout) {
		t.Fatalf("exit %d, stdout %q, stderr %q", r.code, r.stdout, r.stderr)
	}
	if r.stderr != "ghv: note: the draft pins me:fix/x at "+headSHA+"; ghv-apply refuses it if the branch moves\n" {
		t.Fatalf("stderr %q", r.stderr)
	}
	want := draft.PRCreateFields{Base: "main", Head: "me:fix/x", HeadSHA: headSHA, Title: "fix: late anchor", Body: "Fixes the late anchor.\n", MaintainerCanModify: true}
	if f := queued(t, e).Fields; f != want {
		t.Fatalf("fields %+v", f)
	}
	if asked := rt.uris(); !slices.Equal(asked, []string{"/repos/o/r", "/repos/me/r/branches/fix%2Fx"}) {
		t.Fatalf("asked %q", asked)
	}
}

func TestPRCreateFlags(t *testing.T) {
	rt := prCreateRoutes(t)
	e, _, _ := testEnv(t, rt)
	writeFile(t, filepath.Join(e.dir, "b.md"), "b")
	r := runWith(t, registry, e, "pr", "create", "-R", "o/r", "--head", "o:same-repo", "--base", "release/2.x", "--title", "T", "--body-file", "b.md", "--draft", "--no-maintainer-edit")
	if r.code != 0 {
		t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
	}
	want := draft.PRCreateFields{Base: "release/2.x", Head: "o:same-repo", HeadSHA: headSHA, Title: "T", Body: "b", Draft: true}
	if f := queued(t, e).Fields; f != want {
		t.Fatalf("fields %+v", f)
	}
	if asked := rt.uris(); !slices.Equal(asked, []string{"/repos/o/r/branches/same-repo"}) {
		t.Fatalf("asked %q", asked)
	}
}

func TestPRCreateLookupFailures(t *testing.T) {
	for msg, head := range map[string]string{
		`no branch "gone" in me/r; push it there first`: "me:gone",
		`GitHub sent "HEAD" as the head commit`:         "me:bad-sha",
	} {
		e, _, _ := testEnv(t, prCreateRoutes(t))
		writeFile(t, filepath.Join(e.dir, "b.md"), "b")
		r := runWith(t, registry, e, "pr", "create", "-R", "o/r", "--head", head, "--title", "T", "--body-file", "b.md")
		if r.code != 1 || !strings.Contains(r.stderr, msg) {
			t.Fatalf("%s: exit %d, stderr %q", head, r.code, r.stderr)
		}
		emptyQueue(t, e)
	}
}

func TestPRCreateUsage(t *testing.T) {
	tests := []struct {
		name string
		args []string
		msg  string
	}{
		{"no head", []string{"--title", "T", "--body-file", "b.md"}, "ghv pr create needs --head"},
		{"no title", []string{"--head", "me:fix/x", "--body-file", "b.md"}, "ghv pr create needs --title"},
		{"head without owner", []string{"--head", "fix/x", "--title", "T", "--body-file", "b.md"}, `--head "fix/x" is not OWNER:BRANCH`},
		{"bad head branch", []string{"--head", "me:a..b", "--title", "T", "--body-file", "b.md"}, `--head "me:a..b" is not OWNER:BRANCH`},
		{"bad base", []string{"--head", "me:fix/x", "--base", "a b", "--title", "T", "--body-file", "b.md"}, `--base "a b" is not a branch name`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rt := prCreateRoutes(t)
			e, _, _ := testEnv(t, rt)
			writeFile(t, filepath.Join(e.dir, "b.md"), "b")
			r := runWith(t, registry, e, append([]string{"pr", "create", "-R", "o/r"}, tt.args...)...)
			if r.code != 2 || !strings.Contains(r.stderr, tt.msg) || len(rt.uris()) != 0 {
				t.Fatalf("exit %d, stderr %q, asked %q", r.code, r.stderr, rt.uris())
			}
			emptyQueue(t, e)
		})
	}
}
