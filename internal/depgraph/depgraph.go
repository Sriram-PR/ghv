// Package depgraph checks the import rules behind AGENTS.md invariants 1 and 2.
package depgraph

import (
	"fmt"
	"slices"
	"strings"
)

const Module = "github.com/Sriram-PR/ghv"

type Package struct {
	ImportPath string
	Deps       []string
}

var ghvAllowed = []string{
	"internal/read",
	"internal/draft",
	"internal/sanitize",
	"internal/archive",
	"internal/doctor",
}

var applyThirdParty = []string{
	"golang.org/x/crypto",
	"golang.org/x/term",
	"golang.org/x/sys",
}

var shared = []string{"internal/draft", "internal/sanitize", "internal/doctor"}

var sharedForbidden = []string{
	"internal/read",
	"internal/fetch",
	"internal/write",
	"internal/vault",
	"internal/harden",
	"internal/audit",
}

// Violations returns one message per broken rule; Deps must be transitive.
func Violations(pkgs []Package) []string {
	var out []string
	found := map[string]bool{}
	for _, p := range pkgs {
		rel, ok := strings.CutPrefix(p.ImportPath, Module+"/")
		if !ok {
			continue
		}
		found[rel] = true
		for _, d := range p.Deps {
			if !allowed(rel, d) {
				out = append(out, fmt.Sprintf("%s imports %s", rel, d))
			}
		}
	}
	for _, root := range []string{"cmd/ghv", "cmd/ghv-apply"} {
		if !found[root] {
			out = append(out, "missing package "+root)
		}
	}
	return out
}

func allowed(pkg, dep string) bool {
	depRel, local := strings.CutPrefix(dep, Module+"/")
	switch {
	case pkg == "cmd/ghv":
		return isStd(dep) || (local && slices.Contains(ghvAllowed, depRel))
	case pkg == "cmd/ghv-apply":
		if local {
			return depRel != "internal/read"
		}
		return isStd(dep) || inModules(dep, applyThirdParty)
	case slices.Contains(shared, pkg):
		return !local || !slices.Contains(sharedForbidden, depRel)
	}
	return true
}

func isStd(path string) bool {
	first, _, _ := strings.Cut(path, "/")
	return !strings.Contains(first, ".")
}

func inModules(path string, mods []string) bool {
	for _, m := range mods {
		if path == m || strings.HasPrefix(path, m+"/") {
			return true
		}
	}
	return false
}
