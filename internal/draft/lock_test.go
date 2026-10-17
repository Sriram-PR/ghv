package draft

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
)

func TestLockExcludes(t *testing.T) {
	q := newQueue(t)
	l, err := q.Lock()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := q.Lock(); !errors.Is(err, ErrLocked) {
		t.Fatalf("second Lock: err = %v, want ErrLocked", err)
	}
	if err := l.Release(); err != nil {
		t.Fatal(err)
	}
	l2, err := q.Lock()
	if err != nil {
		t.Fatalf("Lock after Release: %v", err)
	}
	l2.Release()
}

func TestLockFileHoldsPID(t *testing.T) {
	q := newQueue(t)
	l, err := q.Lock()
	if err != nil {
		t.Fatal(err)
	}
	defer l.Release()
	data, err := os.ReadFile(filepath.Join(q.Root, "lock"))
	if err != nil {
		t.Fatal(err)
	}
	if string(data) != strconv.Itoa(os.Getpid()) {
		t.Fatalf("lock holds %q, want our pid", data)
	}
}

func deadPID(t *testing.T) int {
	t.Helper()
	cmd := exec.Command("true")
	if err := cmd.Run(); err != nil {
		t.Fatal(err)
	}
	return cmd.Process.Pid
}

func plantLock(t *testing.T, q Queue, content string) {
	t.Helper()
	if _, err := q.ensureDir(Pending); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(q.Root, "lock"), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestLockRecoversStale(t *testing.T) {
	for name, content := range map[string]string{
		"dead pid": strconv.Itoa(deadPID(t)),
		"garbage":  "not a pid",
		"empty":    "",
	} {
		t.Run(name, func(t *testing.T) {
			q := newQueue(t)
			plantLock(t, q, content)
			l, err := q.Lock()
			if err != nil {
				t.Fatalf("Lock over a stale lock: %v", err)
			}
			l.Release()
		})
	}
}

func TestLockRefusesSymlink(t *testing.T) {
	q := newQueue(t)
	if _, err := q.ensureDir(Pending); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(t.TempDir(), "target")
	if err := os.WriteFile(target, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(q.Root, "lock")); err != nil {
		t.Fatal(err)
	}
	if _, err := q.Lock(); err == nil {
		t.Fatal("Lock accepted a symlinked lock file")
	}
	if data, _ := os.ReadFile(target); string(data) != "keep" {
		t.Fatalf("symlink target changed to %q", data)
	}
}

// Unlinking the file on release would let a starter that opened the old
// inode and one that created a new file both hold a lock.
func TestReleaseKeepsLockFile(t *testing.T) {
	q := newQueue(t)
	l, err := q.Lock()
	if err != nil {
		t.Fatal(err)
	}
	if err := l.Release(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(q.Root, "lock")); err != nil {
		t.Fatalf("lock file after Release: %v", err)
	}
}

func TestLockedErrorNamesHolder(t *testing.T) {
	q := newQueue(t)
	l, err := q.Lock()
	if err != nil {
		t.Fatal(err)
	}
	defer l.Release()
	_, err = q.Lock()
	if want := "pid " + strconv.Itoa(os.Getpid()); err == nil || !strings.Contains(err.Error(), want) {
		t.Fatalf("second Lock: err = %v, want it to name %q", err, want)
	}
}

func TestLockConcurrentStarts(t *testing.T) {
	dead := strconv.Itoa(deadPID(t))
	for _, stale := range []bool{false, true} {
		for round := range 200 {
			q := newQueue(t)
			if stale {
				plantLock(t, q, dead)
			}
			var wg sync.WaitGroup
			var mu sync.Mutex
			held := 0
			for range 8 {
				wg.Go(func() {
					if _, err := q.Lock(); err == nil {
						mu.Lock()
						held++
						mu.Unlock()
					}
				})
			}
			wg.Wait()
			if held > 1 {
				t.Fatalf("stale=%v round %d: %d sessions hold the lock", stale, round, held)
			}
		}
	}
}

// A lock nobody references must still be held: the garbage collector once
// closed the file behind a dropped *Lock and released the flock with it.
func TestLockSurvivesGC(t *testing.T) {
	q := newQueue(t)
	if _, err := q.Lock(); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		runtime.GC()
	}
	if _, err := q.Lock(); !errors.Is(err, ErrLocked) {
		t.Fatalf("second Lock after GC: err = %v, want ErrLocked", err)
	}
}

func TestReleaseTwiceClosesNothingElse(t *testing.T) {
	q := newQueue(t)
	l, err := q.Lock()
	if err != nil {
		t.Fatal(err)
	}
	if err := l.Release(); err != nil {
		t.Fatal(err)
	}
	f, err := os.Open(filepath.Join(q.Root, "lock"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if err := l.Release(); err == nil {
		t.Fatal("second Release returned no error")
	}
	if _, err := f.Stat(); err != nil {
		t.Fatalf("second Release closed another file: %v", err)
	}
}
