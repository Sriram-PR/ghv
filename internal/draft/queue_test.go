package draft

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func newQueue(t *testing.T) Queue {
	return Queue{Root: filepath.Join(t.TempDir(), "ghv")}
}

func mode(t *testing.T, p string) os.FileMode {
	t.Helper()
	fi, err := os.Stat(p)
	if err != nil {
		t.Fatal(err)
	}
	return fi.Mode().Perm()
}

func TestQueueWrite(t *testing.T) {
	q := newQueue(t)
	id, err := q.Write(golden["pr.create"])
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(q.Dir(Pending), id+".json")
	l, err := ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	want := golden["pr.create"]
	want.ID = id
	if !equalDraft(l.Draft, want) {
		t.Fatalf("read back %+v, want %+v", l.Draft, want)
	}
	if m := mode(t, p); m != 0o600 {
		t.Errorf("file mode %o, want 600", m)
	}
	for _, d := range []string{q.Root, q.Dir(Pending)} {
		if m := mode(t, d); m != 0o700 {
			t.Errorf("%s mode %o, want 700", d, m)
		}
	}
	ents, _ := os.ReadDir(q.Dir(Pending))
	for _, e := range ents {
		if strings.HasPrefix(e.Name(), ".tmp-") {
			t.Errorf("temp file left behind: %s", e.Name())
		}
	}
}

func TestQueueWriteTightensDirMode(t *testing.T) {
	q := newQueue(t)
	if err := os.MkdirAll(q.Dir(Pending), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(q.Root, 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := q.Write(golden["comment"]); err != nil {
		t.Fatal(err)
	}
	for _, d := range []string{q.Root, q.Dir(Pending)} {
		if m := mode(t, d); m != 0o700 {
			t.Errorf("%s mode %o, want 700", d, m)
		}
	}
}

func TestQueueWriteRefusesInvalid(t *testing.T) {
	q := newQueue(t)
	bad := Draft{Repo: "bad", CreatedAt: time.Now(), Fields: ForkFields{}}
	if _, err := q.Write(bad); err == nil {
		t.Fatal("Write accepted an invalid draft")
	}
	if ents, _ := os.ReadDir(q.Dir(Pending)); len(ents) != 0 {
		t.Fatalf("queue has %d entries, want 0", len(ents))
	}
}

func TestQueueWriteRefusesOversize(t *testing.T) {
	q := newQueue(t)
	d := golden["issue.create"]
	d.Fields = IssueCreateFields{Title: "t", Body: strings.Repeat("a", MaxSize)}
	if _, err := q.Write(d); err == nil {
		t.Fatal("Write queued a draft ReadFile would refuse")
	}
	if ents, _ := os.ReadDir(q.Dir(Pending)); len(ents) != 0 {
		t.Fatalf("queue has %d entries, want 0", len(ents))
	}
}

func TestQueueWriteRefusesWhatReadFileRejects(t *testing.T) {
	for _, body := range []string{"\xff", "\uFFFD"} {
		q := newQueue(t)
		d := golden["issue.create"]
		d.Fields = IssueCreateFields{Title: "t", Body: body}
		if _, err := q.Write(d); err == nil {
			t.Errorf("Write queued body %q", body)
		}
	}
}

func TestQueueWriteRefusesSymlinkedDir(t *testing.T) {
	q := newQueue(t)
	if err := os.MkdirAll(q.Root, 0o700); err != nil {
		t.Fatal(err)
	}
	elsewhere := t.TempDir()
	if err := os.Symlink(elsewhere, q.Dir(Pending)); err != nil {
		t.Fatal(err)
	}
	if _, err := q.Write(golden["comment"]); err == nil {
		t.Fatal("Write followed a symlinked queue directory")
	}
	if ents, _ := os.ReadDir(elsewhere); len(ents) != 0 {
		t.Fatalf("symlink target has %d entries, want 0", len(ents))
	}
}

func TestQueueWriteUniqueIDs(t *testing.T) {
	q := newQueue(t)
	a, err := q.Write(golden["comment"])
	if err != nil {
		t.Fatal(err)
	}
	b, err := q.Write(golden["comment"])
	if err != nil {
		t.Fatal(err)
	}
	if a == b || !idRE.MatchString(a) || !idRE.MatchString(b) {
		t.Fatalf("ids %q and %q", a, b)
	}
}
