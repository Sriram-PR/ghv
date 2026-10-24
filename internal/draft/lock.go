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

// Lock holds a raw descriptor, not an *os.File: the runtime closes an
// unreachable *os.File, which would drop the flock while a caller still
// relies on it.
type Lock struct {
	fd int
}

// Lock holds flock(2) on Root/lock until Release or process exit, so a
// crashed session never leaves a stale lock. The file holds the PID only to
// name the holder in ErrLocked.
func (q Queue) Lock() (*Lock, error) {
	l, err := q.lock()
	return l, q.permission(err)
}

func (q Queue) lock() (*Lock, error) {
	if _, err := q.ensureDir(Pending); err != nil {
		return nil, err
	}
	path := filepath.Join(q.Root, "lock")
	fd, err := syscall.Open(path, syscall.O_RDWR|syscall.O_CREAT|syscall.O_NOFOLLOW|syscall.O_NONBLOCK|syscall.O_CLOEXEC, 0o600)
	if errors.Is(err, syscall.ELOOP) {
		return nil, fmt.Errorf("%s is a symlink", path)
	}
	if err != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: err}
	}
	var st syscall.Stat_t
	err = syscall.Fstat(fd, &st)
	if err == nil && st.Mode&syscall.S_IFMT != syscall.S_IFREG {
		err = fmt.Errorf("%s is not a regular file", path)
	}
	if err != nil {
		syscall.Close(fd)
		return nil, err
	}
	if err := syscall.Flock(fd, syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		holder := lockHolder(fd)
		syscall.Close(fd)
		if errors.Is(err, syscall.EWOULDBLOCK) {
			return nil, fmt.Errorf("%w (pid %s)", ErrLocked, holder)
		}
		return nil, err
	}
	pid := strconv.Itoa(os.Getpid())
	if err := syscall.Ftruncate(fd, 0); err != nil {
		syscall.Close(fd)
		return nil, err
	}
	if _, err := syscall.Pwrite(fd, []byte(pid), 0); err != nil {
		syscall.Close(fd)
		return nil, err
	}
	return &Lock{fd: fd}, nil
}

func lockHolder(fd int) string {
	buf := make([]byte, 32)
	n, err := syscall.Pread(fd, buf, 0)
	if err != nil || n < 0 {
		return "unknown"
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(buf[:n])))
	if err != nil || pid <= 0 {
		return "unknown"
	}
	return strconv.Itoa(pid)
}

// Release drops the lock and leaves the file in place. A second Release
// is an error and closes nothing, since the descriptor may be reused.
func (l *Lock) Release() error {
	if l.fd < 0 {
		return errors.New("queue lock already released")
	}
	fd := l.fd
	l.fd = -1
	return syscall.Close(fd)
}
