package main

import (
	"os"
	"strings"
	"testing"
)

func pipeTTY(t *testing.T, input string) devTTY {
	t.Helper()
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { r.Close() })
	go func() {
		w.WriteString(input)
		w.Close()
	}()
	return devTTY{r}
}

func TestReadLineCap(t *testing.T) {
	full := strings.Repeat("a", maxLine)
	if got, err := pipeTTY(t, full+"\n").ReadLine(""); err != nil || got != full {
		t.Fatalf("%d bytes: %d bytes back, %v", maxLine, len(got), err)
	}
	if _, err := pipeTTY(t, full+"a\n").ReadLine(""); err == nil {
		t.Fatalf("%d bytes accepted", maxLine+1)
	}
}

// Bytes left in the terminal after ghv-apply exits reach the shell as a
// command, so an overlong line is read to its end.
func TestReadLineDrainsAnOverlongLine(t *testing.T) {
	tty := pipeTTY(t, strings.Repeat("a", 3*maxLine)+"\nyes\n")
	if _, err := tty.ReadLine(""); err == nil {
		t.Fatal("overlong line accepted")
	}
	if got, err := tty.ReadLine(""); err != nil || got != "yes" {
		t.Fatalf("next line %q, %v", got, err)
	}
}
