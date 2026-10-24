package main

import (
	"context"
	"fmt"
	"runtime/debug"
)

func init() {
	register(&command{path: "version", help: "Print ghv's version, Go version, and platform", run: runVersion})
}

type versionView struct {
	Version string `json:"version"`
	Go      string `json:"go,omitempty"`
	OS      string `json:"os,omitempty"`
	Arch    string `json:"arch,omitempty"`
}

// readVersion is the module version Go stamped from the checkout: the tag
// at a clean tagged commit, else a pseudo-version, "+dirty" for local
// changes, or "(devel)" without version control.
func readVersion(buildInfo func() (*debug.BuildInfo, bool)) versionView {
	if buildInfo == nil {
		buildInfo = debug.ReadBuildInfo
	}
	bi, ok := buildInfo()
	if !ok {
		return versionView{Version: "(devel)"}
	}
	v := versionView{Version: bi.Main.Version, Go: bi.GoVersion}
	if v.Version == "" {
		v.Version = "(devel)"
	}
	for _, s := range bi.Settings {
		switch s.Key {
		case "GOOS":
			v.OS = s.Value
		case "GOARCH":
			v.Arch = s.Value
		}
	}
	return v
}

func (v versionView) String() string {
	if v.Go == "" {
		return v.Version
	}
	return fmt.Sprintf("%s (%s, %s/%s)", v.Version, v.Go, v.OS, v.Arch)
}

func runVersion(_ context.Context, a *app) error {
	v := readVersion(a.buildInfo)
	if a.json() {
		return writeJSON(a.stdout, v)
	}
	fmt.Fprintln(a.stdout, "ghv "+v.String())
	return nil
}
