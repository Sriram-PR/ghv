// Package audit is ghv-apply's record of the writes it made: one JSON line
// per sent draft, which ghv-apply log --verify compares with GitHub.
package audit

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
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

// maxLine bounds one line; a sent draft's line is a few hundred bytes.
const maxLine = 64 << 10

// Read returns the log's lines in order, none when there is no log. The
// agent's user can edit the file, so it is read strictly: a symlink, an
// unknown field, or a line that does not decode is an error.
func Read(path string) ([]Entry, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	if fi, err := f.Stat(); err != nil {
		return nil, err
	} else if !fi.Mode().IsRegular() {
		return nil, fmt.Errorf("%s is not a regular file", path)
	}
	var es []Entry
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 4096), maxLine)
	for n := 1; sc.Scan(); n++ {
		dec := json.NewDecoder(bytes.NewReader(sc.Bytes()))
		dec.DisallowUnknownFields()
		var e Entry
		if err := dec.Decode(&e); err != nil {
			return nil, fmt.Errorf("audit log line %d: %w", n, err)
		}
		es = append(es, e)
	}
	if err := sc.Err(); err != nil {
		return nil, fmt.Errorf("audit log line %d: %w", len(es)+1, err)
	}
	return es, nil
}
