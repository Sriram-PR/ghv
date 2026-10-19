package doctor

import (
	"os"
	"syscall"
)

// kernelWritable asks the kernel whether this process may write path. A
// sticky directory this user does not own (such as /tmp) does not count:
// its other users' entries cannot be renamed or removed.
func kernelWritable(path string) bool {
	if syscall.Access(path, 2) != nil {
		return false
	}
	fi, err := os.Stat(path)
	if err != nil {
		return false
	}
	if fi.IsDir() && fi.Mode()&os.ModeSticky != 0 {
		st, ok := fi.Sys().(*syscall.Stat_t)
		return ok && int(st.Uid) == os.Getuid()
	}
	return true
}
