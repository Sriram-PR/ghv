package harden

import (
	"fmt"

	"golang.org/x/sys/unix"
)

func lockProcess() error {
	if err := unix.Prctl(unix.PR_SET_DUMPABLE, 0, 0, 0, 0); err != nil {
		return fmt.Errorf("prctl(PR_SET_DUMPABLE, 0): %v", err)
	}
	if err := unix.Setrlimit(unix.RLIMIT_CORE, &unix.Rlimit{}); err != nil {
		return fmt.Errorf("setrlimit(RLIMIT_CORE, 0): %v", err)
	}
	return nil
}
