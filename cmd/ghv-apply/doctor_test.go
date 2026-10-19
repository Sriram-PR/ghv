package main

import (
	"context"
	"encoding/json"
	"net/http"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Sriram-PR/ghv/internal/harden"
)

// doctorEnv is testEnv for doctor: user agent (uid 1001) on Linux with
// ptrace_scope 1, started by pid 77 with the given environment, a classic
// read token GitHub reports with no scopes, and a human sriram (uid 1000)
// whose home this user cannot write.
func doctorTestEnv(t *testing.T, parent string) (env, *fakeAPI) {
	t.Helper()
	e := testEnv(t)
	f := &fakeAPI{routes: map[string]apiReply{"/rate_limit": {status: 200, header: http.Header{"X-Oauth-Scopes": {""}}, body: "{}"}}}
	e.client = harden.Client(f)
	proc := t.TempDir()
	for name, data := range map[string]string{"sys/kernel/yama/ptrace_scope": "1\n", "77/environ": parent} {
		os.MkdirAll(filepath.Dir(filepath.Join(proc, name)), 0o755)
		os.WriteFile(filepath.Join(proc, name), []byte(data), 0o644)
	}
	human := t.TempDir()
	os.Chmod(human, 0o555)
	t.Cleanup(func() { os.Chmod(human, 0o755) })
	e.goos, e.uid, e.pid, e.ppid, e.username, e.proc = "linux", 1001, 4242, 77, "agent", proc
	e.lookupUser = func(name string) (*user.User, error) {
		if name != "sriram" {
			return nil, user.UnknownUserError(name)
		}
		return &user.User{Uid: "1000", Username: "sriram", HomeDir: human}, nil
	}
	tok := filepath.Join(e.home, ".config/ghv/read-token")
	os.MkdirAll(filepath.Dir(tok), 0o700)
	os.WriteFile(tok, []byte("ghp_"+strings.Repeat("x", 36)+"\n"), 0o600)
	return e, f
}

func TestDoctorContained(t *testing.T) {
	e, f := doctorTestEnv(t, "PATH=/bin\x00HTTPS_PROXY=http://proxy\x00")
	r := runWith(e, "doctor", "--human-user", "sriram")
	want := "What this process can reach, running as agent (uid 1001); the human is sriram:\n\n" +
		"INFO    proxy      HTTPS_PROXY set in the environment of ghv-apply's parent, pid 77: ghv-apply ignores it; other tools send their traffic through it\n" +
		"INFO    ghv-apply  no ghv-apply found to check\n" +
		"\nVerdict: Contained (no HIGH findings).\n"
	if r.code != 0 || r.stdout != want || r.stderr != "" {
		t.Fatalf("exit %d, stderr %q, stdout:\n%s", r.code, r.stderr, r.stdout)
	}
	if len(f.reqs) != 1 || f.reqs[0].Method != "GET" || f.reqs[0].Header.Get("Authorization") != "Bearer ghp_"+strings.Repeat("x", 36) {
		t.Fatalf("%d requests", len(f.reqs))
	}
}

func TestDoctorHigh(t *testing.T) {
	e, _ := doctorTestEnv(t, "GH_TOKEN=ghp_y\x00")
	r := runWith(e, "doctor", "--json")
	var got struct {
		Findings []struct{ Check, Severity, Detail string }
		Verdict  string
	}
	if err := json.Unmarshal([]byte(r.stdout), &got); err != nil || r.code != 4 || r.stderr != "ghv-apply: 2 HIGH findings\n" ||
		got.Verdict != "Uncontained" || len(got.Findings) != 3 ||
		got.Findings[0].Detail != "GH_TOKEN is set in the environment of ghv-apply's parent, pid 77" {
		t.Fatalf("exit %d, stderr %q, stdout %s: %v", r.code, r.stderr, r.stdout, err)
	}
}

func TestDoctorFlags(t *testing.T) {
	e, _ := doctorTestEnv(t, "")
	for _, c := range []struct {
		args []string
		want string
	}{
		{[]string{"doctor", "--human-user"}, "ghv-apply: doctor --human-user needs a value\n"},
		{[]string{"doctor", "--bogus"}, "ghv-apply: doctor takes only --ack-fine-grained, --json, --human-user VALUE\n"},
	} {
		if r := runWith(e, c.args...); r.code != 2 || r.stderr != c.want {
			t.Errorf("%q: exit %d, stderr %q", c.args, r.code, r.stderr)
		}
	}
	if r := runWith(e, "doctor", "--human-user", "nobody-here"); r.code != 1 || r.stderr != "ghv-apply: --human-user nobody-here: user: unknown user nobody-here\n" {
		t.Errorf("exit %d, stderr %q", r.code, r.stderr)
	}
}

func TestTokenScopesRefused(t *testing.T) {
	f := &fakeAPI{routes: map[string]apiReply{"/rate_limit": {status: 401, body: `{"message":"Bad credentials"}`}}}
	if _, _, err := tokenScopes(context.Background(), harden.Client(f), "ghp_x"); err == nil || err.Error() != "GitHub answered 401" {
		t.Fatalf("err %v", err)
	}
}
