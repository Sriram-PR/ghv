package doctor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

func init() {
	register("ghv-apply", applyBinary)
	register("shell-config", shellConfig)
	register("browser-cookies", browserCookies)
}

const installFix = "install ghv-apply root-owned in a root-owned directory: sudo install -o root -m 0755 ghv-apply /usr/local/bin/ghv-apply (on macOS, a root-owned directory other than Homebrew's)"

func applyBinary(_ context.Context, e *Env) []Finding {
	if len(e.ApplyPaths) == 0 {
		return []Finding{{Severity: Info, Detail: "no ghv-apply found to check"}}
	}
	var out []Finding
	for _, p := range e.ApplyPaths {
		for _, q := range linkChain(p) {
			if w := firstWritable(e, q); w != "" {
				what := w + ", a directory above " + q + ","
				if w == q {
					what = q
				}
				out = append(out, Finding{Severity: High,
					Detail: what + " is writable by this user: ghv-apply could be replaced",
					Fix:    installFix})
				break
			}
		}
	}
	return out
}

// linkChain is path, each link it leads through, and its resolved target:
// changing any of them changes what runs.
func linkChain(path string) []string {
	out := []string{path}
	for p := path; len(out) < 40; {
		t, err := os.Readlink(p)
		if err != nil {
			break
		}
		if !filepath.IsAbs(t) {
			t = filepath.Join(filepath.Dir(p), t)
		}
		p = filepath.Clean(t)
		out = append(out, p)
	}
	if r, err := filepath.EvalSymlinks(path); err == nil && !slices.Contains(out, r) {
		out = append(out, r)
	}
	return out
}

// firstWritable is path, or the nearest directory above it, that this
// process can change, or "".
func firstWritable(e *Env, path string) string {
	for p := path; ; p = filepath.Dir(p) {
		if e.writable(p) {
			return p
		}
		if p == filepath.Dir(p) {
			return ""
		}
	}
}

var shellFiles = []string{
	".bashrc", ".bash_profile", ".bash_login", ".profile", ".zshrc", ".zprofile", ".zshenv", ".zlogin",
	".config/fish/config.fish", ".local/bin", "bin",
}

func shellConfig(_ context.Context, e *Env) []Finding {
	home := e.humanHome()
	var names []string
	if e.Human != nil && e.writable(home) {
		names = append(names, home+" itself (new config files can be added)")
	}
	for _, n := range shellFiles {
		p := filepath.Join(home, n)
		if _, err := os.Lstat(p); err == nil && e.writable(p) {
			names = append(names, n)
		}
	}
	if len(names) == 0 {
		return nil
	}
	return []Finding{{Severity: High,
		Detail: fmt.Sprintf("shell config and bin directories in %s are writable by this user: %s; a change there runs as the human, and can put another ghv-apply first on PATH", home, list(names)),
		Fix:    "keep the agent's user out of the human's home (see the README's containment guide)"}}
}

// browsers lists, per browser, the cookie stores' globs relative to a home.
var browsers = []struct {
	name  string
	globs []string
}{
	{"Firefox", []string{".mozilla/firefox/*/cookies.sqlite", "snap/firefox/common/.mozilla/firefox/*/cookies.sqlite",
		".var/app/org.mozilla.firefox/.mozilla/firefox/*/cookies.sqlite", "Library/Application Support/Firefox/Profiles/*/cookies.sqlite"}},
	{"Chrome", chromium(".config/google-chrome", ".var/app/com.google.Chrome/config/google-chrome", "Library/Application Support/Google/Chrome")},
	{"Chromium", chromium(".config/chromium", "snap/chromium/common/chromium", "Library/Application Support/Chromium")},
	{"Brave", chromium(".config/BraveSoftware/Brave-Browser", "Library/Application Support/BraveSoftware/Brave-Browser")},
	{"Edge", chromium(".config/microsoft-edge", "Library/Application Support/Microsoft Edge")},
	{"Vivaldi", chromium(".config/vivaldi", "Library/Application Support/Vivaldi")},
}

func chromium(dirs ...string) []string {
	var gs []string
	for _, d := range dirs {
		gs = append(gs, d+"/*/Cookies", d+"/*/Network/Cookies")
	}
	return gs
}

func browserCookies(_ context.Context, e *Env) []Finding {
	var out []Finding
	for _, home := range e.homes() {
		for _, b := range browsers {
			var found []string
			for _, g := range b.globs {
				ms, _ := filepath.Glob(filepath.Join(home, g))
				for _, m := range ms {
					if readable(m) {
						found = append(found, strings.TrimPrefix(m, home+"/"))
					}
				}
			}
			if len(found) > 0 {
				out = append(out, Finding{Severity: Medium,
					Detail: fmt.Sprintf("%s's cookies in %s are readable: %s; a GitHub session cookie there is a login", b.name, home, list(found)),
					Fix:    "keep the agent's user out of the human's home (see the README's containment guide)"})
			}
		}
	}
	return out
}
