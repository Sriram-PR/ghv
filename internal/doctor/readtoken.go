package doctor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

func init() { register("read-token", readToken) }

const (
	fineGrainedPage = "https://github.com/settings/personal-access-tokens"
	ackPrompt       = "GitHub does not show a fine-grained token's permissions. At " + fineGrainedPage +
		", is the read token read-only and limited to public repositories? Type yes to record that: "
)

func replaceFix(path string) string {
	return "replace it with a classic token with no scopes (https://github.com/settings/tokens/new, tick nothing), saved to " + path
}

func readToken(ctx context.Context, e *Env) []Finding {
	if e.TokenPath == "" {
		return nil
	}
	b, err := readCapped(e.TokenPath, 4097)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return []Finding{{Severity: Info, Detail: fmt.Sprintf("no read token at %s: ghv reads anonymously", e.TokenPath)}}
	case err != nil:
		return []Finding{{Severity: Info, Detail: fmt.Sprintf("cannot read the read token at %s: %v", e.TokenPath, err)}}
	}
	tok := strings.TrimSpace(string(b))
	switch tokenKind(tok) {
	case "classic":
		return classicToken(ctx, e, tok)
	case "fine-grained":
		return fineGrainedToken(e, tok)
	}
	return []Finding{{Severity: Info, Detail: fmt.Sprintf("%s does not hold a personal access token (ghp_ or github_pat_), so ghv refuses it", e.TokenPath)}}
}

// tokenKind tells the type from the prefix, as ghv does; classic tokens
// made before 2021 are 40 hex characters.
func tokenKind(tok string) string {
	switch {
	case strings.HasPrefix(tok, "ghp_"):
		return "classic"
	case strings.HasPrefix(tok, "github_pat_"):
		return "fine-grained"
	case len(tok) == 40 && strings.Trim(tok, "0123456789abcdef") == "":
		return "classic"
	}
	return ""
}

func classicToken(ctx context.Context, e *Env, tok string) []Finding {
	if e.Scopes == nil {
		return []Finding{{Severity: High, Detail: "the read token's scopes were not checked", Fix: "run ghv token"}}
	}
	scopes, reported, err := e.Scopes(ctx, tok)
	switch {
	case err != nil:
		return []Finding{{Severity: High, Detail: "cannot check the read token's scopes: " + err.Error(), Fix: "run ghv token"}}
	case !reported:
		return []Finding{{Severity: High,
			Detail: fmt.Sprintf("GitHub sent no X-OAuth-Scopes header for the classic read token at %s, so it cannot be shown to be read-only", e.TokenPath),
			Fix:    replaceFix(e.TokenPath)}}
	case len(scopes) > 0:
		return []Finding{{Severity: High,
			Detail: fmt.Sprintf("the read token at %s has scopes %s, so it may be able to write", e.TokenPath, strings.Join(scopes, ", ")),
			Fix:    replaceFix(e.TokenPath)}}
	}
	return nil
}

// fineGrainedToken accepts a fine-grained token once the human has said,
// on a terminal, that it is read-only. The acknowledgement holds the
// token's SHA-256, so a new token needs a new one.
func fineGrainedToken(e *Env, tok string) []Finding {
	sum := sha256.Sum256([]byte(tok))
	want := hex.EncodeToString(sum[:])
	ack := e.TokenPath + ".ack"
	if b, err := readCapped(ack, 128); err == nil && strings.TrimSpace(string(b)) == want {
		return []Finding{{Severity: Info, Detail: fmt.Sprintf("the fine-grained read token at %s was acknowledged as read-only (%s)", e.TokenPath, ack)}}
	}
	unverified := Finding{Severity: High,
		Detail: fmt.Sprintf("the read token at %s is fine-grained: GitHub does not show its permissions, so it cannot be shown to be read-only", e.TokenPath),
		Fix:    "check it at " + fineGrainedPage + ", then run doctor --ack-fine-grained; or " + replaceFix(e.TokenPath)}
	if !e.AckFineGrained {
		return []Finding{unverified}
	}
	if e.Confirm == nil {
		unverified.Detail += "; --ack-fine-grained needs a terminal to ask on"
		return []Finding{unverified}
	}
	yes, err := e.Confirm(ackPrompt)
	if err != nil {
		unverified.Detail += "; cannot ask: " + err.Error()
	}
	if err != nil || !yes {
		return []Finding{unverified}
	}
	if err := writeAck(ack, want); err != nil {
		unverified.Detail += "; cannot record the acknowledgement: " + err.Error()
		return []Finding{unverified}
	}
	return []Finding{{Severity: Info, Detail: fmt.Sprintf("the fine-grained read token at %s is now acknowledged as read-only (%s)", e.TokenPath, ack)}}
}

func writeAck(path, sum string) error {
	tmp, err := os.CreateTemp(filepath.Dir(path), ".read-token.ack.*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(sum + "\n"); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}
