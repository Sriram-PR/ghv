package main

import (
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/Sriram-PR/ghv/internal/draft"
)

// noNetwork is a fake GitHub that fails the test on any request: drafting
// commands that need no lookup must work offline.
func noNetwork(t *testing.T) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("unexpected request %s %s", r.Method, r.URL)
		w.WriteHeader(http.StatusTeapot)
	})
}

var draftedRE = regexp.MustCompile(`^Drafted (d-[0-9a-f]{12}): ask the human to run ghv-apply\.\n$`)

// queued returns the one pending draft in e's queue.
func queued(t *testing.T, e env) draft.Draft {
	t.Helper()
	q := queueOf(t, e)
	list, err := q.List()
	if err != nil || len(list) != 1 || list[0].State != draft.Pending {
		t.Fatalf("queue %+v, err %v", list, err)
	}
	l, err := draft.ReadFile(filepath.Join(q.Dir(draft.Pending), list[0].ID+".json"))
	if err != nil {
		t.Fatal(err)
	}
	return l.Draft
}

func queueOf(t *testing.T, e env) draft.Queue {
	t.Helper()
	q, err := e.draftQueue()
	if err != nil {
		t.Fatal(err)
	}
	return q
}

func emptyQueue(t *testing.T, e env) {
	t.Helper()
	if list, err := queueOf(t, e).List(); err != nil || len(list) != 0 {
		t.Fatalf("queue %+v, err %v", list, err)
	}
}

func TestIssueCreateQueuesADraft(t *testing.T) {
	e, _, _ := testEnv(t, noNetwork(t))
	e.getenv = func(k string) string {
		if k == "XDG_DATA_HOME" {
			return "/elsewhere"
		}
		return ""
	}
	writeFile(t, filepath.Join(e.dir, "body.md"), "Steps:\n\n1. run it\n")
	r := runWith(t, registry, e, "issue", "create", "-R", "o/r", "--title", "It breaks", "--body-file", "body.md")
	m := draftedRE.FindStringSubmatch(r.stdout)
	if r.code != 0 || m == nil || r.stderr != "" {
		t.Fatalf("exit %d, stdout %q, stderr %q", r.code, r.stdout, r.stderr)
	}
	d := queued(t, e)
	want := draft.IssueCreateFields{Title: "It breaks", Body: "Steps:\n\n1. run it\n"}
	if d.ID != m[1] || d.Repo != "o/r" || d.Fields != want || !d.CreatedAt.Equal(e.now()) {
		t.Fatalf("draft %+v", d)
	}
	if _, err := os.Stat(filepath.Join(e.home, ".local", "share", "ghv", "queue", d.ID+".json")); err != nil {
		t.Fatal(err)
	}
}

func TestIssueCreateJSON(t *testing.T) {
	e, _, _ := testEnv(t, noNetwork(t))
	writeFile(t, filepath.Join(e.dir, "body.md"), "b")
	r := runWith(t, registry, e, "issue", "create", "-R", "o/r", "--title", "T", "--body-file", "body.md", "--json")
	d := queued(t, e)
	want := "{\n  \"id\": \"" + d.ID + "\",\n  \"kind\": \"issue.create\",\n  \"repo\": \"o/r\"\n}\n"
	if r.code != 0 || r.stdout != want {
		t.Fatalf("exit %d, stdout %q, want %q", r.code, r.stdout, want)
	}
}

func TestIssueCreateUsage(t *testing.T) {
	tests := []struct {
		name string
		args []string
		msg  string
	}{
		{"no -R", []string{"--title", "T", "--body-file", "body.md"}, "drafts need -R OWNER/REPO"},
		{"bad -R", []string{"-R", "o", "--title", "T", "--body-file", "body.md"}, `-R "o" is not OWNER/REPO`},
		{"no title", []string{"-R", "o/r", "--body-file", "body.md"}, "ghv issue create needs --title"},
		{"empty title", []string{"-R", "o/r", "--title", " ", "--body-file", "body.md"}, "title is empty"},
		{"no body", []string{"-R", "o/r", "--title", "T"}, "ghv issue create needs --body-file"},
		{"missing file", []string{"-R", "o/r", "--title", "T", "--body-file", "nope.md"}, "no such file"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, _, _ := testEnv(t, noNetwork(t))
			writeFile(t, filepath.Join(e.dir, "body.md"), "b")
			r := runWith(t, registry, e, append([]string{"issue", "create"}, tt.args...)...)
			if r.code != 2 || !strings.Contains(r.stderr, tt.msg) || r.stdout != "" {
				t.Fatalf("exit %d, stdout %q, stderr %q", r.code, r.stdout, r.stderr)
			}
			emptyQueue(t, e)
		})
	}
}

func TestBodyFromStdinWithCRLF(t *testing.T) {
	e, _, _ := testEnv(t, noNetwork(t))
	e.stdin = strings.NewReader("line 1\r\nline 2\r\n")
	r := runWith(t, registry, e, "issue", "create", "-R", "o/r", "--title", "T", "--body-file", "-")
	if r.code != 0 || r.stderr != "ghv: note: - has CRLF line endings; the draft uses LF\n" {
		t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
	}
	if f := queued(t, e).Fields.(draft.IssueCreateFields); f.Body != "line 1\nline 2\n" {
		t.Fatalf("body %q", f.Body)
	}
}

func TestDraftRefusesWhatGhvApplyRejects(t *testing.T) {
	for name, body := range map[string]string{
		"escape":       "red \x1b[31mtext\n",
		"lone CR":      "a\rb",
		"invalid utf8": "a\xffb",
	} {
		t.Run(name, func(t *testing.T) {
			e, _, _ := testEnv(t, noNetwork(t))
			writeFile(t, filepath.Join(e.dir, "body.md"), body)
			r := runWith(t, registry, e, "issue", "create", "-R", "o/r", "--title", "T", "--body-file", "body.md")
			if r.code != 2 || !strings.Contains(r.stderr, "ghv-apply would refuse this draft: body:1:") {
				t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
			}
			emptyQueue(t, e)
		})
	}
}

func TestDraftRefusalListsAtMostFive(t *testing.T) {
	e, _, _ := testEnv(t, noNetwork(t))
	writeFile(t, filepath.Join(e.dir, "body.md"), strings.Repeat("a\x01\n", 7))
	r := runWith(t, registry, e, "issue", "create", "-R", "o/r", "--title", "T", "--body-file", "body.md")
	if r.code != 2 || strings.Count(r.stderr, "body:") != 5 || !strings.Contains(r.stderr, "; and 2 more") {
		t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
	}
}

func TestDraftWarnsAboutRiskFlags(t *testing.T) {
	e, _, _ := testEnv(t, noNetwork(t))
	writeFile(t, filepath.Join(e.dir, "body.md"), "cc @claude\nfixes #12\n")
	r := runWith(t, registry, e, "issue", "create", "-R", "o/r", "--title", "T", "--body-file", "body.md", "--json")
	if r.code != 0 || !strings.HasPrefix(r.stdout, "{") {
		t.Fatalf("exit %d, stdout %q", r.code, r.stdout)
	}
	for _, s := range []string{"ghv: warning: body:1:4: bot-mention @claude", "ghv: warning: body:2:1: closing-keyword fixes #12"} {
		if !strings.Contains(r.stderr, s) {
			t.Fatalf("missing %q in stderr:\n%s", s, r.stderr)
		}
	}
	queued(t, e)
}

func TestBodySizeLimits(t *testing.T) {
	e, _, _ := testEnv(t, noNetwork(t))
	writeFile(t, filepath.Join(e.dir, "long.md"), strings.Repeat("a", 65537))
	r := runWith(t, registry, e, "issue", "create", "-R", "o/r", "--title", "T", "--body-file", "long.md")
	if r.code != 0 || !strings.Contains(r.stderr, "ghv: warning: long.md is 65537 characters; GitHub refuses bodies over 65536\n") {
		t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
	}
	writeFile(t, filepath.Join(e.dir, "huge.md"), strings.Repeat("a", draft.MaxSize+1))
	r = runWith(t, registry, e, "issue", "create", "-R", "o/r", "--title", "T", "--body-file", "huge.md")
	if r.code != 2 || !strings.Contains(r.stderr, "huge.md is over 1048576 bytes") {
		t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
	}
	writeFile(t, filepath.Join(e.dir, "big.md"), strings.Repeat(`"`, draft.MaxSize/2+1))
	r = runWith(t, registry, e, "issue", "create", "-R", "o/r", "--title", "T", "--body-file", "big.md")
	if r.code != 2 || !strings.Contains(r.stderr, "draft is larger than 1048576 bytes") {
		t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
	}
}

func TestDraftingNeedsThePasswdHome(t *testing.T) {
	for name, passwd := range map[string]string{
		"other home": t.TempDir(),
		"no entry":   "",
	} {
		t.Run(name, func(t *testing.T) {
			e, _, _ := testEnv(t, noNetwork(t))
			e.passwdHome = passwd
			writeFile(t, filepath.Join(e.dir, "b.md"), "body\n")
			for _, args := range [][]string{
				{"issue", "create", "-R", "o/r", "--title", "T", "--body-file", "b.md"},
				{"drafts"},
			} {
				r := runWith(t, registry, e, args...)
				if r.code != 1 || r.stdout != "" || !strings.Contains(r.stderr, "passwd") {
					t.Fatalf("%q: exit %d, stdout %q, stderr %q", args, r.code, r.stdout, r.stderr)
				}
			}
			for _, h := range []string{e.home, passwd} {
				if _, err := os.Stat(filepath.Join(h, ".local")); h != "" && !os.IsNotExist(err) {
					t.Fatalf("%s/.local: %v", h, err)
				}
			}
		})
	}
}

func TestDraftingFollowsASymlinkedHome(t *testing.T) {
	e, _, _ := testEnv(t, noNetwork(t))
	link := filepath.Join(t.TempDir(), "home")
	if err := os.Symlink(e.passwdHome, link); err != nil {
		t.Fatal(err)
	}
	e.home = link
	writeFile(t, filepath.Join(e.dir, "b.md"), "body\n")
	r := runWith(t, registry, e, "issue", "create", "-R", "o/r", "--title", "T", "--body-file", "b.md")
	if r.code != 0 || !draftedRE.MatchString(r.stdout) {
		t.Fatalf("exit %d, stdout %q, stderr %q", r.code, r.stdout, r.stderr)
	}
	queued(t, e)
}
