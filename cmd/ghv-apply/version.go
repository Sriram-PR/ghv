package main

import (
	"context"
	"fmt"
	"runtime/debug"
)

func init() {
	commands = append(commands, command{name: "version", help: "print ghv-apply's version, Go version, and platform", run: runVersion})
}

// versionText is the module version Go stamped from the checkout (the
// tag at a clean tagged commit, else a pseudo-version, "+dirty" for local
// changes, or "(devel)"), then the Go version and platform.
func versionText(buildInfo func() (*debug.BuildInfo, bool)) string {
	if buildInfo == nil {
		buildInfo = debug.ReadBuildInfo
	}
	bi, ok := buildInfo()
	if !ok {
		return "(devel)"
	}
	v := bi.Main.Version
	if v == "" {
		v = "(devel)"
	}
	var goos, goarch string
	for _, s := range bi.Settings {
		switch s.Key {
		case "GOOS":
			goos = s.Value
		case "GOARCH":
			goarch = s.Value
		}
	}
	return fmt.Sprintf("%s (%s, %s/%s)", v, bi.GoVersion, goos, goarch)
}

func runVersion(_ context.Context, e env) error {
	fmt.Fprintln(e.stdout, "ghv-apply "+versionText(e.buildInfo))
	return nil
}
