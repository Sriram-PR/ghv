package draft

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func entries(t *testing.T, q Queue) []Entry {
	t.Helper()
	es, err := q.List()
	if err != nil {
		t.Fatal(err)
	}
	return es
}

// sumOf is the SHA-256 of a draft's file as it is now.
func sumOf(t *testing.T, q Queue, id string, s State) [32]byte {
	t.Helper()
	l, err := ReadFile(filepath.Join(q.Dir(s), id+".json"))
	if err != nil {
		t.Fatal(err)
	}
	return l.SHA256
}

func TestLifecycle(t *testing.T) {
	q := newQueue(t)
	id, err := q.Write(golden["comment"])
	if err != nil {
		t.Fatal(err)
	}
	if es := entries(t, q); len(es) != 1 || es[0].State != Pending || es[0].Outcome != nil {
		t.Fatalf("after Write: %+v", es)
	}
	sent := time.Date(2026, 10, 17, 9, 0, 0, 0, time.UTC)
	sum := sumOf(t, q, id, Pending)
	if err := q.Move(id, Pending, Unknown, sum, Outcome{At: sent, Reason: "sending"}); err != nil {
		t.Fatal(err)
	}
	if es := entries(t, q); len(es) != 1 || es[0].State != Unknown || es[0].Outcome == nil || es[0].Outcome.Reason != "sending" {
		t.Fatalf("after Pending→Unknown: %+v", es)
	}
	url := "https://github.com/reticlehq/reticle/pull/1638#issuecomment-1"
	if err := q.Move(id, Unknown, Applied, sum, Outcome{At: sent, Status: 201, URL: url, FullName: "o/r", Edited: true, Note: "n"}); err != nil {
		t.Fatal(err)
	}
	es := entries(t, q)
	if len(es) != 1 || es[0].ID != id || es[0].State != Applied || es[0].Outcome == nil {
		t.Fatalf("after Unknown→Applied: %+v", es)
	}
	if o := es[0].Outcome; o.Status != 201 || o.URL != url || !o.At.Equal(sent) || o.FullName != "o/r" || !o.Edited || o.Note != "n" {
		t.Fatalf("outcome %+v", o)
	}
	if _, err := os.Stat(filepath.Join(q.Dir(Unknown), id+".outcome.json")); !os.IsNotExist(err) {
		t.Fatalf("stale outcome left in unknown/: %v", err)
	}
	if _, err := ReadFile(filepath.Join(q.Dir(Applied), id+".json")); err != nil {
		t.Fatalf("applied draft unreadable: %v", err)
	}
}

func TestMoveRejectsTransitions(t *testing.T) {
	q := newQueue(t)
	id, err := q.Write(golden["comment"])
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range [][2]State{{Pending, Applied}, {Pending, Pending}, {Applied, Pending}, {Rejected, Pending}, {Applied, Rejected}} {
		if err := q.Move(id, m[0], m[1], sumOf(t, q, id, Pending), Outcome{}); err == nil {
			t.Errorf("Move %s→%s allowed", m[0], m[1])
		}
	}
	if _, err := os.Stat(filepath.Join(q.Dir(Pending), id+".json")); err != nil {
		t.Fatalf("draft left pending/: %v", err)
	}
}

func TestMoveRefusesOverwrite(t *testing.T) {
	q := newQueue(t)
	id, err := q.Write(golden["comment"])
	if err != nil {
		t.Fatal(err)
	}
	if _, err := q.ensureDir(Unknown); err != nil {
		t.Fatal(err)
	}
	planted := filepath.Join(q.Dir(Unknown), id+".json")
	if err := os.WriteFile(planted, []byte("planted"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := q.Move(id, Pending, Unknown, sumOf(t, q, id, Pending), Outcome{}); err == nil {
		t.Fatal("Move overwrote an existing file")
	}
	if data, _ := os.ReadFile(planted); string(data) != "planted" {
		t.Fatal("planted file changed")
	}
	if _, err := os.Stat(filepath.Join(q.Dir(Pending), id+".json")); err != nil {
		t.Fatalf("draft left pending/: %v", err)
	}
}

func TestMoveRefusesAChangedFile(t *testing.T) {
	q := newQueue(t)
	id, err := q.Write(golden["comment"])
	if err != nil {
		t.Fatal(err)
	}
	sum := sumOf(t, q, id, Pending)
	path := filepath.Join(q.Dir(Pending), id+".json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	edited := bytes.Replace(data, []byte(`"body": "`), []byte(`"body": "edited `), 1)
	if bytes.Equal(edited, data) {
		t.Fatal("the test did not change the draft")
	}
	if err := os.WriteFile(path, edited, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := q.Move(id, Pending, Unknown, sum, Outcome{}); !errors.Is(err, ErrChanged) {
		t.Fatalf("Move of a changed file: %v", err)
	}
	if es := entries(t, q); len(es) != 1 || es[0].State != Pending {
		t.Fatalf("after a refused Move: %+v", es)
	}

	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	other := filepath.Join(t.TempDir(), "x.json")
	if err := os.WriteFile(other, data, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(other, path); err != nil {
		t.Fatal(err)
	}
	if err := q.Move(id, Pending, Unknown, sum, Outcome{}); err == nil {
		t.Fatal("Move followed a symlink")
	}
}

func TestMoveAnUndecodableDraft(t *testing.T) {
	q := newQueue(t)
	dir, err := q.ensureDir(Pending)
	if err != nil {
		t.Fatal(err)
	}
	id := "d-000000000000"
	if err := os.WriteFile(filepath.Join(dir, id+".json"), []byte(`{"schema":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	l, err := ReadFile(filepath.Join(dir, id+".json"))
	if err == nil || l.SHA256 != sha256.Sum256([]byte(`{"schema":1}`)) {
		t.Fatalf("ReadFile: %v, SHA-256 %x", err, l.SHA256)
	}
	if err := q.Move(id, Pending, Rejected, l.SHA256, Outcome{Reason: "unreadable"}); err != nil {
		t.Fatal(err)
	}
}

func TestMoveAnOversizedDraft(t *testing.T) {
	q := newQueue(t)
	dir, err := q.ensureDir(Pending)
	if err != nil {
		t.Fatal(err)
	}
	id := "d-000000000001"
	path := filepath.Join(dir, id+".json")
	big := bytes.Repeat([]byte(" "), MaxSize+1)
	if err := os.WriteFile(path, big, 0o600); err != nil {
		t.Fatal(err)
	}
	l, err := ReadFile(path)
	if err == nil || l.SHA256 != sha256.Sum256(big) || l.Raw != nil {
		t.Fatalf("ReadFile: %v, SHA-256 %x, %d bytes kept", err, l.SHA256, len(l.Raw))
	}
	if err := q.Move(id, Pending, Rejected, [32]byte{}, Outcome{}); !errors.Is(err, ErrChanged) {
		t.Fatalf("Move with another sum: %v", err)
	}
	if err := q.Move(id, Pending, Rejected, l.SHA256, Outcome{Reason: "unreadable"}); err != nil {
		t.Fatal(err)
	}
}

func TestMoveRejectsBadID(t *testing.T) {
	q := newQueue(t)
	for _, id := range []string{"../x", "d-0123", "d-0123456789ab.json", ""} {
		if err := q.Move(id, Pending, Rejected, [32]byte{}, Outcome{}); err == nil {
			t.Errorf("Move(%q) allowed", id)
		}
	}
}

func TestListSkipsJunk(t *testing.T) {
	q := newQueue(t)
	id, err := q.Write(golden["comment"])
	if err != nil {
		t.Fatal(err)
	}
	dir := q.Dir(Pending)
	for _, name := range []string{"notes.txt", "d-zz.json", ".tmp-1"} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(dir, "d-aaaaaaaaaaaa.json"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, id+".json"), filepath.Join(dir, "d-bbbbbbbbbbbb.json")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, id+".outcome.json"), []byte(`{"bogus":1}`), 0o600); err != nil {
		t.Fatal(err)
	}
	es := entries(t, q)
	if len(es) != 1 || es[0].ID != id || es[0].Outcome != nil {
		t.Fatalf("List = %+v, want only %s with no outcome", es, id)
	}
}
