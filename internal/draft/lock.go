package draft

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

var ErrLocked = errors.New("another ghv-apply session holds the queue lock")

type Lock struct {
	path string
	pid  string
}

// Lock takes Root/lock. A lock whose PID no longer exists, or which does
// not hold a PID at all, is stale and removed.
func (q Queue) Lock() (*Lock, error) {
	if _, err := q.ensureDir(Pending); err != nil {
		return nil, err
	}
	path := filepath.Join(q.Root, "lock")
	pid := strconv.Itoa(os.Getpid())
	for range 2 {
		f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL|syscall.O_NOFOLLOW, 0o600)
		if err == nil {
			_, werr := f.WriteString(pid)
			if err := errors.Join(werr, f.Close()); err != nil {
				os.Remove(path)
				return nil, err
			}
			return &Lock{path: path, pid: pid}, nil
		}
		if !errors.Is(err, fs.ErrExist) {
			return nil, err
		}
		holder, err := lockHolder(path)
		if err != nil {
			return nil, err
		}
		if holder > 0 && processAlive(holder) {
			return nil, fmt.Errorf("%w (pid %d; remove %s if that is not ghv-apply)", ErrLocked, holder, path)
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, err
		}
	}
	return nil, ErrLocked
}

// lockHolder returns the PID in the lock file, or 0 if it holds none.
func lockHolder(path string) (int, error) {
	data, err := readRegular(path, 32)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, nil
	}
	if errors.Is(err, syscall.ELOOP) {
		return 0, fmt.Errorf("%s is a symlink", path)
	}
	if err != nil && strings.Contains(err.Error(), "larger than") {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		return 0, nil
	}
	return pid, nil
}

func processAlive(pid int) bool {
	err := syscall.Kill(pid, 0)
	return err == nil || errors.Is(err, syscall.EPERM)
}

func (l *Lock) Release() error {
	data, err := readRegular(l.path, 32)
	if err != nil {
		return err
	}
	if string(data) != l.pid {
		return fmt.Errorf("%s now belongs to another process; leaving it", l.path)
	}
	return os.Remove(l.path)
}
