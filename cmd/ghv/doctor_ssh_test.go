package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/binary"
	"encoding/pem"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func sshStr(b []byte) []byte {
	return append(binary.BigEndian.AppendUint32(nil, uint32(len(b))), b...)
}

// plantSSHKey writes an ed25519 key without a passphrase to home's
// ~/.ssh/id_ed25519 and returns its public key line.
func plantSSHKey(t *testing.T, home string) string {
	t.Helper()
	pub := ed25519.NewKeyFromSeed(make([]byte, 32)).Public().(ed25519.PublicKey)
	blob := append(sshStr([]byte("ssh-ed25519")), sshStr(pub)...)
	b := append([]byte("openssh-key-v1\x00"), sshStr([]byte("none"))...)
	b = append(b, sshStr([]byte("none"))...)
	b = append(b, sshStr(nil)...)
	b = binary.BigEndian.AppendUint32(b, 1)
	b = append(b, sshStr(blob)...)
	b = append(b, sshStr(nil)...)
	p := filepath.Join(home, ".ssh", "id_ed25519")
	os.MkdirAll(filepath.Dir(p), 0o700)
	if err := os.WriteFile(p, pem.EncodeToMemory(&pem.Block{Type: "OPENSSH PRIVATE KEY", Bytes: b}), 0o600); err != nil {
		t.Fatal(err)
	}
	return "ssh-ed25519 " + base64.StdEncoding.EncodeToString(blob)
}

func TestDoctorSSHKeysOnGitHub(t *testing.T) {
	var paths []string
	var key string
	e, _, _ := testEnv(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		paths = append(paths, r.URL.RequestURI())
		switch r.URL.Path {
		case "/rate_limit":
			w.Header().Set("X-Oauth-Scopes", "")
			io.WriteString(w, rateBody)
		case "/user":
			io.WriteString(w, `{"login":"octo"}`)
		case "/users/octo/keys":
			io.WriteString(w, `[{"id":1,"key":"`+key+`"}]`)
		default:
			http.NotFound(w, r)
		}
	}))
	e.goos, e.uid, e.username = "linux", 1001, "agent"
	key = plantSSHKey(t, e.home)
	r := runWith(t, registry, e, "doctor", "--login", "octo")
	if !strings.Contains(r.stdout, "id_ed25519 has no passphrase (ssh-ed25519 SHA256:") || !strings.Contains(r.stdout, "; registered on GitHub for octo\n") ||
		strings.Join(paths, " ") != "/users/octo/keys?per_page=100" {
		t.Fatalf("exit %d, requests %v, stderr %q, stdout:\n%s", r.code, paths, r.stderr, r.stdout)
	}
	paths = nil
	r = runWith(t, registry, e, "doctor")
	if !strings.Contains(r.stdout, "; not compared with GitHub: give --login LOGIN\n") || paths != nil {
		t.Fatalf("anonymous: requests %v, stdout:\n%s", paths, r.stdout)
	}
	setToken(t, e, "ghp_"+strings.Repeat("x", 36))
	r = runWith(t, registry, e, "doctor")
	if !strings.Contains(r.stdout, "; registered on GitHub for octo\n") || !strings.Contains(strings.Join(paths, " "), "/user ") {
		t.Fatalf("the read token's login: requests %v, stdout:\n%s", paths, r.stdout)
	}
	if r := runWith(t, registry, e, "doctor", "--login", "not/a-login"); r.code != 2 || !strings.HasPrefix(r.stderr, "ghv: --login not/a-login is not a GitHub login\n") {
		t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
	}
}
