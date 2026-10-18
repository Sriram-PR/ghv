package vault

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
)

const maxFile = 4096

var (
	ErrNoVault = errors.New("no vault")
	ErrExists  = errors.New("a vault already exists")
)

// Path is the vault under home, which callers take from the passwd entry.
func Path(home string) string { return filepath.Join(home, ".config", "ghv", "vault") }

// Load reads the vault without following a symlink or blocking on a FIFO.
func Load(path string) ([]byte, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil, fmt.Errorf("%w at %s; run ghv-apply init", ErrNoVault, path)
	case errors.Is(err, syscall.ELOOP):
		return nil, fmt.Errorf("%s is a symbolic link", path)
	case err != nil:
		return nil, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", path)
	}
	if fi.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("%s is readable by other users; run chmod 600 %s", path, path)
	}
	data, err := io.ReadAll(io.LimitReader(f, maxFile+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxFile {
		return nil, fmt.Errorf("%s is over %d bytes", path, maxFile)
	}
	return data, nil
}

// Create writes a new vault, creating its directory with mode 700. It
// never replaces one: the file appears by link(2), which fails if the
// name exists.
func Create(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := writeTemp(dir, data)
	if err != nil {
		return err
	}
	defer os.Remove(tmp)
	if err := os.Link(tmp, path); errors.Is(err, fs.ErrExist) {
		return fmt.Errorf("%w at %s; use ghv-apply rotate", ErrExists, path)
	} else if err != nil {
		return err
	}
	return syncDir(dir)
}

// Replace swaps an existing vault for data by rename(2).
func Replace(path string, data []byte) error {
	fi, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("%w at %s; run ghv-apply init", ErrNoVault, path)
	}
	if err != nil {
		return err
	}
	if !fi.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file", path)
	}
	dir := filepath.Dir(path)
	tmp, err := writeTemp(dir, data)
	if err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		os.Remove(tmp)
		return err
	}
	return syncDir(dir)
}

// writeTemp writes data to a new mode-600 file in dir and syncs it.
func writeTemp(dir string, data []byte) (string, error) {
	f, err := os.CreateTemp(dir, ".vault-*")
	if err != nil {
		return "", err
	}
	_, err = f.Write(data)
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}

func syncDir(dir string) error {
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
