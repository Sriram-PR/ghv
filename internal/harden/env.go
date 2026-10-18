// Package harden sets up ghv-apply's process before it touches a secret:
// an allowlisted environment, no core dumps or tracing, and an HTTP client
// that talks to api.github.com only.
package harden

import (
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"syscall"
)

// Environ keeps TERM, LANG, LC_*, and EDITOR and VISUAL when they start
// with an absolute path, in their original order. A repeated name keeps
// its first value, which is the one Go's os.Getenv reads.
func Environ(env []string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, kv := range env {
		k, v, ok := strings.Cut(kv, "=")
		if !ok || seen[k] || !allowed(k, v) {
			continue
		}
		seen[k] = true
		out = append(out, kv)
	}
	return out
}

func allowed(k, v string) bool {
	switch k {
	case "TERM", "LANG":
		return true
	case "EDITOR", "VISUAL":
		return filepath.IsAbs(v)
	}
	rest, ok := strings.CutPrefix(k, "LC_")
	return ok && rest != "" && strings.Trim(rest, "ABCDEFGHIJKLMNOPQRSTUVWXYZ_") == ""
}

// Init re-executes the process with Environ(os.Environ()) unless the
// environment is already exactly that. There is no marker variable: one
// the agent could set would skip the cleanup.
func Init() error {
	env := os.Environ()
	clean := Environ(env)
	if slices.Equal(env, clean) {
		return nil
	}
	exe := "/proc/self/exe"
	if runtime.GOOS != "linux" {
		var err error
		if exe, err = os.Executable(); err != nil {
			return fmt.Errorf("find own executable: %v", err)
		}
	}
	return fmt.Errorf("re-exec with a clean environment: %v", syscall.Exec(exe, os.Args, clean))
}

// Home is the home directory of the current user's passwd entry; $HOME is
// never read. os/user.Current is not used: without cgo it falls back to
// $HOME.
func Home() (string, error) {
	uid := os.Getuid()
	u, err := user.LookupId(strconv.Itoa(uid))
	if err != nil {
		return "", fmt.Errorf("no passwd entry for uid %d: %v", uid, err)
	}
	if !filepath.IsAbs(u.HomeDir) {
		return "", fmt.Errorf("the passwd home of uid %d, %q, is not an absolute path", uid, u.HomeDir)
	}
	return u.HomeDir, nil
}
