package doctor

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

func init() { register("ssh-keys", sshKeys) }

const sshKeyLimit = 64 << 10

func sshKeys(_ context.Context, e *Env) []Finding {
	var out []Finding
	for _, k := range unprotectedKeys(e) {
		f := Finding{Severity: High,
			Detail: fmt.Sprintf("%s has no passphrase (%s)", k.path, k.describe()),
			Fix:    fmt.Sprintf("add a passphrase: ssh-keygen -p -f %s; or delete the key, and remove it at https://github.com/settings/keys if it is there", k.path)}
		if k.securityKey() {
			f.Severity = Medium
			f.Detail += "; using it needs its security key"
		}
		out = append(out, f)
	}
	return out
}

// unprotectedKeys are the private keys without a passphrase in each home's
// ~/.ssh, then those its ~/.ssh/config names elsewhere.
func unprotectedKeys(e *Env) []sshKey {
	var out []sshKey
	seen := map[string]bool{}
	for _, home := range e.homes() {
		dir := filepath.Join(home, ".ssh")
		var paths []string
		if ents, err := os.ReadDir(dir); err == nil {
			for _, ent := range ents {
				if n := ent.Name(); ent.Type().IsRegular() && !strings.HasSuffix(n, ".pub") {
					paths = append(paths, filepath.Join(dir, n))
				}
			}
		}
		paths = append(paths, identityFiles(home)...)
		for _, p := range paths {
			if seen[p] {
				continue
			}
			seen[p] = true
			b, err := readCapped(p, sshKeyLimit)
			if err != nil {
				continue
			}
			if k, ok := unprotectedKey(p, b); ok {
				out = append(out, k)
			}
		}
	}
	return out
}

// identityFiles are the absolute IdentityFile paths in home's
// ~/.ssh/config, with ~ and %d expanded; other tokens are skipped.
func identityFiles(home string) []string {
	b, err := readCapped(filepath.Join(home, ".ssh", "config"), credLimit)
	if err != nil {
		return nil
	}
	var out []string
	sc := bufio.NewScanner(strings.NewReader(string(b)))
	for sc.Scan() {
		f := strings.FieldsFunc(sc.Text(), func(r rune) bool { return r == ' ' || r == '\t' || r == '=' })
		if len(f) < 2 || !strings.EqualFold(f[0], "IdentityFile") {
			continue
		}
		p := strings.Trim(strings.Join(f[1:], " "), `"`)
		if rest, ok := strings.CutPrefix(p, "~/"); ok {
			p = filepath.Join(home, rest)
		} else if rest, ok := strings.CutPrefix(p, "%d/"); ok {
			p = filepath.Join(home, rest)
		}
		if p = filepath.Clean(p); filepath.IsAbs(p) && !strings.Contains(p, "%") && !slices.Contains(out, p) {
			out = append(out, p)
		}
	}
	return out
}
