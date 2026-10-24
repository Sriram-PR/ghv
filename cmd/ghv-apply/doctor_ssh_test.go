package main

import (
	"crypto/ed25519"
	"encoding/base64"
	"encoding/binary"
	"encoding/pem"
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
	e, f := doctorTestEnv(t, "")
	key := plantSSHKey(t, e.home)
	f.routes["/users/octo/keys?per_page=100"] = apiReply{status: 200, body: `[{"id":1,"key":"` + key + `"}]`}
	r := runWith(e, "doctor", "--human-user", "sriram", "--login", "octo")
	if !strings.Contains(r.stdout, "id_ed25519 has no passphrase (ssh-ed25519 SHA256:") || !strings.Contains(r.stdout, "; registered on GitHub for octo\n") ||
		len(f.reqs) != 2 || f.reqs[1].Header.Get("Authorization") != "" {
		t.Fatalf("exit %d, %d requests, stderr %q, stdout:\n%s", r.code, len(f.reqs), r.stderr, r.stdout)
	}
	f.reqs = nil
	r = runWith(e, "doctor", "--human-user", "sriram")
	if !strings.Contains(r.stdout, "; not compared with GitHub: give --login LOGIN\n") || len(f.reqs) != 1 || f.reqs[0].URL.Path != "/rate_limit" {
		t.Fatalf("without --login: %d requests, stdout:\n%s", len(f.reqs), r.stdout)
	}
	if r := runWith(e, "doctor", "--login", "not/a-login"); r.code != 1 || r.stderr != "ghv-apply: --login not/a-login is not a GitHub login\n" {
		t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
	}
}
