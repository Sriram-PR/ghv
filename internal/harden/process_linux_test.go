package harden

import (
	"testing"

	"golang.org/x/sys/unix"
)

func TestProcess(t *testing.T) {
	if err := Process(); err != nil {
		t.Fatal(err)
	}
	if d, err := unix.PrctlRetInt(unix.PR_GET_DUMPABLE, 0, 0, 0, 0); err != nil || d != 0 {
		t.Fatalf("dumpable = %d, %v; want 0", d, err)
	}
	var lim unix.Rlimit
	if err := unix.Getrlimit(unix.RLIMIT_CORE, &lim); err != nil || lim.Cur != 0 || lim.Max != 0 {
		t.Fatalf("RLIMIT_CORE = %+v, %v; want 0/0", lim, err)
	}
}
