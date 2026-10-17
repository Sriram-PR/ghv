package draft

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
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

func TestReleaseLeavesForeignLock(t *testing.T) {
	q := newQueue(t)
	l, err := q.Lock()
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(q.Root, "lock")
	if err := os.WriteFile(p, []byte("999999"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := l.Release(); err == nil {
		t.Fatal("Release removed a lock it does not hold")
	}
	if _, err := os.Stat(p); err != nil {
		t.Fatalf("foreign lock was removed: %v", err)
	}
}
