package harden

import (
	"fmt"

	"golang.org/x/sys/unix"
)

func lockProcess() error {
	if err := unix.PtraceDenyAttach(); err != nil {
		return fmt.Errorf("ptrace(PT_DENY_ATTACH): %v", err)
	}
	if err := unix.Setrlimit(unix.RLIMIT_CORE, &unix.Rlimit{}); err != nil {
		return fmt.Errorf("setrlimit(RLIMIT_CORE, 0): %v", err)
	}
	return nil
}
