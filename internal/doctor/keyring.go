package doctor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

func init() { register("keyring", keyring) }

// keyringSearches are the attributes each tool stores a github.com token
// under: gh through go-keyring, git's libsecret helper, and Git Credential
// Manager with its default "git" namespace.
var keyringSearches = []struct {
	who   string
	attrs map[string]string
}{
	{"gh", map[string]string{"service": "gh:github.com"}},
	{"git-credential-libsecret", map[string]string{"server": "github.com"}},
	{"Git Credential Manager", map[string]string{"service": "git:https://github.com"}},
}

const keyringFix = `remove them: gh auth logout, and printf 'protocol=https\nhost=github.com\n\n' | git credential reject; or run the agent as its own user, which cannot reach your session bus`

func keyring(ctx context.Context, e *Env) []Finding {
	if e.GOOS == "darwin" {
		return keychain(ctx, e)
	}
	var out []Finding
	for _, bus := range sessionBuses(e) {
		counts, locked, err := searchKeyring(bus, e.UID)
		if err != nil {
			continue
		}
		total := 0
		var parts []string
		for i, s := range keyringSearches {
			if counts[i] > 0 {
				total += counts[i]
				parts = append(parts, fmt.Sprintf("%s %d", s.who, counts[i]))
			}
		}
		if total == 0 {
			continue
		}
		state := "it is unlocked"
		if total > 1 {
			state = "they are unlocked"
		}
		if locked > 0 {
			state = fmt.Sprintf("%d %s locked, but asking for one can show you an unlock prompt", locked, map[bool]string{true: "is", false: "are"}[locked == 1])
		}
		out = append(out, Finding{Severity: High,
			Detail: fmt.Sprintf("the keyring (Secret Service) on the session bus at %s holds %s for github.com: %s; %s", bus, plural(total, "item", "items"), strings.Join(parts, ", "), state),
			Fix:    keyringFix})
	}
	return out
}

// sessionBuses are the session bus sockets this process might reach: its
// DBUS_SESSION_BUS_ADDRESS, and each user's runtime directory.
func sessionBuses(e *Env) []string {
	var cands []string
	if e.Getenv != nil {
		for addr := range strings.SplitSeq(e.Getenv("DBUS_SESSION_BUS_ADDRESS"), ";") {
			if rest, ok := strings.CutPrefix(addr, "unix:"); ok {
				for kv := range strings.SplitSeq(rest, ",") {
					if p, ok := strings.CutPrefix(kv, "path="); ok {
						cands = append(cands, p)
					}
				}
			}
		}
	}
	if e.Runtime != "" {
		cands = append(cands, filepath.Join(e.Runtime, strconv.Itoa(e.UID), "bus"))
		if e.Human != nil && e.Human.UID != e.UID {
			cands = append(cands, filepath.Join(e.Runtime, strconv.Itoa(e.Human.UID), "bus"))
		}
	}
	var out []string
	var seen []os.FileInfo
	for _, p := range cands {
		fi, err := os.Stat(p)
		if !filepath.IsAbs(p) || err != nil || fi.Mode()&os.ModeSocket == 0 ||
			slices.ContainsFunc(seen, func(s os.FileInfo) bool { return os.SameFile(s, fi) }) {
			continue
		}
		seen = append(seen, fi)
		out = append(out, p)
	}
	return out
}

// searchKeyring counts the items each of keyringSearches finds, each item
// once, and how many are locked. Nothing is unlocked or read.
func searchKeyring(bus string, uid int) (counts []int, locked int, err error) {
	b, err := dialBus(bus, uid)
	if err != nil {
		return nil, 0, err
	}
	defer b.Close()
	counts = make([]int, len(keyringSearches))
	seen := map[string]bool{}
	for i, s := range keyringSearches {
		m, err := b.call("org.freedesktop.secrets", "/org/freedesktop/secrets", "org.freedesktop.Secret.Service", "SearchItems", s.attrs)
		if err != nil {
			return nil, 0, err
		}
		u, l, err := objectPathArrays(m)
		if err != nil {
			return nil, 0, err
		}
		for j, p := range append(u, l...) {
			if !seen[p] {
				seen[p] = true
				counts[i]++
				if j >= len(u) {
					locked++
				}
			}
		}
	}
	return counts, locked, nil
}

// keychain asks macOS's security for each tool's item by its attributes,
// without -g, so no secret is read and no prompt is shown.
func keychain(ctx context.Context, e *Env) []Finding {
	if e.Exec == nil {
		return nil
	}
	var who []string
	for _, q := range []struct {
		who  string
		args []string
	}{
		{"gh", []string{"find-generic-password", "-s", "gh:github.com"}},
		{"git-credential-osxkeychain", []string{"find-internet-password", "-s", "github.com"}},
		{"Git Credential Manager", []string{"find-generic-password", "-s", "git:https://github.com"}},
	} {
		out, err := e.Exec(ctx, "", "security", q.args...)
		clear(out)
		if err == nil {
			who = append(who, q.who)
		}
	}
	if len(who) == 0 {
		return nil
	}
	return []Finding{{Severity: High, Detail: "the login keychain holds GitHub items from " + strings.Join(who, ", "),
		Fix: "remove them in Keychain Access (gh auth logout for gh's); or run the agent as its own user, with its own keychain"}}
}
