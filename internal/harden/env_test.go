package harden

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"testing"
)

func TestEnviron(t *testing.T) {
	in := []string{
		"TERM=xterm-256color",
		"PATH=/usr/bin",
		"LANG=en_US.UTF-8",
		"LC_ALL=C.UTF-8",
		"LC_TIME=en_GB.UTF-8",
		"LC_=x",
		"LC_lower=x",
		"EDITOR=/usr/bin/vim",
		"VISUAL=code --wait",
		"HOME=/tmp/agent",
		"XDG_CONFIG_HOME=/tmp/agent/.config",
		"HTTPS_PROXY=http://127.0.0.1:8080",
		"https_proxy=http://127.0.0.1:8080",
		"SSL_CERT_FILE=/tmp/agent/ca.pem",
		"GODEBUG=x509ignoreCN=0",
		"GOTRACEBACK=all",
		"TERM=dumb",
		"NOEQUALS",
	}
	want := []string{
		"TERM=xterm-256color",
		"LANG=en_US.UTF-8",
		"LC_ALL=C.UTF-8",
		"LC_TIME=en_GB.UTF-8",
		"EDITOR=/usr/bin/vim",
	}
	if got := Environ(in); !slices.Equal(got, want) {
		t.Fatalf("Environ:\n got %q\nwant %q", got, want)
	}
	if got := Environ(want); !slices.Equal(got, want) {
		t.Fatalf("Environ is not idempotent: %q", got)
	}
}

// TestInitChild is the process TestInit starts. It runs only when the
// test binary gets the extra argument "harden-child", which survives the
// re-exec where an environment marker would not.
func TestInitChild(t *testing.T) {
	if flag.Arg(0) != "harden-child" {
		t.Skip("helper process for TestInit")
	}
	if err := Init(); err != nil {
		fmt.Fprintln(os.Stderr, "Init:", err)
		os.Exit(1)
	}
	fmt.Print(strings.Join(os.Environ(), "\n"))
	os.Exit(0)
}

func TestInit(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestInitChild$", "--", "harden-child")
	cmd.Env = []string{
		"HOME=/tmp/agent",
		"XDG_DATA_HOME=/tmp/agent/data",
		"HTTPS_PROXY=http://127.0.0.1:1",
		"SSL_CERT_FILE=/tmp/agent/ca.pem",
		"GODEBUG=http2client=0",
		"TERM=xterm",
		"LANG=C.UTF-8",
		"EDITOR=vi",
		"VISUAL=/usr/bin/vi",
		"PATH=/usr/bin:/bin",
	}
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("child: %v\n%s", err, out)
	}
	want := "TERM=xterm\nLANG=C.UTF-8\nVISUAL=/usr/bin/vi"
	if string(out) != want {
		t.Fatalf("child environment:\n%s\nwant:\n%s", out, want)
	}
}

func TestHome(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	u, err := user.LookupId(strconv.Itoa(os.Getuid()))
	if err != nil {
		t.Skip("no passwd entry:", err)
	}
	got, err := Home()
	if err != nil || got != u.HomeDir || !filepath.IsAbs(got) {
		t.Fatalf("Home() = %q, %v; passwd says %q", got, err, u.HomeDir)
	}
}
