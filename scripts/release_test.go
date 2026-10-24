package scripts

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// checkout copies this working tree's tracked and unignored files into a
// new git repository, commits them, and tags the commit tag.
func checkout(t *testing.T, tag string) string {
	t.Helper()
	files, err := exec.Command("git", "-C", "..", "ls-files", "-co", "--exclude-standard", "-z").Output()
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	for _, f := range strings.Split(strings.TrimSuffix(string(files), "\x00"), "\x00") {
		b, err := os.ReadFile(filepath.Join("..", f))
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			t.Fatal(err)
		}
		fi, _ := os.Stat(filepath.Join("..", f))
		os.MkdirAll(filepath.Join(dir, filepath.Dir(f)), 0o755)
		if err := os.WriteFile(filepath.Join(dir, f), b, fi.Mode().Perm()); err != nil {
			t.Fatal(err)
		}
	}
	git(t, dir, "init", "-q")
	git(t, dir, "add", "-A")
	git(t, dir, "-c", "user.name=t", "-c", "user.email=t@example.com", "commit", "-q", "-m", "release test")
	git(t, dir, "tag", tag)
	return dir
}

func git(t *testing.T, dir string, args ...string) {
	t.Helper()
	if out, err := exec.Command("git", append([]string{"-C", dir}, args...)...).CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

func release(dir string, args ...string) (int, string) {
	cmd := exec.Command("sh", append([]string{"scripts/release.sh"}, args...)...)
	cmd.Dir = dir
	out, err := cmd.CombinedOutput()
	if ee, ok := err.(*exec.ExitError); ok {
		return ee.ExitCode(), string(out)
	}
	if err != nil {
		return -1, err.Error()
	}
	return 0, string(out)
}

func TestReleaseRefuses(t *testing.T) {
	dir := checkout(t, "v0.1.0")
	for _, c := range []struct {
		args []string
		code int
		out  string
	}{
		{nil, 2, "usage: scripts/release.sh vX.Y.Z\n"},
		{[]string{"0.1.0"}, 2, "usage: scripts/release.sh vX.Y.Z\n"},
		{[]string{"v0.1"}, 2, "usage: scripts/release.sh vX.Y.Z\n"},
		{[]string{"v0.2.0"}, 1, "release: HEAD is not at v0.2.0\n"},
	} {
		if code, out := release(dir, c.args...); code != c.code || out != c.out {
			t.Errorf("%v: exit %d, output %q", c.args, code, out)
		}
	}
	os.WriteFile(filepath.Join(dir, "stray.txt"), []byte("x"), 0o644)
	if code, out := release(dir, "v0.1.0"); code != 1 || out != "release: the checkout has changes; commit or remove them first\n" {
		t.Errorf("dirty: exit %d, output %q", code, out)
	}
}

// The second build is a clone of the first: the same commit at another
// path.
func TestReleaseBuildsReproducibly(t *testing.T) {
	first := checkout(t, "v0.1.0")
	second := filepath.Join(t.TempDir(), "clone")
	git(t, ".", "clone", "-q", first, second)
	var sums []string
	for _, dir := range []string{first, second} {
		if code, out := release(dir, "v0.1.0"); code != 0 {
			t.Fatalf("exit %d:\n%s", code, out)
		}
		out := filepath.Join(dir, "dist", "v0.1.0")
		b, err := os.ReadFile(filepath.Join(out, "SHA256SUMS"))
		if err != nil {
			t.Fatal(err)
		}
		var names []string
		for _, l := range strings.Split(strings.TrimSuffix(string(b), "\n"), "\n") {
			names = append(names, strings.Fields(l)[1])
		}
		want := "ghv-apply_v0.1.0_darwin_amd64 ghv-apply_v0.1.0_darwin_arm64 ghv-apply_v0.1.0_linux_amd64 ghv-apply_v0.1.0_linux_arm64 " +
			"ghv_v0.1.0_darwin_amd64 ghv_v0.1.0_darwin_arm64 ghv_v0.1.0_linux_amd64 ghv_v0.1.0_linux_arm64"
		if got := strings.Join(names, " "); got != want {
			t.Fatalf("SHA256SUMS lists %s", got)
		}
		host := filepath.Join(out, "ghv_v0.1.0_"+runtime.GOOS+"_"+runtime.GOARCH)
		v, err := exec.Command(host, "version").Output()
		if err != nil || !strings.HasPrefix(string(v), "ghv v0.1.0 (go") {
			t.Fatalf("%s version: %q, %v", host, v, err)
		}
		sums = append(sums, string(b))
	}
	if sums[0] != sums[1] {
		t.Fatalf("two checkouts built different files:\n%s\n%s", sums[0], sums[1])
	}
}
