package vault

import (
	"crypto/rand"
	"errors"
	"testing"
)

func TestLogin(t *testing.T) {
	file, err := sealWithKey(testSecret(), make([]byte, keyLen), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	if login, err := Login(file); err != nil || login != "Sriram-PR" {
		t.Fatalf("%q, %v", login, err)
	}
	bad := append([]byte("XHV1"), file[4:]...)
	if _, err := Login(bad); !errors.Is(err, ErrFormat) {
		t.Fatalf("bad magic: %v", err)
	}
	if _, err := Login(file[:20]); !errors.Is(err, ErrFormat) {
		t.Fatalf("truncated: %v", err)
	}
}
