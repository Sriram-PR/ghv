package doctor

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

func TestKernelWritable(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root can write anything")
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "f")
	if err := os.WriteFile(p, nil, 0o400); err != nil {
		t.Fatal(err)
	}
	if kernelWritable(p) || !kernelWritable(dir) || kernelWritable(filepath.Join(dir, "missing")) {
		t.Fatal("file 0400 must not be writable, its directory must be")
	}
	fi, err := os.Stat("/tmp")
	if st, ok := fi.Sys().(*syscall.Stat_t); err == nil && ok && fi.Mode()&os.ModeSticky != 0 && st.Uid == 0 {
		if kernelWritable("/tmp") {
			t.Fatal("a sticky directory owned by root does not count")
		}
	}
}

// writableOnly says only the given paths can be changed.
func writableOnly(paths ...string) func(string) bool {
	return func(p string) bool {
		for _, w := range paths {
			if p == w {
				return true
			}
		}
		return false
	}
}

func TestApplyBinary(t *testing.T) {
	root := t.TempDir()
	bin := filepath.Join(root, "usr/local/bin")
	plant(t, root, map[string]string{"usr/local/bin/ghv-apply": "", "home/u/.local/bin/target": ""})
	link := filepath.Join(root, "home/u/bin/ghv-apply")
	os.MkdirAll(filepath.Dir(link), 0o700)
	if err := os.Symlink(filepath.Join(bin, "ghv-apply"), link); err != nil {
		t.Fatal(err)
	}
	for _, c := range []struct {
		name     string
		paths    []string
		writable func(string) bool
		want     string
	}{
		{"none found", nil, writableOnly(), "INFO no ghv-apply found to check\n"},
		{"root-owned", []string{filepath.Join(bin, "ghv-apply")}, writableOnly(), ""},
		{"the file", []string{filepath.Join(bin, "ghv-apply")}, writableOnly(filepath.Join(bin, "ghv-apply")),
			"HIGH " + filepath.Join(bin, "ghv-apply") + " is writable by this user: ghv-apply could be replaced\n"},
		{"a parent", []string{filepath.Join(bin, "ghv-apply")}, writableOnly(filepath.Join(root, "usr")),
			"HIGH " + filepath.Join(root, "usr") + ", a directory above " + filepath.Join(bin, "ghv-apply") + ", is writable by this user: ghv-apply could be replaced\n"},
		{"a link in a writable directory", []string{link}, writableOnly(filepath.Dir(link)),
			"HIGH " + filepath.Dir(link) + ", a directory above " + link + ", is writable by this user: ghv-apply could be replaced\n"},
		{"the link's target", []string{link}, writableOnly(bin),
			"HIGH " + bin + ", a directory above " + filepath.Join(bin, "ghv-apply") + ", is writable by this user: ghv-apply could be replaced\n"},
	} {
		got := details(applyBinary(context.Background(), &Env{ApplyPaths: c.paths, Writable: c.writable}))
		if got != c.want {
			t.Errorf("%s: %q", c.name, got)
		}
	}
}

func TestShellConfig(t *testing.T) {
	human := t.TempDir()
	plant(t, human, map[string]string{".bashrc": "", ".zshrc": "", ".local/bin/x": ""})
	w := writableOnly(human, filepath.Join(human, ".bashrc"), filepath.Join(human, ".local/bin"), filepath.Join(human, ".profile"))
	e := &Env{Home: "/home/agent", Human: &Human{Home: human}, Writable: w}
	want := "HIGH shell config and bin directories in " + human + " are writable by this user: " + human +
		" itself (new config files can be added), .bashrc, .local/bin; a change there runs as the human, and can put another ghv-apply first on PATH\n"
	if got := details(shellConfig(context.Background(), e)); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
	if got := shellConfig(context.Background(), &Env{Home: human, Writable: writableOnly(human)}); got != nil {
		t.Fatalf("without --human-user the home itself is not listed: %v", got)
	}
}

func TestBrowserCookies(t *testing.T) {
	home := t.TempDir()
	plant(t, home, map[string]string{
		".mozilla/firefox/abc.default/cookies.sqlite":   "",
		".config/google-chrome/Default/Network/Cookies": "",
		".config/google-chrome/Profile 1/Cookies":       "",
		".config/chromium/Default/Cookies":              "",
	})
	if os.Getuid() != 0 {
		os.Chmod(filepath.Join(home, ".config/chromium/Default/Cookies"), 0)
	}
	want := "MEDIUM Firefox's cookies in " + home + " are readable: .mozilla/firefox/abc.default/cookies.sqlite; a GitHub session cookie there is a login\n" +
		"MEDIUM Chrome's cookies in " + home + " are readable: .config/google-chrome/Profile 1/Cookies, .config/google-chrome/Default/Network/Cookies; a GitHub session cookie there is a login\n"
	if got := details(browserCookies(context.Background(), &Env{Home: home})); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}
