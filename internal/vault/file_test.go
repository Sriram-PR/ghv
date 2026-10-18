package vault

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestPath(t *testing.T) {
	if got := Path("/home/u"); got != "/home/u/.config/ghv/vault" {
		t.Fatal(got)
	}
}

func onlyFile(t *testing.T, dir string) {
	t.Helper()
	ents, err := os.ReadDir(dir)
	if err != nil || len(ents) != 1 || ents[0].Name() != "vault" {
		t.Fatalf("directory holds %v, %v", ents, err)
	}
}

func TestCreateLoad(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "ghv")
	path := filepath.Join(dir, "vault")
	if _, err := Load(path); !errors.Is(err, ErrNoVault) || !strings.Contains(err.Error(), "ghv-apply init") {
		t.Fatalf("missing vault: %v", err)
	}
	if err := Create(path, []byte("one")); err != nil {
		t.Fatal(err)
	}
	if fi, err := os.Stat(dir); err != nil || fi.Mode().Perm() != 0o700 {
		t.Fatalf("dir mode %v, %v", fi.Mode(), err)
	}
	if fi, err := os.Stat(path); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("file mode %v, %v", fi.Mode(), err)
	}
	if got, err := Load(path); err != nil || string(got) != "one" {
		t.Fatalf("Load = %q, %v", got, err)
	}
	if err := Create(path, []byte("two")); !errors.Is(err, ErrExists) || !strings.Contains(err.Error(), "ghv-apply rotate") {
		t.Fatalf("second Create: %v", err)
	}
	if got, _ := Load(path); string(got) != "one" {
		t.Fatalf("second Create changed the vault to %q", got)
	}
	onlyFile(t, dir)
}

func TestReplace(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "vault")
	if err := Replace(path, []byte("x")); !errors.Is(err, ErrNoVault) {
		t.Fatalf("Replace without a vault: %v", err)
	}
	if err := Create(path, []byte("old")); err != nil {
		t.Fatal(err)
	}
	if err := Replace(path, []byte("new")); err != nil {
		t.Fatal(err)
	}
	if got, err := Load(path); err != nil || string(got) != "new" {
		t.Fatalf("Load = %q, %v", got, err)
	}
	if fi, _ := os.Stat(path); fi.Mode().Perm() != 0o600 {
		t.Fatalf("mode %v", fi.Mode())
	}
	onlyFile(t, dir)
}

func TestLoadRefusals(t *testing.T) {
	dir := t.TempDir()
	real := filepath.Join(dir, "real")
	if err := os.WriteFile(real, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(dir, "link")
	if err := os.Symlink(real, link); err != nil {
		t.Fatal(err)
	}
	fifo := filepath.Join(dir, "fifo")
	if err := syscall.Mkfifo(fifo, 0o600); err != nil {
		t.Fatal(err)
	}
	open := filepath.Join(dir, "open")
	if err := os.WriteFile(open, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	big := filepath.Join(dir, "big")
	if err := os.WriteFile(big, bytes.Repeat([]byte("x"), maxFile+1), 0o600); err != nil {
		t.Fatal(err)
	}
	for path, want := range map[string]string{
		link: "symbolic link",
		fifo: "not a regular file",
		open: "chmod 600",
		big:  "over 4096 bytes",
	} {
		done := make(chan error, 1)
		go func() { _, err := Load(path); done <- err }()
		select {
		case err := <-done:
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Errorf("%s: %v, want %q", filepath.Base(path), err, want)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("%s: Load hung", filepath.Base(path))
		}
	}
	if err := Replace(link, []byte("y")); err == nil {
		t.Error("Replace followed or replaced a symlink")
	}
	if got, _ := os.ReadFile(real); string(got) != "x" {
		t.Errorf("symlink target changed to %q", got)
	}
}
