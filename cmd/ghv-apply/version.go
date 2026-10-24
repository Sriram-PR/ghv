package main

import (
	"context"
	"fmt"

	"github.com/Sriram-PR/ghv/internal/buildinfo"
)

func init() {
	commands = append(commands, command{name: "version", help: "print ghv-apply's version, Go version, and platform", run: runVersion})
}

func runVersion(_ context.Context, e env) error {
	fmt.Fprintln(e.stdout, "ghv-apply "+buildinfo.Read(e.buildInfo).String())
	return nil
}
