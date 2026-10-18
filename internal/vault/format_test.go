package vault

import (
	"bytes"
	"crypto/rand"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"io"
	"testing"
	"time"

	"golang.org/x/crypto/argon2"
)

func testSecret() Secret {
	exp := time.Date(2027, 1, 16, 0, 0, 0, 0, time.UTC)
	return Secret{
		Login:   "Sriram-PR",
		Token:   []byte("ghp_" + "abcdefghijklmnopqrstuvwxyz0123456789"),
		Type:    "classic",
		Created: time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC),
		Expires: &exp,
	}
}

func sameSecret(t *testing.T, got, want Secret) {
	t.Helper()
	if got.Login != want.Login || !bytes.Equal(got.Token, want.Token) || got.Type != want.Type ||
		!got.Created.Equal(want.Created) || (got.Expires == nil) != (want.Expires == nil) ||
		(got.Expires != nil && !got.Expires.Equal(*want.Expires)) {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestSealOpen(t *testing.T) {
	pass := []byte("correct horse battery staple")
	file, err := Seal(testSecret(), pass, rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if string(file[:4]) != "GHV1" || file[4] != 1 {
		t.Fatalf("header starts %q", file[:5])
	}
	got, err := Open(file, pass)
	if err != nil {
		t.Fatal(err)
	}
	sameSecret(t, got, testSecret())
	got.Zero()
	if !bytes.Equal(got.Token, make([]byte, len(got.Token))) {
		t.Fatal("Zero left token bytes")
	}
	if _, err := Open(file, []byte("correct horse battery stapler")); !errors.Is(err, ErrWrongPassphrase) {
		t.Fatalf("wrong passphrase: %v", err)
	}
}

func TestSealWithoutExpiry(t *testing.T) {
	s := testSecret()
	s.Expires = nil
	key := make([]byte, keyLen)
	file, err := sealWithKey(s, key, rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	h, err := parseHeader(file)
	if err != nil {
		t.Fatal(err)
	}
	got, err := openWithKey(file, h, key)
	if err != nil {
		t.Fatal(err)
	}
	sameSecret(t, got, s)
}

func sealWithKey(s Secret, key []byte, r io.Reader) ([]byte, error) {
	salt := make([]byte, saltLen)
	if _, err := io.ReadFull(r, salt); err != nil {
		return nil, err
	}
	return seal(s, salt, key, r)
}

// Offsets into the header of a vault for login "Sriram-PR".
const (
	offKDF    = 4
	offMemory = 5
	offTime   = 9
	offLanes  = 13
	offSalt   = 14
	offNonce  = 30
	offLogin  = 42
)

func TestParametersRefusedBeforeTheKDF(t *testing.T) {
	file, err := sealWithKey(testSecret(), make([]byte, keyLen), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	for name, edit := range map[string]func(b []byte){
		"magic":  func(b []byte) { b[0] = 'X' },
		"kdf":    func(b []byte) { b[offKDF] = 2 },
		"memory": func(b []byte) { binary.BigEndian.PutUint32(b[offMemory:], 0xFFFFFFFF) },
		"time":   func(b []byte) { binary.BigEndian.PutUint32(b[offTime:], 1<<31) },
		"lanes":  func(b []byte) { b[offLanes] = 255 },
		"login":  func(b []byte) { b[offLogin] = 40 },
	} {
		b := bytes.Clone(file)
		edit(b)
		// Running Argon2id with 4 TiB of memory or 2^31 passes would
		// crash or hang this test; Open must refuse first.
		if _, err := Open(b, []byte("x")); !errors.Is(err, ErrFormat) {
			t.Errorf("%s: %v, want ErrFormat", name, err)
		}
	}
	for _, n := range []int{0, offLogin, offLogin + 1 + len("Sriram-PR") + 15} {
		if _, err := Open(file[:n], []byte("x")); !errors.Is(err, ErrFormat) {
			t.Errorf("truncated to %d: %v, want ErrFormat", n, err)
		}
	}
}

func TestHeaderIsAuthenticated(t *testing.T) {
	key := make([]byte, keyLen)
	file, err := sealWithKey(testSecret(), key, rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	for name, off := range map[string]int{
		"salt":         offSalt,
		"nonce":        offNonce,
		"login":        offLogin + 1,
		"ciphertext":   len(file) - 20,
		"tag":          len(file) - 1,
		"login change": offLogin + 1 + len("Sriram-P"),
	} {
		b := bytes.Clone(file)
		b[off] ^= 0x01
		h, err := parseHeader(b)
		if err != nil {
			t.Errorf("%s: parse: %v", name, err)
			continue
		}
		if _, err := openWithKey(b, h, key); !errors.Is(err, ErrWrongPassphrase) {
			t.Errorf("%s: %v, want ErrWrongPassphrase", name, err)
		}
	}
}

func TestSealRefusesBadSecrets(t *testing.T) {
	for name, edit := range map[string]func(*Secret){
		"bad login":     func(s *Secret) { s.Login = "-x" },
		"long login":    func(s *Secret) { s.Login = "a234567890123456789012345678901234567890" },
		"empty token":   func(s *Secret) { s.Token = nil },
		"newline token": func(s *Secret) { s.Token = []byte("ghp_a\nb") },
		"newline type":  func(s *Secret) { s.Type = "classic\n" },
	} {
		s := testSecret()
		edit(&s)
		if _, err := sealWithKey(s, make([]byte, keyLen), rand.Reader); err == nil {
			t.Errorf("%s: sealed", name)
		}
	}
}

// The reference implementation's Argon2id vector (phc-winner-argon2,
// test.c), so the KDF is Argon2id version 0x13. RFC 9106's vectors use a
// secret and associated data, which argon2.IDKey does not take.
func TestArgon2idReferenceVector(t *testing.T) {
	got := hex.EncodeToString(argon2.IDKey([]byte("password"), []byte("somesalt"), 2, 1<<16, 1, 32))
	if want := "09316115d5cf24ed5a15a31a3ba326e5cf32edc24702987c02b6566f61913cf7"; got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
}

// Pins deriveKey to v1's parameters: 256 MiB, 3 passes, 4 lanes.
func TestDeriveKeyV1(t *testing.T) {
	var salt [saltLen]byte
	got := hex.EncodeToString(deriveKey([]byte("pw"), salt[:]))
	if want := "2c547c943134da5c3697f51575cd311dc1bfa7694b2b895d8a897170c76f4b15"; got != want {
		t.Fatalf("got %s, want %s", got, want)
	}
}
