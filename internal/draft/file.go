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
// renders and sends from; the file is never read again.
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
	if err != nil {
		return Loaded{}, err
	}
	d, err := Decode(raw)
	if err != nil {
		return Loaded{}, err
	}
	d.ID = id
	return Loaded{Draft: d, Raw: raw, SHA256: sha256.Sum256(raw)}, nil
}

var errTooLarge = errors.New("larger than the limit")

// readRegular refuses symlinks (O_NOFOLLOW) and opens FIFOs without
// blocking (O_NONBLOCK) so the regular-file check can reject them.
func readRegular(path string, max int64) ([]byte, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if err != nil {
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
