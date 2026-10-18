package draft

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"syscall"
)

const MaxSize = 1 << 20

// Loaded is one draft as read from disk. Raw is the only copy ghv-apply
// renders, and Draft, decoded from it, the one it sends from unless the
// reviewer edits it; the file is read again only to check its SHA-256 on a
// move. A draft that does not decode still has Raw and SHA256, and one too
// large to read still has SHA256, so either can be moved out of the queue.
type Loaded struct {
	Draft  Draft
	Raw    []byte
	SHA256 [32]byte
}

func ReadFile(path string) (Loaded, error) {
	name := filepath.Base(path)
	id, ok := strings.CutSuffix(name, ".json")
	if !ok || !idRE.MatchString(id) {
		return Loaded{}, invalid("file name %q is not d-<12 hex>.json", name)
	}
	raw, err := readRegular(path, MaxSize)
	if errors.Is(err, errTooLarge) {
		sum, herr := hashRegular(path)
		if herr != nil {
			return Loaded{}, err
		}
		return Loaded{SHA256: sum}, err
	}
	if err != nil {
		return Loaded{}, err
	}
	l := Loaded{Raw: raw, SHA256: sha256.Sum256(raw)}
	d, err := Decode(raw)
	if err != nil {
		return l, err
	}
	d.ID = id
	l.Draft = d
	return l, nil
}

var errTooLarge = errors.New("larger than the limit")

// openRegular refuses symlinks (O_NOFOLLOW) and opens FIFOs without
// blocking (O_NONBLOCK) so the regular-file check can reject them.
func openRegular(path string) (*os.File, os.FileInfo, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, nil, err
	}
	fi, err := f.Stat()
	if err != nil {
		f.Close()
		return nil, nil, err
	}
	if !fi.Mode().IsRegular() {
		f.Close()
		return nil, nil, fmt.Errorf("%s is not a regular file", path)
	}
	return f, fi, nil
}

// hashRegular streams the file, so its size does not matter.
func hashRegular(path string) ([32]byte, error) {
	f, _, err := openRegular(path)
	if err != nil {
		return [32]byte{}, err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return [32]byte{}, err
	}
	return [32]byte(h.Sum(nil)), nil
}

func readRegular(path string, max int64) ([]byte, error) {
	f, fi, err := openRegular(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if fi.Size() > max {
		return nil, fmt.Errorf("%s: %w (%d bytes)", path, errTooLarge, max)
	}
	data, err := io.ReadAll(io.LimitReader(f, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > max {
		return nil, fmt.Errorf("%s: %w (%d bytes)", path, errTooLarge, max)
	}
	return data, nil
}
