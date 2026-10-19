package main

import (
	"encoding/json"
	"errors"
	"net/http"
	"os"
	"os/user"
	"path/filepath"
	"strings"
	"testing"
)

// doctorEnv is testEnv for doctor: user agent (uid 1001) on Linux with
// ptrace_scope 1, a classic read token GitHub reports with no scopes, no
// ghv-apply to find, and a human sriram (uid 1000) whose home this user
// cannot write.
func doctorTestEnv(t *testing.T, vars map[string]string) (env, *int) {
	t.Helper()
	calls := 0
	e, _, _ := testEnv(t, rateServer(t, http.Header{"X-Oauth-Scopes": {""}}, &calls))
	proc := t.TempDir()
	os.MkdirAll(filepath.Join(proc, "sys/kernel/yama"), 0o755)
	os.WriteFile(filepath.Join(proc, "sys/kernel/yama/ptrace_scope"), []byte("1\n"), 0o644)
	human := t.TempDir()
	os.Chmod(human, 0o555)
	t.Cleanup(func() { os.Chmod(human, 0o755) })
	e.goos, e.uid, e.pid, e.username, e.proc = "linux", 1001, 4242, "agent", proc
	e.getenv = func(k string) string { return vars[k] }
	e.lookupUser = func(name string) (*user.User, error) {
		if name != "sriram" {
			return nil, user.UnknownUserError(name)
		}
		return &user.User{Uid: "1000", Username: "sriram", HomeDir: human}, nil
	}
	e.confirm = func(string) (bool, error) { return false, errors.New("no terminal in this test") }
	setToken(t, e, "ghp_"+strings.Repeat("x", 36))
	return e, &calls
}

func TestDoctorContained(t *testing.T) {
	e, calls := doctorTestEnv(t, map[string]string{"PATH": "/nonexistent"})
	r := runWith(t, registry, e, "doctor", "--human-user", "sriram")
	want := "What this process can reach, running as agent (uid 1001); the human is sriram:\n\n" +
		"INFO    ghv-apply  no ghv-apply found to check\n" +
		"\nVerdict: Contained (no HIGH findings).\n"
	if r.code != 0 || r.stdout != want || r.stderr != "" || *calls != 1 {
		t.Fatalf("exit %d, %d calls, stderr %q, stdout:\n%s", r.code, *calls, r.stderr, r.stdout)
	}
}

func TestDoctorHigh(t *testing.T) {
	e, _ := doctorTestEnv(t, map[string]string{"GH_TOKEN": "ghp_" + "y"})
	r := runWith(t, registry, e, "doctor")
	if r.code != 4 || r.stderr != "ghv: 2 HIGH findings\n" ||
		!strings.Contains(r.stdout, "HIGH    env-tokens  GH_TOKEN is set in this process's environment\n") ||
		!strings.Contains(r.stdout, "HIGH    same-user   no --human-user given") ||
		!strings.HasSuffix(r.stdout, "Verdict: Uncontained (2 HIGH findings). The agent can act as you on GitHub by these routes; fix them, or run it as its own user.\n") {
		t.Fatalf("exit %d, stderr %q, stdout:\n%s", r.code, r.stderr, r.stdout)
	}
	r = runWith(t, registry, e, "doctor", "--json")
	var got struct {
		User     string `json:"user"`
		Findings []struct{ Check, Severity, Detail string }
		Verdict  string `json:"verdict"`
	}
	if err := json.Unmarshal([]byte(r.stdout), &got); err != nil || r.code != 4 || got.Verdict != "Uncontained" || got.User != "agent" ||
		len(got.Findings) != 3 || got.Findings[0].Severity != "HIGH" || !strings.Contains(r.stderr, `"code": "doctor_high"`) {
		t.Fatalf("exit %d, stderr %q, json %s: %v", r.code, r.stderr, r.stdout, err)
	}
}

func TestDoctorUnknownHuman(t *testing.T) {
	e, calls := doctorTestEnv(t, nil)
	r := runWith(t, registry, e, "doctor", "--human-user", "nobody-here")
	if r.code != 2 || !strings.HasPrefix(r.stderr, "ghv: --human-user nobody-here: user: unknown user nobody-here\n") || *calls != 0 {
		t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
	}
}

func TestDoctorAckNeedsATerminal(t *testing.T) {
	e, _ := doctorTestEnv(t, nil)
	setToken(t, e, "github_pat_"+strings.Repeat("z", 40))
	r := runWith(t, registry, e, "doctor", "--human-user", "sriram", "--ack-fine-grained")
	if r.code != 4 || !strings.Contains(r.stdout, "cannot ask: no terminal in this test\n") {
		t.Fatalf("exit %d, stdout:\n%s", r.code, r.stdout)
	}
	if _, err := os.Stat(e.tokenPath() + ".ack"); err == nil {
		t.Fatal("nothing is recorded without a yes")
	}
}

func TestApplyPaths(t *testing.T) {
	e, _ := doctorTestEnv(t, nil)
	bin, system := t.TempDir(), t.TempDir()
	for _, d := range []string{bin, system} {
		os.WriteFile(filepath.Join(d, "ghv-apply"), nil, 0o755)
	}
	os.Mkdir(filepath.Join(e.home, "ghv-apply"), 0o755)
	e.systemBin = system
	e.getenv = func(k string) string {
		return map[string]string{"PATH": "relative:" + e.home + ":" + bin + ":" + bin}[k]
	}
	got := (&app{env: e}).applyPaths()
	want := []string{filepath.Join(bin, "ghv-apply"), filepath.Join(system, "ghv-apply")}
	if strings.Join(got, " ") != strings.Join(want, " ") {
		t.Fatalf("%q", got)
	}
}
