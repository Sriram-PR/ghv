package doctor

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

func init() { register("ghv-files", ghvFiles) }

// ghvFiles checks the human's vault and audit log, and each directory from
// theirs up to the human's home: if this process can change one, the
// agent could delete or replace them. Missing files are checked through
// their nearest existing directory.
func ghvFiles(_ context.Context, e *Env) []Finding {
	if e.Human == nil || e.Human.UID == e.UID {
		return nil
	}
	home := e.Human.Home
	vault := filepath.Join(home, ".config", "ghv", "vault")
	var changeable []string
	for _, f := range []string{vault, filepath.Join(home, ".local", "state", "ghv", "audit.jsonl")} {
		for p := f; ; p = filepath.Dir(p) {
			if _, err := os.Lstat(p); err == nil && e.writable(p) && !slices.Contains(changeable, p) {
				changeable = append(changeable, p)
			}
			if p == home || p == filepath.Dir(p) {
				break
			}
		}
	}
	var out []Finding
	if len(changeable) > 0 {
		out = append(out, Finding{Severity: High,
			Detail: fmt.Sprintf("%s's ghv-apply files can be changed by this user: %s; the agent could delete or replace the vault or the audit log", e.Human.Name, list(changeable)),
			Fix:    "chmod go-w " + strings.Join(changeable, " ")})
	}
	if readable(vault) && !slices.Contains(changeable, vault) {
		out = append(out, Finding{Severity: Info,
			Detail: fmt.Sprintf("%s's vault at %s is readable by this user; it is sealed with the passphrase, but the agent should not have it", e.Human.Name, vault)})
	}
	return out
}
