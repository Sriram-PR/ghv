// Package audit is ghv-apply's record of the writes it made: one JSON line
// per sent draft, which ghv-apply log --verify compares with GitHub.
package audit

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"time"
)

type Entry struct {
	Time          time.Time `json:"time"`
	Draft         string    `json:"draft"`
	Kind          string    `json:"kind"`
	Repo          string    `json:"repo"`
	RequestSHA256 string    `json:"request_sha256"`
	Status        int       `json:"status"`
	URL           string    `json:"url"`
	// Found marks a send whose answer was unclear, found on GitHub later:
	// its status and request SHA-256 are not known.
	Found bool `json:"found,omitempty"`
}

// Path is the log under home, which callers take from the passwd entry.
func Path(home string) string { return filepath.Join(home, ".local", "state", "ghv", "audit.jsonl") }

// Append adds e as one line, in one write. A symlink is refused, and a
// FIFO cannot hang it.
func Append(path string, e Entry) error {
	line, err := json.Marshal(e)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND|os.O_CREATE|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0o600)
	if err != nil {
		return err
	}
	fi, err := f.Stat()
	if err == nil && !fi.Mode().IsRegular() {
		err = fmt.Errorf("%s is not a regular file", path)
	}
	if err == nil {
		_, err = f.Write(append(line, '\n'))
	}
	if err == nil {
		err = f.Sync()
	}
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	return err
}
