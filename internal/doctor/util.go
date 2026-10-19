package doctor

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"syscall"
)

// homes are the running user's home and the human's, once each.
func (e *Env) homes() []string {
	hs := []string{e.Home}
	if e.Human != nil && e.Human.Home != e.Home {
		hs = append(hs, e.Human.Home)
	}
	return hs
}

// readCapped reads up to limit bytes of a regular file. O_NONBLOCK keeps a
// FIFO from hanging the scan.
func readCapped(path string, limit int64) ([]byte, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !fi.Mode().IsRegular() {
		return nil, errors.New("not a regular file")
	}
	return io.ReadAll(io.LimitReader(f, limit))
}

func isGitHubHost(h string) bool {
	h = strings.ToLower(h)
	return h == "github.com" || strings.HasSuffix(h, ".github.com")
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}
