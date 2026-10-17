// Package archive reads GitHub's zip downloads without trusting them: entry
// names must be local, only regular files and directories are accepted,
// and the size caps count the bytes actually written.
package archive

import (
	"archive/zip"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

var (
	ErrUnsafe   = errors.New("unsafe zip entry")
	ErrTooLarge = errors.New("the archive is over its limit")
)

type Limits struct {
	MaxBytes int64
	MaxFiles int
}

type File struct {
	Name string
	Data []byte
}

func open(ra io.ReaderAt, size int64) (*zip.Reader, error) {
	zr, err := zip.NewReader(ra, size)
	if errors.Is(err, zip.ErrInsecurePath) {
		return nil, fmt.Errorf("%w: %v", ErrUnsafe, err)
	}
	if err != nil {
		return nil, fmt.Errorf("not a zip archive: %w", err)
	}
	for _, f := range zr.File {
		if _, _, err := check(f); err != nil {
			return nil, err
		}
	}
	return zr, nil
}

// check returns the entry's cleaned name and whether it is a directory.
// archive/zip checks names only under GODEBUG=zipinsecurepath=0, which is
// not the default, so this is the check that counts.
func check(f *zip.File) (string, bool, error) {
	name := strings.TrimSuffix(f.Name, "/")
	mode := f.Mode()
	switch {
	case !filepath.IsLocal(name) || strings.Contains(name, `\`):
		return "", false, fmt.Errorf("%w: %q is not a local path", ErrUnsafe, f.Name)
	case mode&fs.ModeSymlink != 0:
		return "", false, fmt.Errorf("%w: %q is a symlink", ErrUnsafe, f.Name)
	case mode.IsDir() || strings.HasSuffix(f.Name, "/"):
		return path.Clean(name), true, nil
	case !mode.IsRegular():
		return "", false, fmt.Errorf("%w: %q is not a regular file", ErrUnsafe, f.Name)
	}
	return path.Clean(name), false, nil
}

// Extract writes the archive's files into dir, which it creates and which
// must not exist yet, through an os.Root, so nothing lands outside it. No
// file is overwritten and none is executable. On any error dir is removed.
// It returns the files written, in archive order, and their total size.
func Extract(ra io.ReaderAt, size int64, dir string, lim Limits) (files []string, written int64, err error) {
	zr, err := open(ra, size)
	if err != nil {
		return nil, 0, err
	}
	if len(zr.File) > lim.MaxFiles {
		return nil, 0, fmt.Errorf("%w: %d entries, over %d", ErrTooLarge, len(zr.File), lim.MaxFiles)
	}
	if err := os.Mkdir(dir, 0o755); errors.Is(err, fs.ErrExist) {
		return nil, 0, fmt.Errorf("%s already exists; give a new directory", dir)
	} else if err != nil {
		return nil, 0, err
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		os.Remove(dir)
		return nil, 0, err
	}
	defer func() {
		root.Close()
		if err != nil {
			os.RemoveAll(dir)
			files, written = nil, 0
		}
	}()
	for _, f := range zr.File {
		name, isDir, _ := check(f)
		if isDir {
			if err := root.MkdirAll(name, 0o755); err != nil {
				return nil, 0, err
			}
			continue
		}
		if d := path.Dir(name); d != "." {
			if err := root.MkdirAll(d, 0o755); err != nil {
				return nil, 0, err
			}
		}
		n, err := extractFile(root, f, name, lim.MaxBytes-written)
		written += n
		if errors.Is(err, ErrTooLarge) {
			return nil, 0, fmt.Errorf("%w: over %d bytes", ErrTooLarge, lim.MaxBytes)
		}
		if err != nil {
			return nil, 0, err
		}
		files = append(files, name)
	}
	return files, written, nil
}

func extractFile(root *os.Root, f *zip.File, name string, room int64) (int64, error) {
	out, err := root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if errors.Is(err, fs.ErrExist) {
		return 0, fmt.Errorf("%w: %q appears twice", ErrUnsafe, f.Name)
	}
	if err != nil {
		return 0, err
	}
	defer out.Close()
	rc, err := f.Open()
	if err != nil {
		return 0, err
	}
	defer rc.Close()
	n, err := io.Copy(out, io.LimitReader(rc, room+1))
	if n > room {
		return n, ErrTooLarge
	}
	if err != nil {
		return n, err
	}
	return n, out.Close()
}

// ReadFiles reads the regular files keep accepts, in archive order, into
// memory, up to maxBytes in total. Every entry's name is checked, kept or
// not.
func ReadFiles(ra io.ReaderAt, size int64, keep func(name string) bool, maxBytes int64) ([]File, error) {
	zr, err := open(ra, size)
	if err != nil {
		return nil, err
	}
	var out []File
	var total int64
	for _, f := range zr.File {
		name, isDir, _ := check(f)
		if isDir || !keep(name) {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return nil, err
		}
		data, err := io.ReadAll(io.LimitReader(rc, maxBytes-total+1))
		rc.Close()
		total += int64(len(data))
		if total > maxBytes {
			return nil, fmt.Errorf("%w: over %d bytes", ErrTooLarge, maxBytes)
		}
		if err != nil {
			return nil, err
		}
		out = append(out, File{name, data})
	}
	return out, nil
}
