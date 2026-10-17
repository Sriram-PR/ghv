package draft

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

var ErrLocked = errors.New("another ghv-apply session holds the queue lock")

type Lock struct {
	f *os.File
}

// Lock holds flock(2) on Root/lock until Release or process exit, so a
// crashed session never leaves a stale lock. The file holds the PID only to
// name the holder in ErrLocked.
func (q Queue) Lock() (*Lock, error) {
	if _, err := q.ensureDir(Pending); err != nil {
		return nil, err
	}
	path := filepath.Join(q.Root, "lock")
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|syscall.O_NOFOLLOW|syscall.O_NONBLOCK, 0o600)
	if errors.Is(err, syscall.ELOOP) {
		return nil, fmt.Errorf("%s is a symlink", path)
	}
	if err != nil {
		return nil, err
	}
	fi, err := f.Stat()
	if err == nil && !fi.Mode().IsRegular() {
		err = fmt.Errorf("%s is not a regular file", path)
	}
	if err != nil {
		f.Close()
		return nil, err
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		holder := lockHolder(f)
		f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, fmt.Errorf("%w (pid %s)", ErrLocked, holder)
		}
		return nil, err
	}
	pid := strconv.Itoa(os.Getpid())
	if err := f.Truncate(0); err != nil {
		f.Close()
		return nil, err
	}
	if _, err := f.WriteAt([]byte(pid), 0); err != nil {
		f.Close()
		return nil, err
	}
	return &Lock{f: f}, nil
}

func lockHolder(f *os.File) string {
	buf := make([]byte, 32)
	n, _ := f.ReadAt(buf, 0)
	pid, err := strconv.Atoi(strings.TrimSpace(string(buf[:n])))
	if err != nil || pid <= 0 {
		return "unknown"
	}
	return strconv.Itoa(pid)
}

// Release drops the lock and leaves the file in place.
func (l *Lock) Release() error {
	return l.f.Close()
}
