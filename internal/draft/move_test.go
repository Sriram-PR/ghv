package draft

import (
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
	if err := q.Move(id, Pending, Unknown, Outcome{At: sent, Reason: "sending"}); err != nil {
		t.Fatal(err)
	}
	if es := entries(t, q); len(es) != 1 || es[0].State != Unknown || es[0].Outcome == nil || es[0].Outcome.Reason != "sending" {
		t.Fatalf("after Pending→Unknown: %+v", es)
	}
	url := "https://github.com/reticlehq/reticle/pull/1638#issuecomment-1"
	if err := q.Move(id, Unknown, Applied, Outcome{At: sent, Status: 201, URL: url}); err != nil {
		t.Fatal(err)
	}
	es := entries(t, q)
	if len(es) != 1 || es[0].ID != id || es[0].State != Applied || es[0].Outcome == nil {
		t.Fatalf("after Unknown→Applied: %+v", es)
	}
	if o := es[0].Outcome; o.Status != 201 || o.URL != url || !o.At.Equal(sent) {
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
		if err := q.Move(id, m[0], m[1], Outcome{}); err == nil {
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
	if err := q.Move(id, Pending, Unknown, Outcome{}); err == nil {
		t.Fatal("Move overwrote an existing file")
	}
	if data, _ := os.ReadFile(planted); string(data) != "planted" {
		t.Fatal("planted file changed")
	}
	if _, err := os.Stat(filepath.Join(q.Dir(Pending), id+".json")); err != nil {
		t.Fatalf("draft left pending/: %v", err)
	}
}

func TestMoveRejectsBadID(t *testing.T) {
	q := newQueue(t)
	for _, id := range []string{"../x", "d-0123", "d-0123456789ab.json", ""} {
		if err := q.Move(id, Pending, Rejected, Outcome{}); err == nil {
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
