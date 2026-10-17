package main

import (
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const repoBody = `{"id":212613049,"full_name":"cli/cli","description":"GitHub’s official command line tool \u001b]8;;https://evil\u0007x","private":false,"fork":true,
"parent":{"full_name":"up/cli"},"archived":false,"default_branch":"trunk","stargazers_count":38000,"forks_count":6000,
"open_issues_count":700,"license":{"spdx_id":"MIT","name":"MIT License"},"topics":["cli","git\u202e"],"homepage":"https://cli.github.com",
"created_at":"2019-10-03T19:08:39Z","pushed_at":"2026-10-17T22:01:05Z","html_url":"https://github.com/cli/cli","owner":{"login":"cli"}}`

func repoServer(t *testing.T, paths *[]string) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*paths = append(*paths, r.URL.Path)
		if r.URL.Path != "/repos/cli/cli" {
			w.WriteHeader(404)
			io.WriteString(w, `{"message":"Not Found"}`)
			return
		}
		io.WriteString(w, repoBody)
	})
}

func TestRepoView(t *testing.T) {
	var paths []string
	e, _, _ := testEnv(t, repoServer(t, &paths))
	r := runWith(t, registry, e, "repo", "view", "cli/cli")
	want := "cli/cli\n" +
		"GitHub’s official command line tool <U+001B>]8;;https://evil<U+0007>x\n\n" +
		"fork of      up/cli\n" +
		"default      trunk\n" +
		"stars        38000\n" +
		"forks        6000\n" +
		"open issues  700\n" +
		"license      MIT\n" +
		"topics       cli, git<U+202E>\n" +
		"homepage     https://cli.github.com\n" +
		"created      2019-10-03T19:08:39Z\n" +
		"pushed       2026-10-17T22:01:05Z\n" +
		"url          https://github.com/cli/cli\n"
	if r.code != 0 || r.stdout != want {
		t.Fatalf("exit %d, stderr %q, stdout:\n%s\nwant:\n%s", r.code, r.stderr, r.stdout, want)
	}
}

func TestRepoViewJSONGolden(t *testing.T) {
	var paths []string
	e, _, _ := testEnv(t, repoServer(t, &paths))
	r := runWith(t, registry, e, "repo", "view", "cli/cli", "--json")
	golden := filepath.Join("testdata", "repo-view.json")
	if os.Getenv("GHV_UPDATE_GOLDEN") == "1" {
		os.WriteFile(golden, []byte(r.stdout), 0o644)
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatal(err)
	}
	if r.code != 0 || r.stdout != string(want) {
		t.Fatalf("exit %d, stdout:\n%s\nwant:\n%s", r.code, r.stdout, want)
	}
}

func TestRepoViewInfersRepo(t *testing.T) {
	var paths []string
	e, _, _ := testEnv(t, repoServer(t, &paths))
	writeFile(t, filepath.Join(e.dir, ".git", "config"), "[remote \"origin\"]\n\turl = git@github.com:cli/cli.git\n")
	r := runWith(t, registry, e, "repo", "view")
	if r.code != 0 || !strings.HasPrefix(r.stdout, "cli/cli\n") || len(paths) != 1 {
		t.Fatalf("exit %d, stdout %q, stderr %q", r.code, r.stdout, r.stderr)
	}
}

func TestRepoViewErrors(t *testing.T) {
	tests := []struct {
		args   []string
		exit   int
		stderr string
	}{
		{[]string{"repo", "view", "not-a-repo"}, 2, `"not-a-repo" is not OWNER/REPO`},
		{[]string{"repo", "view", "../x"}, 2, "is not OWNER/REPO"},
		{[]string{"repo", "view"}, 2, "this is not a git checkout"},
		{[]string{"repo", "view", "o/missing"}, 1, "not found, or the token cannot see it"},
	}
	for _, tt := range tests {
		t.Run(strings.Join(tt.args, " "), func(t *testing.T) {
			var paths []string
			e, _, _ := testEnv(t, repoServer(t, &paths))
			r := runWith(t, registry, e, tt.args...)
			if r.code != tt.exit || r.stdout != "" || !strings.Contains(r.stderr, tt.stderr) {
				t.Fatalf("exit %d, stdout %q, stderr %q", r.code, r.stdout, r.stderr)
			}
		})
	}
}
