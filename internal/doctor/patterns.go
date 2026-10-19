package doctor

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

func init() {
	register("token-patterns", tokenPatterns)
	register("docker-ghcr", dockerGHCR)
}

// scanned are the files, relative to a home, where GitHub tokens get
// pasted: shell history, shell and tool config, and MCP client configs.
var scanned = []string{
	".bash_history", ".zsh_history", ".histfile", ".local/share/fish/fish_history",
	".bashrc", ".bash_profile", ".bash_login", ".profile", ".zshrc", ".zprofile", ".zshenv", ".zlogin",
	".config/fish/config.fish", ".gitconfig", ".config/git/config", ".npmrc", ".config/gh/hosts.yml",
	".claude.json", ".mcp.json", ".claude/settings.json", ".cursor/mcp.json", ".codeium/windsurf/mcp_config.json",
	".config/Code/User/settings.json", ".config/Code/User/mcp.json",
	"Library/Application Support/Code/User/settings.json", "Library/Application Support/Code/User/mcp.json",
	".docker/config.json",
}

var tokenRE = regexp.MustCompile(`\b(?:gh[pousr]_[A-Za-z0-9]{36,255}|github_pat_[A-Za-z0-9_]{22,255})\b`)

const scanLimit = 64 << 20

func tokenPatterns(_ context.Context, e *Env) []Finding {
	var out []Finding
	for _, home := range e.homes() {
		for _, name := range scanned {
			p := filepath.Join(home, name)
			b, err := readCapped(p, scanLimit)
			if err != nil {
				continue
			}
			var lines, kinds []string
			n, i := 0, 0
			for line := range strings.Lines(string(b)) {
				i++
				ms := tokenRE.FindAllString(line, -1)
				if len(ms) == 0 {
					continue
				}
				n += len(ms)
				lines = append(lines, strconv.Itoa(i))
				for _, m := range ms {
					k := m[:strings.IndexByte(m, '_')+1]
					if strings.HasPrefix(m, "github_pat_") {
						k = "github_pat_"
					}
					if !slices.Contains(kinds, k) {
						kinds = append(kinds, k)
					}
				}
			}
			if n > 0 {
				on := "line"
				if len(lines) > 1 {
					on = "lines"
				}
				out = append(out, Finding{Severity: High,
					Detail: fmt.Sprintf("%s holds %s (%s) on %s %s", p, plural(n, "GitHub token", "GitHub tokens"), strings.Join(kinds, ", "), on, list(lines)),
					Fix:    fmt.Sprintf("delete them from %s and revoke them at https://github.com/settings/tokens", p)})
			}
		}
	}
	return out
}

// dockerGHCR finds a Docker login for GitHub's registries, which holds a
// GitHub token even when a credential helper keeps it out of the file.
func dockerGHCR(_ context.Context, e *Env) []Finding {
	var out []Finding
	for _, home := range e.homes() {
		p := filepath.Join(home, ".docker/config.json")
		b, err := readCapped(p, credLimit)
		if err != nil {
			continue
		}
		var c struct {
			Auths       map[string]json.RawMessage `json:"auths"`
			CredHelpers map[string]string          `json:"credHelpers"`
		}
		if json.Unmarshal(b, &c) != nil {
			continue
		}
		var hosts []string
		for h := range c.Auths {
			hosts = append(hosts, h)
		}
		for h := range c.CredHelpers {
			hosts = append(hosts, h)
		}
		slices.Sort(hosts)
		for _, h := range slices.Compact(hosts) {
			host := strings.TrimPrefix(strings.TrimPrefix(h, "https://"), "http://")
			host, _, _ = strings.Cut(host, "/")
			if name, _, _ := strings.Cut(host, ":"); name == "ghcr.io" || name == "docker.pkg.github.com" {
				out = append(out, Finding{Severity: High,
					Detail: fmt.Sprintf("%s has a login for %s, which is a GitHub token", p, h),
					Fix:    fmt.Sprintf("docker logout %s, and revoke the token at https://github.com/settings/tokens", host)})
			}
		}
	}
	return out
}
