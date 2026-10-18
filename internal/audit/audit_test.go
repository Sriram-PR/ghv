package audit

import (
	"bufio"
	"encoding/json"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func TestAppend(t *testing.T) {
	home := t.TempDir()
	path := Path(home)
	if path != filepath.Join(home, ".local", "state", "ghv", "audit.jsonl") {
		t.Fatalf("path %s", path)
	}
	at := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	es := []Entry{
		{Time: at, Draft: "d-0123456789ab", Kind: "comment", Repo: "o/r", RequestSHA256: "ab12", Status: 201, URL: "https://github.com/o/r/issues/7#issuecomment-1"},
		{Time: at.Add(time.Minute), Draft: "d-0123456789ac", Kind: "fork", Repo: "o/r", RequestSHA256: "cd34", Status: 202, URL: "https://github.com/Sriram-PR/r"},
	}
	for _, e := range es {
		if err := Append(path, e); err != nil {
			t.Fatal(err)
		}
	}
	for p, mode := range map[string]os.FileMode{path: 0o600, filepath.Dir(path): 0o700} {
		if fi, err := os.Stat(p); err != nil || fi.Mode().Perm() != mode {
			t.Fatalf("%s: %v, %v", p, fi.Mode(), err)
		}
	}
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var got []Entry
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		var e Entry
		if err := json.Unmarshal(sc.Bytes(), &e); err != nil {
			t.Fatalf("line %q: %v", sc.Text(), err)
		}
		got = append(got, e)
	}
	if len(got) != 2 || got[0] != es[0] || got[1] != es[1] {
		t.Fatalf("read back %+v", got)
	}
	data, _ := os.ReadFile(path)
	want := `{"time":"2026-10-18T12:00:00Z","draft":"d-0123456789ab","kind":"comment","repo":"o/r","request_sha256":"ab12","status":201,"url":"https://github.com/o/r/issues/7#issuecomment-1"}` + "\n"
	if string(data[:len(want)]) != want {
		t.Fatalf("first line %q", data[:len(want)])
	}
}

func TestAppendRefusesWhatIsNotAFile(t *testing.T) {
	home := t.TempDir()
	path := Path(home)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(home, "elsewhere")
	if err := os.Symlink(target, path); err != nil {
		t.Fatal(err)
	}
	if err := Append(path, Entry{Draft: "d-0123456789ab"}); err == nil {
		t.Fatal("Append followed a symlink")
	}
	if _, err := os.Stat(target); !os.IsNotExist(err) {
		t.Fatalf("the symlink's target was created: %v", err)
	}
	os.Remove(path)
	if err := syscall.Mkfifo(path, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := Append(path, Entry{Draft: "d-0123456789ab"}); err == nil {
		t.Fatal("Append wrote to a FIFO")
	}
}

func TestFoundLaterIsMarked(t *testing.T) {
	path := Path(t.TempDir())
	at := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	for _, e := range []Entry{
		{Time: at, Draft: "d-0123456789ab", Kind: "comment", Repo: "o/r", RequestSHA256: "ab12", Status: 201, URL: "u1"},
		{Time: at, Draft: "d-0123456789ac", Kind: "comment", Repo: "o/r", URL: "u2", Found: true},
	} {
		if err := Append(path, e); err != nil {
			t.Fatal(err)
		}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"time":"2026-10-18T12:00:00Z","draft":"d-0123456789ab","kind":"comment","repo":"o/r","request_sha256":"ab12","status":201,"url":"u1"}` + "\n" +
		`{"time":"2026-10-18T12:00:00Z","draft":"d-0123456789ac","kind":"comment","repo":"o/r","request_sha256":"","status":0,"url":"u2","found":true}` + "\n"
	if string(data) != want {
		t.Fatalf("got\n%s", data)
	}
}
