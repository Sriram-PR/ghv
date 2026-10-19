package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"os/user"
	"runtime"
	"slices"
	"strconv"
	"time"
)

// registry is filled by each command file's init.
var registry []*command

func register(c *command) { registry = append(registry, c) }

func main() {
	home, err := os.UserHomeDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, "ghv:", err)
		os.Exit(1)
	}
	// Not os/user.Current: without cgo it falls back to $HOME.
	var passwdHome, username string
	if u, err := user.LookupId(strconv.Itoa(os.Getuid())); err == nil {
		passwdHome, username = u.HomeDir, u.Username
	}
	proc := ""
	if runtime.GOOS == "linux" {
		proc = "/proc"
	}
	dir, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, "ghv:", err)
		os.Exit(1)
	}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	code := run(ctx, registry, os.Args[1:], env{
		stdin:      os.Stdin,
		stdout:     os.Stdout,
		stderr:     os.Stderr,
		home:       home,
		passwdHome: passwdHome,
		getenv:     os.Getenv,
		dir:        dir,
		now:        time.Now,
		goos:       runtime.GOOS,
		uid:        os.Getuid(),
		pid:        os.Getpid(),
		username:   username,
		proc:       proc,
		systemBin:  "/usr/local/bin",
		lookupUser: user.Lookup,
		confirm:    confirmOnTTY,
	})
	stop()
	os.Exit(code)
}

func run(ctx context.Context, cmds []*command, args []string, e env) int {
	inv, err := parse(cmds, args)
	var hr helpRequest
	switch {
	case errors.As(err, &hr):
		fmt.Fprint(e.stdout, helpText(cmds, hr.cmd))
		return 0
	case err != nil && len(args) == 0:
		fmt.Fprint(e.stderr, helpText(cmds, nil))
		return 2
	case err != nil:
		cmd, _ := lookup(cmds, args)
		return report(e, cmd, err, slices.Contains(args, "--json"))
	}
	if err := inv.cmd.run(ctx, &app{env: e, inv: inv}); err != nil {
		return report(e, inv.cmd, err, inv.bool("json"))
	}
	return 0
}
