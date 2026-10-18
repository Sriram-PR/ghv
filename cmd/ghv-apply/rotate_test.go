package main

import (
	"bytes"
	"crypto/rand"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Sriram-PR/ghv/internal/vault"
)

const (
	oldPass  = "the old passphrase, long enough"
	newPass  = "the new passphrase, long enough"
	newToken = "ghp_ZYXWVUTSRQPONMLKJIHGFEDCBA9876543210"
)

// seedVault writes a vault for Sriram-PR holding testToken under oldPass
// and returns its bytes.
func seedVault(t *testing.T, home string) []byte {
	t.Helper()
	exp := time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC)
	s := vault.Secret{Login: "Sriram-PR", Token: []byte(testToken), Type: "classic",
		Created: time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), Expires: &exp}
	data, err := vault.Seal(s, []byte(oldPass), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if err := vault.Create(vault.Path(home), data); err != nil {
		t.Fatal(err)
	}
	return data
}

func rotate(t *testing.T, gh *fakeGitHub, as ...any) (env, []byte, *fakeTerm) {
	t.Helper()
	ft := answers(t, as...)
	e := ttyEnv(t, ft, gh)
	before := seedVault(t, e.home)
	r := runWith(e, "rotate")
	if r.code != 0 || r.stderr != "" || ft.unanswered() != 0 {
		t.Fatalf("exit %d, stderr %q, terminal:\n%s", r.code, r.stderr, ft.shown())
	}
	after, err := vault.Load(vault.Path(e.home))
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(before[14:30], after[14:30]) || bytes.Equal(before[30:42], after[30:42]) {
		t.Fatal("salt or nonce reused")
	}
	return e, after, ft
}

func TestRotatePassphrase(t *testing.T) {
	gh := github("Sriram-PR")
	e, after, ft := rotate(t, gh, oldPass, "p", "n", newPass, newPass)
	if _, err := vault.Open(after, []byte(oldPass)); !errors.Is(err, vault.ErrWrongPassphrase) {
		t.Fatalf("old passphrase: %v", err)
	}
	s := openVault(t, e.home, []byte(newPass))
	if string(s.Token) != testToken || s.Login != "Sriram-PR" || !s.Created.Equal(time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("vault holds %+v", s)
	}
	if len(gh.reqs) != 0 {
		t.Fatalf("%d requests", len(gh.reqs))
	}
	if !strings.Contains(ft.shown(), "The vault holds a write token for Sriram-PR.") {
		t.Fatalf("terminal:\n%s", ft.shown())
	}
}

func TestRotateToken(t *testing.T) {
	gh := github("Sriram-PR")
	e, _, _ := rotate(t, gh, oldPass, "t", newToken)
	s := openVault(t, e.home, []byte(oldPass))
	exp := time.Date(2027, 1, 16, 8, 0, 0, 0, time.UTC)
	if string(s.Token) != newToken || !s.Created.Equal(e.now()) || s.Expires == nil || !s.Expires.Equal(exp) {
		t.Fatalf("vault holds %+v", s)
	}
	if len(gh.reqs) != 1 || gh.reqs[0].Header.Get("Authorization") != "Bearer "+newToken {
		t.Fatalf("requests %v", gh.reqs)
	}
}

func TestRotateBoth(t *testing.T) {
	e, _, _ := rotate(t, github("Sriram-PR"), oldPass, "b", newToken, "n", newPass, newPass)
	if s := openVault(t, e.home, []byte(newPass)); string(s.Token) != newToken {
		t.Fatalf("vault holds %+v", s)
	}
}

func TestRotateRefusals(t *testing.T) {
	tests := []struct {
		name  string
		seed  bool
		login string
		ft    []any
		want  string
	}{
		{"no vault", false, "Sriram-PR", nil, "run ghv-apply init"},
		{"wrong passphrase", true, "Sriram-PR", []any{"not the passphrase at all"}, "wrong passphrase"},
		{"bad answer", true, "Sriram-PR", []any{oldPass, "x"}, "answer t, p, or b"},
		{"other account", true, "someone-else", []any{oldPass, "t", newToken}, "the new token belongs to someone-else, but the vault is for Sriram-PR"},
		{"bad new token", true, "Sriram-PR", []any{oldPass, "b", "gho_abcdefghijklmnopqrstuvwxyz0123456789"}, "classic personal access token"},
		{"mismatch", true, "Sriram-PR", []any{oldPass, "p", "n", newPass, oldPass}, "the passphrases do not match"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ft := answers(t, tt.ft...)
			e := ttyEnv(t, ft, github(tt.login))
			var before []byte
			if tt.seed {
				before = seedVault(t, e.home)
			}
			r := runWith(e, "rotate")
			if r.code != 1 || !strings.Contains(r.stderr, tt.want) || ft.unanswered() != 0 {
				t.Fatalf("exit %d, stderr %q, terminal:\n%s", r.code, r.stderr, ft.shown())
			}
			after, err := os.ReadFile(vault.Path(e.home))
			if tt.seed && !bytes.Equal(after, before) {
				t.Fatalf("vault changed: %v", err)
			}
		})
	}
}
