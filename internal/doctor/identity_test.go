package doctor

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeProc makes a /proc with the given files, relative to it.
func fakeProc(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, data := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(data), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func details(fs []Finding) string {
	var b strings.Builder
	for _, f := range fs {
		b.WriteString(f.Severity.String() + " " + f.Detail + "\n")
	}
	return b.String()
}

func TestSameUser(t *testing.T) {
	for _, c := range []struct {
		name  string
		human *Human
		want  string
	}{
		{"no flag", nil, "HIGH no --human-user given, so this counts as the human's own user, agent: the agent can do anything you can\n"},
		{"same uid", &Human{Name: "sriram", UID: 1001}, "HIGH runs as agent, the human's user: the agent can do anything you can\n"},
		{"separate", &Human{Name: "sriram", UID: 1000}, ""},
	} {
		got := details(sameUser(context.Background(), &Env{User: "agent", UID: 1001, Human: c.human}))
		if got != c.want {
			t.Errorf("%s: %q", c.name, got)
		}
	}
}

func TestPtraceScope(t *testing.T) {
	scope := "sys/kernel/yama/ptrace_scope"
	for _, c := range []struct {
		name, goos string
		files      map[string]string
		want       string
	}{
		{"0", "linux", map[string]string{scope: "0\n"}, "HIGH kernel.yama.ptrace_scope is 0: a process can trace any other process of its user, ghv-apply's passphrase prompt included\n"},
		{"1", "linux", map[string]string{scope: "1\n"}, ""},
		{"3", "linux", map[string]string{scope: "3\n"}, ""},
		{"no Yama", "linux", nil, "HIGH the kernel has no Yama: a process can trace any other process of its user\n"},
		{"macOS", "darwin", nil, ""},
	} {
		got := details(ptraceScope(context.Background(), &Env{GOOS: c.goos, Proc: fakeProc(t, c.files)}))
		if got != c.want {
			t.Errorf("%s: %q", c.name, got)
		}
	}
}
