package draft

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

const okName = "d-0123456789ab.json"

func put(t *testing.T, dir, name string, data []byte) string {
	t.Helper()
	p := filepath.Join(dir, name)
	if err := os.WriteFile(p, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestReadFile(t *testing.T) {
	raw := readGolden(t, "comment")
	p := put(t, t.TempDir(), okName, raw)
	l, err := ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	want := golden["comment"]
	want.ID = "d-0123456789ab"
	if !equalDraft(l.Draft, want) {
		t.Fatalf("Draft = %+v, want %+v", l.Draft, want)
	}
	if !bytes.Equal(l.Raw, raw) || l.SHA256 != sha256.Sum256(raw) {
		t.Fatal("Raw or SHA256 does not match the file")
	}
}

func TestReadFileIDFromName(t *testing.T) {
	in := `{"id":"d-ffffffffffff","schema":1,"kind":"comment","repo":"o/r","created_at":"2026-10-16T05:05:12Z","fields":{"number":5,"body":"hi"}}`
	l, err := ReadFile(put(t, t.TempDir(), okName, []byte(in)))
	if err != nil {
		t.Fatal(err)
	}
	if l.Draft.ID != "d-0123456789ab" {
		t.Fatalf("ID = %q, want d-0123456789ab", l.Draft.ID)
	}
}

func TestReadFileRejectsBadNames(t *testing.T) {
	raw := readGolden(t, "comment")
	for _, name := range []string{"x.json", "d-0123.json", "d-0123456789AB.json", "d-0123456789ab.json.bak", "d-0123456789ab"} {
		_, err := ReadFile(put(t, t.TempDir(), name, raw))
		if !errors.Is(err, ErrInvalid) {
			t.Errorf("ReadFile(%s) = %v, want ErrInvalid", name, err)
		}
	}
}

func TestReadFileRejectsSymlink(t *testing.T) {
	dir := t.TempDir()
	target := put(t, dir, "elsewhere.json", readGolden(t, "comment"))
	link := filepath.Join(dir, okName)
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadFile(link); !errors.Is(err, syscall.ELOOP) {
		t.Fatalf("err = %v, want ELOOP", err)
	}
}

func TestReadFileRejectsFIFO(t *testing.T) {
	p := filepath.Join(t.TempDir(), okName)
	if err := syscall.Mkfifo(p, 0o600); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { _, err := ReadFile(p); done <- err }()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "not a regular file") {
			t.Fatalf("err = %v, want 'not a regular file'", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("ReadFile blocked on a FIFO")
	}
}

func TestReadFileRejectsDirectory(t *testing.T) {
	p := filepath.Join(t.TempDir(), okName)
	if err := os.Mkdir(p, 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := ReadFile(p); err == nil || !strings.Contains(err.Error(), "not a regular file") {
		t.Fatalf("err = %v, want 'not a regular file'", err)
	}
}

func TestReadFileSizeLimit(t *testing.T) {
	raw := readGolden(t, "comment")
	exact := append(bytes.Clone(raw), bytes.Repeat([]byte(" "), MaxSize-len(raw))...)
	if _, err := ReadFile(put(t, t.TempDir(), okName, exact)); err != nil {
		t.Fatalf("file of exactly MaxSize: %v", err)
	}
	over := append(exact, ' ')
	if _, err := ReadFile(put(t, t.TempDir(), okName, over)); err == nil || !strings.Contains(err.Error(), "larger than") {
		t.Fatalf("file over MaxSize: err = %v", err)
	}
}

func TestReadFileKeepsWhatItRead(t *testing.T) {
	raw := readGolden(t, "comment")
	p := put(t, t.TempDir(), okName, raw)
	l, err := ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, readGolden(t, "issue.create"), 0o600); err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(l.Raw, raw) || l.Draft.Kind() != KindComment {
		t.Fatal("Loaded changed after the file was rewritten")
	}
}
