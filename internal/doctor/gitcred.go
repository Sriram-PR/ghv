package doctor

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

func init() { register("git-credentials", gitCredentialFill) }

var gitTimeout = 10 * time.Second

func gitCredentialFill(ctx context.Context, e *Env) []Finding {
	if e.Exec == nil {
		return nil
	}
	helpers := gitHelpers(ctx, e)
	fillCtx, cancel := context.WithTimeout(ctx, gitTimeout)
	defer cancel()
	out, err := e.Exec(fillCtx, "protocol=https\nhost=github.com\n\n", "git", "-c", "core.askPass=", "-c", "credential.interactive=never", "credential", "fill")
	got := hasPassword(out)
	clear(out)
	var exit ExitError
	switch {
	case got:
		detail := "git credential fill returns a password for https://github.com"
		if len(helpers) > 0 {
			detail += " (credential helpers: " + strings.Join(helpers, ", ") + ")"
		}
		return []Finding{{Severity: High, Detail: detail,
			Fix: `erase it: printf 'protocol=https\nhost=github.com\n\n' | git credential reject; remove the helper from your git config, and revoke the token at https://github.com/settings/tokens; or run the agent as its own user`}}
	case fillCtx.Err() != nil:
		return []Finding{{Severity: Info, Detail: fmt.Sprintf("git credential fill did not finish in %s; credential helpers not checked", gitTimeout)}}
	case err != nil && !errors.As(err, &exit):
		return []Finding{{Severity: Info, Detail: "git credential fill could not run: " + err.Error()}}
	case len(helpers) > 0:
		return []Finding{{Severity: Info, Detail: "git credential helpers are set (" + strings.Join(helpers, ", ") + "), but none returned a password for https://github.com"}}
	}
	return nil
}

func hasPassword(out []byte) bool {
	for l := range bytes.Lines(out) {
		if v, ok := bytes.CutPrefix(bytes.TrimRight(l, "\n"), []byte("password=")); ok && len(v) > 0 {
			return true
		}
	}
	return false
}

var knownHelpers = []string{"store", "cache", "libsecret", "osxkeychain", "manager", "manager-core", "wincred", "gh", "oauth", "netrc"}

// gitHelpers names the credential helpers set for every URL or for
// github.com. A helper's command can hold a token, so only known names
// are shown.
func gitHelpers(ctx context.Context, e *Env) []string {
	ctx, cancel := context.WithTimeout(ctx, gitTimeout)
	defer cancel()
	out, err := e.Exec(ctx, "", "git", "config", "--get-regexp", `^credential\..*helper$`)
	if err != nil {
		return nil
	}
	var names []string
	for l := range strings.Lines(string(out)) {
		key, val, _ := strings.Cut(strings.TrimRight(l, "\n"), " ")
		// credential.helper, or credential.<url>.helper
		if url := strings.TrimSuffix(strings.TrimPrefix(key, "credential."), "helper"); url != "" && !strings.Contains(url, "github.com") {
			continue
		}
		f := strings.Fields(strings.TrimPrefix(strings.TrimSpace(val), "!"))
		if len(f) == 0 {
			continue
		}
		name := strings.TrimPrefix(filepath.Base(f[0]), "git-credential-")
		if !slices.Contains(knownHelpers, name) {
			name = "a custom helper"
		}
		if !slices.Contains(names, name) {
			names = append(names, name)
		}
	}
	return names
}
