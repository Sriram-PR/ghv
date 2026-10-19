package doctor

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
)

func init() {
	register("env-tokens", envTokens)
	register("proxy", proxyVars)
}

var tokenVars = []string{"GITHUB_TOKEN", "GH_TOKEN", "GITHUB_PERSONAL_ACCESS_TOKEN"}

const environLimit = 1 << 20

func envTokens(_ context.Context, e *Env) []Finding {
	var out []Finding
	if e.Getenv == nil && e.EnvErr != nil {
		out = append(out, Finding{Severity: Info, Detail: fmt.Sprintf("%s, not checked: %v", e.EnvOf, e.EnvErr)})
	}
	if e.Getenv != nil {
		for _, v := range tokenVars {
			if e.Getenv(v) != "" {
				out = append(out, Finding{Severity: High,
					Detail: fmt.Sprintf("%s is set in %s", v, e.EnvOf),
					Fix:    fmt.Sprintf("unset %s, remove it from wherever it is set, and revoke the token at https://github.com/settings/tokens", v)})
			}
		}
	}
	if e.Proc == "" {
		return append(out, Finding{Severity: Info, Detail: "other processes' environments not checked: no /proc on " + e.GOOS})
	}
	holders := map[string][]string{}
	ents, _ := os.ReadDir(e.Proc)
	for _, ent := range ents {
		pid, err := strconv.Atoi(ent.Name())
		if err != nil || pid == e.PID || pid == e.EnvPID {
			continue
		}
		b, err := readCapped(filepath.Join(e.Proc, ent.Name(), "environ"), environLimit)
		if err != nil {
			continue
		}
		for kv := range bytes.SplitSeq(b, []byte{0}) {
			k, val, _ := bytes.Cut(kv, []byte("="))
			if len(val) > 0 && slices.Contains(tokenVars, string(k)) {
				holders[string(k)] = append(holders[string(k)], procName(e.Proc, ent.Name()))
			}
		}
	}
	for _, v := range tokenVars {
		if ps := holders[v]; len(ps) > 0 {
			out = append(out, Finding{Severity: High,
				Detail: fmt.Sprintf("%s is set in %s: %s", v, plural(len(ps), "other process", "other processes"), list(ps)),
				Fix:    fmt.Sprintf("find where %s is set for them (shell config, a service, a launcher), remove it, and revoke the token", v)})
		}
	}
	return out
}

// Environ is the environment pid started with, first value of each name.
func Environ(proc string, pid int) (map[string]string, error) {
	b, err := readCapped(filepath.Join(proc, strconv.Itoa(pid), "environ"), environLimit)
	if err != nil {
		return nil, err
	}
	vars := map[string]string{}
	for kv := range bytes.SplitSeq(b, []byte{0}) {
		if k, v, ok := bytes.Cut(kv, []byte("=")); ok {
			if _, seen := vars[string(k)]; !seen {
				vars[string(k)] = string(v)
			}
		}
	}
	return vars, nil
}

func procName(proc, pid string) string {
	b, _ := readCapped(filepath.Join(proc, pid, "comm"), 64)
	if c := strings.TrimSpace(string(b)); c != "" {
		return fmt.Sprintf("pid %s (%s)", pid, c)
	}
	return "pid " + pid
}

// list joins up to five items, then counts the rest.
func list(items []string) string {
	if len(items) > 5 {
		return strings.Join(items[:5], ", ") + fmt.Sprintf(" and %d more", len(items)-5)
	}
	return strings.Join(items, ", ")
}

var proxyNames = []string{"HTTPS_PROXY", "https_proxy", "ALL_PROXY", "all_proxy", "SSL_CERT_FILE", "SSL_CERT_DIR"}

// proxyVars names, without their values, settings that steer other
// tools' TLS. ghv-apply drops them.
func proxyVars(_ context.Context, e *Env) []Finding {
	if e.Getenv == nil {
		return nil
	}
	var set []string
	for _, v := range proxyNames {
		if e.Getenv(v) != "" {
			set = append(set, v)
		}
	}
	if len(set) == 0 {
		return nil
	}
	return []Finding{{Severity: Info,
		Detail: fmt.Sprintf("%s set in %s: ghv-apply ignores it; other tools send their traffic through it", strings.Join(set, ", "), e.EnvOf)}}
}
