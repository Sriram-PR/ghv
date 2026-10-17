package draft

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"
)

type Outcome struct {
	At     time.Time `json:"at"`
	Status int       `json:"status,omitempty"`
	URL    string    `json:"url,omitempty"`
	Reason string    `json:"reason,omitempty"`
}

type Entry struct {
	ID      string
	State   State
	Outcome *Outcome
}

// Applied is reachable only from Unknown: ghv-apply marks a draft Unknown
// before sending, so a crash mid-send never leaves it looking unsent.
var transitions = map[State][]State{
	Pending: {Unknown, Rejected},
	Unknown: {Applied, Rejected, Pending},
}

const maxOutcomeSize = 64 << 10

// Move renames the draft before writing its outcome: a crash in between
// leaves the draft in the right state, only without details.
func (q Queue) Move(id string, from, to State, o Outcome) error {
	if !idRE.MatchString(id) {
		return invalid("bad draft ID %q", id)
	}
	if !slices.Contains(transitions[from], to) {
		return fmt.Errorf("draft %s: cannot move from %s to %s", id, from, to)
	}
	src := filepath.Join(q.Dir(from), id+".json")
	dstDir, err := q.ensureDir(to)
	if err != nil {
		return err
	}
	dst := filepath.Join(dstDir, id+".json")
	fi, err := os.Lstat(src)
	if err != nil {
		return err
	}
	if !fi.Mode().IsRegular() {
		return fmt.Errorf("%s is not a regular file", src)
	}
	if _, err := os.Lstat(dst); err == nil {
		return fmt.Errorf("%s already exists", dst)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if err := os.Rename(src, dst); err != nil {
		return err
	}
	// The draft has moved; a leftover sidecar in the old directory is harmless.
	os.Remove(outcomePath(q.Dir(from), id))
	return writeOutcome(dstDir, id, o)
}

func outcomePath(dir, id string) string { return filepath.Join(dir, id+".outcome.json") }

func writeOutcome(dir, id string, o Outcome) error {
	data, err := json.Marshal(o)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".tmp-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), outcomePath(dir, id))
}

func readOutcome(dir, id string) *Outcome {
	data, err := readRegular(outcomePath(dir, id), maxOutcomeSize)
	if err != nil {
		return nil
	}
	var o Outcome
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&o); err != nil {
		return nil
	}
	return &o
}

func (q Queue) List() ([]Entry, error) {
	var out []Entry
	for _, s := range []State{Pending, Unknown, Applied, Rejected} {
		ents, err := os.ReadDir(q.Dir(s))
		if errors.Is(err, fs.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, err
		}
		for _, e := range ents {
			id, ok := strings.CutSuffix(e.Name(), ".json")
			if !ok || !idRE.MatchString(id) || !e.Type().IsRegular() {
				continue
			}
			out = append(out, Entry{ID: id, State: s, Outcome: readOutcome(q.Dir(s), id)})
		}
	}
	slices.SortFunc(out, func(a, b Entry) int { return strings.Compare(a.ID, b.ID) })
	return out, nil
}
