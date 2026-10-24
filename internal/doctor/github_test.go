package doctor

import (
	"context"
	"encoding/base64"
	"errors"
	"path/filepath"
	"testing"
)

func TestSSHKeysOnGitHub(t *testing.T) {
	home := t.TempDir()
	plant(t, home, map[string]string{".ssh/a": opensshKey("none", ed25519Blob(1)), ".ssh/b": opensshKey("none", ed25519Blob(2))})
	registered := "ssh-ed25519 " + base64.StdEncoding.EncodeToString(ed25519Blob(1))
	a, b := filepath.Join(home, ".ssh/a"), filepath.Join(home, ".ssh/b")
	head := func(p string, seed byte) string {
		return "HIGH " + p + " has no passphrase (ssh-ed25519 " + fingerprint(ed25519Blob(seed)) + ")"
	}
	calls := 0
	keys := func(_ context.Context, login string) ([]string, error) {
		calls++
		if login != "octo" {
			t.Errorf("login %q", login)
		}
		return []string{registered, "ssh-rsa !!notbase64"}, nil
	}
	for _, c := range []struct {
		name string
		e    Env
		want string
	}{
		{"--login", Env{Login: "octo", UserKeys: keys},
			head(a, 1) + "; registered on GitHub for octo\n" +
				head(b, 2) + "; not among octo's keys on GitHub (deploy keys and other accounts' keys cannot be listed)\n"},
		{"the read token's login", Env{WhoAmI: func(context.Context) (string, error) { return "octo", nil }, UserKeys: keys},
			head(a, 1) + "; registered on GitHub for octo\n" +
				head(b, 2) + "; not among octo's keys on GitHub (deploy keys and other accounts' keys cannot be listed)\n"},
		{"no login", Env{WhoAmI: func(context.Context) (string, error) { return "", nil }, UserKeys: keys},
			head(a, 1) + "; not compared with GitHub: give --login LOGIN\n" +
				head(b, 2) + "; not compared with GitHub: give --login LOGIN\n"},
		{"no fallback", Env{UserKeys: keys},
			head(a, 1) + "; not compared with GitHub: give --login LOGIN\n" +
				head(b, 2) + "; not compared with GitHub: give --login LOGIN\n"},
		{"lookup fails", Env{Login: "octo", UserKeys: func(context.Context, string) ([]string, error) { return nil, errors.New("GET users/octo/keys: 404") }},
			head(a, 1) + "; not compared with GitHub: GET users/octo/keys: 404\n" +
				head(b, 2) + "; not compared with GitHub: GET users/octo/keys: 404\n"},
		{"the token's login fails", Env{WhoAmI: func(context.Context) (string, error) { return "", errors.New("token refused") }, UserKeys: keys},
			head(a, 1) + "; not compared with GitHub: token refused\n" +
				head(b, 2) + "; not compared with GitHub: token refused\n"},
	} {
		calls = 0
		c.e.Home = home
		if got := details(sshKeys(context.Background(), &c.e)); got != c.want {
			t.Errorf("%s: got:\n%s\nwant:\n%s", c.name, got, c.want)
		}
		if calls > 1 {
			t.Errorf("%s: %d lookups", c.name, calls)
		}
	}
	calls = 0
	e := &Env{Home: t.TempDir(), Login: "octo", UserKeys: keys}
	if sshKeys(context.Background(), e) != nil || calls != 0 {
		t.Fatalf("no keys, no lookup: %d calls", calls)
	}
}
