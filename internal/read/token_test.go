package read

import (
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"slices"
	"syscall"
	"testing"
	"time"
)

func writeToken(t *testing.T, content string, mode os.FileMode) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "read-token")
	if err := os.WriteFile(p, []byte(content), mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(p, mode); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestLoadToken(t *testing.T) {
	tok, err := LoadToken(writeToken(t, "ghp_abcDEF123\n", 0o600))
	if err != nil || tok != "ghp_abcDEF123" {
		t.Fatalf("got %q, %v", tok, err)
	}
}

func TestLoadTokenMissingIsAnonymous(t *testing.T) {
	tok, err := LoadToken(filepath.Join(t.TempDir(), "read-token"))
	if err != nil || tok != "" {
		t.Fatalf("got %q, %v; want anonymous", tok, err)
	}
}

func TestLoadTokenRefuses(t *testing.T) {
	big := make([]byte, 5000)
	for i := range big {
		big[i] = 'a'
	}
	tests := []struct {
		name, content string
		mode          os.FileMode
	}{
		{"group readable", "ghp_x", 0o640},
		{"world readable", "ghp_x", 0o604},
		{"empty", "\n", 0o600},
		{"two lines", "ghp_x\nghp_y\n", 0o600},
		{"header injection", "ghp_x\r\nX-HTTP-Method-Override: DELETE", 0o600},
		{"space inside", "ghp_ x", 0o600},
		{"oauth token", "gho_abc", 0o600},
		{"app token", "ghs_abc", 0o600},
		{"too long", string(big), 0o600},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := LoadToken(writeToken(t, tt.content, tt.mode))
			if !errors.Is(err, ErrTokenFile) {
				t.Fatalf("err = %v, want ErrTokenFile", err)
			}
		})
	}
}

func TestLoadTokenRefusesDirectoryAndFIFO(t *testing.T) {
	dir := t.TempDir()
	if _, err := LoadToken(dir); !errors.Is(err, ErrTokenFile) {
		t.Fatalf("directory: err = %v", err)
	}
	fifo := filepath.Join(dir, "fifo")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadToken(fifo); !errors.Is(err, ErrTokenFile) {
		t.Fatalf("FIFO: err = %v", err)
	}
}

func TestKindOf(t *testing.T) {
	tests := map[string]TokenKind{
		"":                   TokenNone,
		"ghp_abc":            TokenClassic,
		"github_pat_11ABC_x": TokenFineGrained,
		"0123456789abcdef0123456789abcdef01234567": TokenClassic,
	}
	for tok, want := range tests {
		if got := KindOf(tok); got != want {
			t.Errorf("KindOf(%q) = %q, want %q", tok, got, want)
		}
	}
}

func TestInspect(t *testing.T) {
	exp := "2026-11-05 14:03:13 UTC"
	tests := []struct {
		name    string
		kind    TokenKind
		header  http.Header
		verdict Verdict
		scopes  []string
	}{
		{"anonymous", TokenNone, http.Header{}, VerdictAnonymous, nil},
		{"classic no scopes", TokenClassic, http.Header{"X-Oauth-Scopes": {""}}, VerdictReadOnly, []string{}},
		{"classic with scopes", TokenClassic, http.Header{"X-Oauth-Scopes": {"public_repo, read:org"}}, VerdictRefused, []string{"public_repo", "read:org"}},
		{"classic without the header", TokenClassic, http.Header{}, VerdictUnverified, nil},
		{"fine-grained", TokenFineGrained, http.Header{"Github-Authentication-Token-Expiration": {exp}}, VerdictUnverified, nil},
		{"fine-grained with scopes", TokenFineGrained, http.Header{"X-Oauth-Scopes": {"repo"}}, VerdictRefused, []string{"repo"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Inspect(tt.kind, tt.header)
			if got.Verdict != tt.verdict || got.Kind != tt.kind || !slices.Equal(got.Scopes, tt.scopes) {
				t.Fatalf("got %+v", got)
			}
			if (tt.scopes == nil) != (got.Scopes == nil) {
				t.Fatalf("scopes nil = %v, want %v", got.Scopes == nil, tt.scopes == nil)
			}
		})
	}
}

func TestInspectExpiry(t *testing.T) {
	for _, v := range []string{"2026-11-05 14:03:13 UTC", "2026-11-05 14:03:13 +0000"} {
		got := Inspect(TokenFineGrained, http.Header{"Github-Authentication-Token-Expiration": {v}})
		want := time.Date(2026, 11, 5, 14, 3, 13, 0, time.UTC)
		if got.Expires == nil || !got.Expires.Equal(want) {
			t.Errorf("%q: Expires = %v", v, got.Expires)
		}
	}
	if got := Inspect(TokenClassic, http.Header{"X-Oauth-Scopes": {""}}); got.Expires != nil {
		t.Errorf("no expiry header: Expires = %v", got.Expires)
	}
}
