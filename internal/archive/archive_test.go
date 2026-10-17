package archive

import (
	"archive/zip"
	"bytes"
	"compress/flate"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

type entry struct {
	name string
	mode fs.FileMode
	data string
}

func zipOf(t *testing.T, entries ...entry) (*bytes.Reader, int64) {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, e := range entries {
		h := &zip.FileHeader{Name: e.name, Method: zip.Deflate}
		mode := e.mode
		if mode == 0 {
			mode = 0o644
		}
		h.SetMode(mode)
		w, err := zw.CreateHeader(h)
		if err != nil {
			t.Fatal(err)
		}
		io.WriteString(w, e.data)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return bytes.NewReader(buf.Bytes()), int64(buf.Len())
}

var limits = Limits{MaxBytes: 1 << 20, MaxFiles: 100}

func TestExtract(t *testing.T) {
	ra, size := zipOf(t,
		entry{name: "a.txt", data: "A"},
		entry{name: "d/", mode: fs.ModeDir | 0o755},
		entry{name: "d/b.sh", mode: 0o755, data: "BB"},
		entry{name: "e/f/c.txt", data: "C"},
	)
	dir := filepath.Join(t.TempDir(), "out")
	files, n, err := Extract(ra, size, dir, limits)
	if err != nil || !slices.Equal(files, []string{"a.txt", "d/b.sh", "e/f/c.txt"}) || n != 4 {
		t.Fatalf("files %q, %d bytes, err %v", files, n, err)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "d", "b.sh"))
	fi, _ := os.Stat(filepath.Join(dir, "d", "b.sh"))
	if string(b) != "BB" || fi.Mode().Perm() != 0o644 {
		t.Fatalf("d/b.sh = %q, mode %v", b, fi.Mode())
	}
}

func TestExtractRefusesUnsafeEntries(t *testing.T) {
	tests := []struct {
		name    string
		entries []entry
	}{
		{"parent", []entry{{name: "../evil", data: "x"}}},
		{"nested parent", []entry{{name: "d/../../evil", data: "x"}}},
		{"absolute", []entry{{name: "/tmp/evil", data: "x"}}},
		{"empty name", []entry{{name: "", data: "x"}}},
		{"backslash", []entry{{name: `a\..\b`, data: "x"}}},
		{"symlink", []entry{{name: "link", mode: fs.ModeSymlink | 0o777, data: "/etc/passwd"}}},
		{"named pipe", []entry{{name: "fifo", mode: fs.ModeNamedPipe | 0o644}}},
		{"duplicate", []entry{{name: "a", data: "1"}, {name: "a", data: "2"}}},
		{"duplicate after cleaning", []entry{{name: "a", data: "1"}, {name: "d/../a", data: "2"}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			parent := t.TempDir()
			dir := filepath.Join(parent, "out")
			ra, size := zipOf(t, tt.entries...)
			_, _, err := Extract(ra, size, dir, limits)
			if !errors.Is(err, ErrUnsafe) {
				t.Fatalf("err %v, want ErrUnsafe", err)
			}
			if left, _ := os.ReadDir(parent); len(left) != 0 {
				t.Fatalf("left behind: %v", left)
			}
		})
	}
}

func TestExtractNeedsANewDirectory(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a"), []byte("keep"), 0o600)
	ra, size := zipOf(t, entry{name: "b", data: "x"})
	_, _, err := Extract(ra, size, dir, limits)
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("err %v", err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "a")); string(b) != "keep" || exists(filepath.Join(dir, "b")) {
		t.Fatal("touched an existing directory")
	}
}

func exists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}

func TestExtractCaps(t *testing.T) {
	t.Run("bytes", func(t *testing.T) {
		ra, size := zipOf(t, entry{name: "a", data: strings.Repeat("0", 600)}, entry{name: "b", data: strings.Repeat("0", 600)})
		dir := filepath.Join(t.TempDir(), "out")
		_, _, err := Extract(ra, size, dir, Limits{MaxBytes: 1000, MaxFiles: 10})
		if !errors.Is(err, ErrTooLarge) || exists(dir) {
			t.Fatalf("err %v, dir left %v", err, exists(dir))
		}
	})
	t.Run("files", func(t *testing.T) {
		ra, size := zipOf(t, entry{name: "a"}, entry{name: "b"}, entry{name: "c"})
		dir := filepath.Join(t.TempDir(), "out")
		_, _, err := Extract(ra, size, dir, Limits{MaxBytes: 1000, MaxFiles: 2})
		if !errors.Is(err, ErrTooLarge) || exists(dir) {
			t.Fatalf("err %v, dir left %v", err, exists(dir))
		}
	})
}

// lyingZip declares 10 bytes for an entry that inflates to 2 MiB.
func lyingZip(t *testing.T) (*bytes.Reader, int64) {
	t.Helper()
	var comp bytes.Buffer
	fw, _ := flate.NewWriter(&comp, flate.BestCompression)
	fw.Write(make([]byte, 2<<20))
	fw.Close()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.CreateRaw(&zip.FileHeader{Name: "bomb", Method: zip.Deflate,
		CompressedSize64: uint64(comp.Len()), UncompressedSize64: 10})
	if err != nil {
		t.Fatal(err)
	}
	w.Write(comp.Bytes())
	zw.Close()
	return bytes.NewReader(buf.Bytes()), int64(buf.Len())
}

// archive/zip stops an entry at its declared size; the byte cap stands
// behind that.
func TestExtractRefusesLyingSizes(t *testing.T) {
	ra, size := lyingZip(t)
	dir := filepath.Join(t.TempDir(), "out")
	_, _, err := Extract(ra, size, dir, Limits{MaxBytes: 1 << 20, MaxFiles: 10})
	if err == nil || exists(dir) {
		t.Fatalf("err %v, dir left %v", err, exists(dir))
	}
}

func TestExtractRefusesNonZip(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "out")
	_, _, err := Extract(strings.NewReader("not a zip"), 9, dir, limits)
	if err == nil || exists(dir) {
		t.Fatalf("err %v, dir left %v", err, exists(dir))
	}
}

func TestReadFiles(t *testing.T) {
	ra, size := zipOf(t,
		entry{name: "0_lint.txt", data: "lint log"},
		entry{name: "lint/", mode: fs.ModeDir | 0o755},
		entry{name: "lint/system.txt", data: "system"},
		entry{name: "1_test.txt", data: "test log"},
	)
	top := func(name string) bool { return !strings.Contains(name, "/") }
	files, err := ReadFiles(ra, size, top, 100)
	if err != nil || len(files) != 2 || files[0].Name != "0_lint.txt" || string(files[1].Data) != "test log" {
		t.Fatalf("files %+v, err %v", files, err)
	}
	if _, err := ReadFiles(ra, size, top, 15); !errors.Is(err, ErrTooLarge) {
		t.Fatalf("over the cap: err %v", err)
	}
	ra, size = lyingZip(t)
	if _, err := ReadFiles(ra, size, top, 1<<20); err == nil {
		t.Fatalf("lying header: err %v", err)
	}
	ra, size = zipOf(t, entry{name: "../x.txt", data: "x"})
	if _, err := ReadFiles(ra, size, top, 100); !errors.Is(err, ErrUnsafe) {
		t.Fatalf("unsafe name: err %v", err)
	}
}
