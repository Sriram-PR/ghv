package doctor

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func tokenEnv(t *testing.T, tok string) *Env {
	t.Helper()
	dir := t.TempDir()
	e := &Env{TokenPath: filepath.Join(dir, "read-token")}
	if tok != "" {
		if err := os.WriteFile(e.TokenPath, []byte(tok+"\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return e
}

func scopes(s []string, reported bool, err error) func(context.Context, string) ([]string, bool, error) {
	return func(context.Context, string) ([]string, bool, error) { return s, reported, err }
}

func TestReadTokenClassic(t *testing.T) {
	for _, c := range []struct {
		name   string
		scopes func(context.Context, string) ([]string, bool, error)
		want   string
	}{
		{"no scopes", scopes([]string{}, true, nil), ""},
		{"scopes", scopes([]string{"repo", "workflow"}, true, nil), "HIGH the read token at PATH has scopes repo, workflow, so it may be able to write\n"},
		{"no header", scopes(nil, false, nil), "HIGH GitHub sent no X-OAuth-Scopes header for the classic read token at PATH, so it cannot be shown to be read-only\n"},
		{"error", scopes(nil, false, errors.New("GitHub answered 401")), "HIGH cannot check the read token's scopes: GitHub answered 401\n"},
	} {
		e := tokenEnv(t, classic)
		var sent string
		e.Scopes = func(ctx context.Context, tok string) ([]string, bool, error) {
			sent = tok
			return c.scopes(ctx, tok)
		}
		got := strings.ReplaceAll(details(readToken(context.Background(), e)), e.TokenPath, "PATH")
		if got != c.want || sent != classic {
			t.Errorf("%s: %q, sent %q", c.name, got, sent)
		}
	}
}

func TestReadTokenOther(t *testing.T) {
	e := tokenEnv(t, "")
	if got := details(readToken(context.Background(), e)); got != "INFO no read token at "+e.TokenPath+": ghv reads anonymously\n" {
		t.Fatal(got)
	}
	e = tokenEnv(t, "gho_"+strings.Repeat("x", 36))
	if got := details(readToken(context.Background(), e)); !strings.Contains(got, "does not hold a personal access token") {
		t.Fatal(got)
	}
}

func TestReadTokenFineGrained(t *testing.T) {
	sum := sha256.Sum256([]byte(fineGrained))
	unverified := "HIGH the read token at PATH is fine-grained: GitHub does not show its permissions, so it cannot be shown to be read-only"
	e := tokenEnv(t, fineGrained)
	e.Scopes = func(context.Context, string) ([]string, bool, error) {
		t.Fatal("a fine-grained token is never sent")
		return nil, false, nil
	}
	got := func() string {
		return strings.ReplaceAll(details(readToken(context.Background(), e)), e.TokenPath, "PATH")
	}
	if g := got(); g != unverified+"\n" {
		t.Fatalf("no ack: %q", g)
	}
	e.AckFineGrained = true
	if g := got(); g != unverified+"; --ack-fine-grained needs a terminal to ask on\n" {
		t.Fatalf("no terminal: %q", g)
	}
	var asked string
	e.Confirm = func(p string) (bool, error) { asked = p; return false, nil }
	if g := got(); g != unverified+"\n" || asked != ackPrompt {
		t.Fatalf("declined: %q, asked %q", g, asked)
	}
	e.Confirm = func(string) (bool, error) { return false, errors.New("no terminal: open /dev/tty: no such device") }
	if g := got(); g != unverified+"; cannot ask: no terminal: open /dev/tty: no such device\n" {
		t.Fatalf("cannot ask: %q", g)
	}
	e.Confirm = func(string) (bool, error) { return true, nil }
	if g := got(); g != "INFO the fine-grained read token at PATH is now acknowledged as read-only (PATH.ack)\n" {
		t.Fatalf("confirmed: %q", g)
	}
	b, _ := os.ReadFile(e.TokenPath + ".ack")
	if string(b) != hex.EncodeToString(sum[:])+"\n" {
		t.Fatalf("ack file %q", b)
	}
	e.AckFineGrained, e.Confirm = false, nil
	if g := got(); g != "INFO the fine-grained read token at PATH was acknowledged as read-only (PATH.ack)\n" {
		t.Fatalf("acked: %q", g)
	}
	other := "github_pat_" + strings.Repeat("Z9", 30)
	os.WriteFile(e.TokenPath, []byte(other), 0o600)
	if g := got(); g != unverified+"\n" {
		t.Fatalf("a new token needs a new ack: %q", g)
	}
}
