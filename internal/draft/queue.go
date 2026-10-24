package draft

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

type State string

const (
	Pending  State = "pending"
	Unknown  State = "unknown"
	Applied  State = "applied"
	Rejected State = "rejected"
)

var stateDirs = map[State]string{
	Pending:  "queue",
	Unknown:  "unknown",
	Applied:  "applied",
	Rejected: "rejected",
}

// Queue is the draft store at Root: the shared queue, or the home queue
// (~/.local/share/ghv) under the passwd home, never $HOME. Locate picks it.
type Queue struct {
	Root   string
	shared bool
	gid    uint32
}

func (q Queue) Dir(s State) string { return filepath.Join(q.Root, stateDirs[s]) }

func (q Queue) ensureDir(s State) (string, error) {
	// Setup made the shared queue's directories, and Locate checked them.
	if q.shared {
		return q.Dir(s), nil
	}
	if err := os.MkdirAll(filepath.Dir(q.Root), 0o700); err != nil {
		return "", err
	}
	for _, p := range []string{q.Root, q.Dir(s)} {
		if err := os.Mkdir(p, 0o700); err != nil && !errors.Is(err, fs.ErrExist) {
			return "", err
		}
		fi, err := os.Lstat(p)
		if err != nil {
			return "", err
		}
		if !fi.IsDir() {
			return "", fmt.Errorf("%s is not a directory", p)
		}
		if err := os.Chmod(p, 0o700); err != nil {
			return "", err
		}
	}
	return q.Dir(s), nil
}

// Write stores d as a new pending draft. The file appears complete or not
// at all: it is written to a temp file, then hard-linked into place, which
// also refuses to replace an existing draft.
func (q Queue) Write(d Draft) (string, error) {
	id, err := q.write(d)
	return id, q.permission(err)
}

func (q Queue) write(d Draft) (string, error) {
	data, err := Encode(d)
	if err != nil {
		return "", err
	}
	if len(data) > MaxSize {
		return "", invalid("draft is larger than %d bytes", MaxSize)
	}
	if _, err := Decode(data); err != nil {
		return "", err
	}
	dir, err := q.ensureDir(Pending)
	if err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return "", err
	}
	defer os.Remove(tmp.Name())
	if err := tmp.Chmod(q.fileMode()); err != nil {
		tmp.Close()
		return "", err
	}
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return "", err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return "", err
	}
	if err := tmp.Close(); err != nil {
		return "", err
	}
	for range 3 {
		id := newID()
		err := os.Link(tmp.Name(), filepath.Join(dir, id+".json"))
		if err == nil {
			return id, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return "", err
		}
	}
	return "", errors.New("could not pick an unused draft ID")
}

func newID() string {
	b := make([]byte, 6)
	rand.Read(b)
	return "d-" + hex.EncodeToString(b)
}
