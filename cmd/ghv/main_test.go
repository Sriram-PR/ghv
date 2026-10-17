package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Sriram-PR/ghv/internal/read"
	"github.com/Sriram-PR/ghv/internal/read/readtest"
)

type result struct {
	code           int
	stdout, stderr string
}

// testEnv is an environment with an empty home and the fake GitHub h.
func testEnv(t *testing.T, h http.Handler) (env, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	var out, errb bytes.Buffer
	home := t.TempDir()
	e := env{
		stdout: &out,
		stderr: &errb,
		home:   home,
		getenv: func(string) string { return "" },
		dir:    home,
		now:    func() time.Time { return time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC) },
	}
	if h != nil {
		e.transport = readtest.Transport{Handler: h}
	}
	return e, &out, &errb
}

// runWith runs ghv with args in e, clearing e's output first, so one env
// can serve several runs.
func runWith(t *testing.T, cmds []*command, e env, args ...string) result {
	t.Helper()
	e.stdout.(*bytes.Buffer).Reset()
	e.stderr.(*bytes.Buffer).Reset()
	code := run(context.Background(), cmds, args, e)
	return result{code, e.stdout.(*bytes.Buffer).String(), e.stderr.(*bytes.Buffer).String()}
}

func setToken(t *testing.T, e env, tok string) {
	t.Helper()
	p := e.tokenPath()
	os.MkdirAll(filepath.Dir(p), 0o700)
	if err := os.WriteFile(p, []byte(tok+"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
}

var failCmds = []*command{
	{path: "fail", args: []string{"KIND"}, run: func(ctx context.Context, a *app) error {
		switch a.inv.args[0] {
		case "usage":
			return usageError{"bad usage"}
		case "api404":
			return &read.APIError{Status: 404, Code: read.CodeNotFound, Message: "not found, or the token cannot see it"}
		case "api401":
			return &read.APIError{Status: 401, Code: read.CodeTokenInvalid, Message: "the read token is invalid or expired"}
		case "anon":
			return &read.APIError{Status: 401, Code: read.CodeTokenRequired, Message: "this needs a read token"}
		case "scopes":
			return &read.TokenError{Info: read.TokenInfo{Scopes: []string{"repo"}}}
		case "readonly":
			return read.ErrReadOnly
		case "escape":
			return errors.New("evil\x1b]52;c;x\x07\nline")
		case "client":
			_, err := a.read()
			return err
		}
		return nil
	}},
}

func TestExitCodesAndErrors(t *testing.T) {
	tests := []struct {
		kind, code string
		exit       int
		stderr     string
	}{
		{"ok", "", 0, ""},
		{"usage", "usage", 2, "ghv: bad usage\nRun 'ghv fail --help' for usage.\n"},
		{"api404", "not_found", 1, "ghv: not found, or the token cannot see it\n"},
		{"api401", "token_invalid", 3, "ghv: the read token is invalid or expired\n"},
		{"scopes", "token_refused", 3, ""},
		{"readonly", "read_only", 1, "ghv: ghv is read-only; draft this with 'ghv <command>'\n"},
		{"escape", "error", 1, "ghv: evil<U+001B>]52;c;x<U+0007><U+000A>line\n"},
	}
	for _, tt := range tests {
		t.Run(tt.kind, func(t *testing.T) {
			e, _, _ := testEnv(t, nil)
			r := runWith(t, failCmds, e, "fail", tt.kind)
			if r.code != tt.exit || r.stdout != "" || (tt.stderr != "" && r.stderr != tt.stderr) {
				t.Fatalf("exit %d, stdout %q, stderr %q", r.code, r.stdout, r.stderr)
			}
			if tt.code == "" {
				return
			}
			e, _, _ = testEnv(t, nil)
			r = runWith(t, failCmds, e, "fail", tt.kind, "--json")
			var got struct {
				Error struct{ Code, Message string } `json:"error"`
			}
			if err := json.Unmarshal([]byte(r.stderr), &got); err != nil || got.Error.Code != tt.code || got.Error.Message == "" {
				t.Fatalf("--json stderr %q: %v", r.stderr, err)
			}
			if r.code != tt.exit || r.stdout != "" {
				t.Fatalf("--json exit %d, stdout %q", r.code, r.stdout)
			}
		})
	}
}

func TestTokenRequiredNamesTheTokenFile(t *testing.T) {
	e, _, _ := testEnv(t, nil)
	r := runWith(t, failCmds, e, "fail", "anon")
	if r.code != 3 || !strings.Contains(r.stderr, filepath.Join(e.home, ".config", "ghv", "read-token")) {
		t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
	}
}

func TestUsageErrorAsJSON(t *testing.T) {
	e, _, _ := testEnv(t, nil)
	r := runWith(t, failCmds, e, "nope", "--json")
	if r.code != 2 || !strings.Contains(r.stderr, `"code": "usage"`) {
		t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
	}
}

func TestHelpGoesToStdout(t *testing.T) {
	e, _, _ := testEnv(t, nil)
	r := runWith(t, failCmds, e, "--help")
	if r.code != 0 || !strings.Contains(r.stdout, "Usage: ghv") || r.stderr != "" {
		t.Fatalf("exit %d, stdout %q, stderr %q", r.code, r.stdout, r.stderr)
	}
	r = runWith(t, failCmds, e)
	if r.code != 2 || !strings.Contains(r.stderr, "Usage: ghv") {
		t.Fatalf("no args: exit %d, stderr %q", r.code, r.stderr)
	}
}

func TestBadTokenFileIsExit3(t *testing.T) {
	e, _, _ := testEnv(t, nil)
	setToken(t, e, "ghp_x")
	os.Chmod(e.tokenPath(), 0o644)
	r := runWith(t, failCmds, e, "fail", "client")
	if r.code != 3 || !strings.Contains(r.stderr, "chmod 600") {
		t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
	}
}

func TestPaths(t *testing.T) {
	e, _, _ := testEnv(t, nil)
	if e.tokenPath() != filepath.Join(e.home, ".config", "ghv", "read-token") || e.cacheDir() != filepath.Join(e.home, ".cache", "ghv") {
		t.Fatalf("%s %s", e.tokenPath(), e.cacheDir())
	}
	e.getenv = func(k string) string {
		return map[string]string{"XDG_CONFIG_HOME": "/x/config", "XDG_CACHE_HOME": "relative"}[k]
	}
	if e.tokenPath() != "/x/config/ghv/read-token" || e.cacheDir() != filepath.Join(e.home, ".cache", "ghv") {
		t.Fatalf("%s %s", e.tokenPath(), e.cacheDir())
	}
}
