package main

import (
	"context"
	"fmt"

	"github.com/Sriram-PR/ghv/internal/buildinfo"
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

func runVersion(_ context.Context, a *app) error {
	i := buildinfo.Read(a.buildInfo)
	if a.json() {
		return writeJSON(a.stdout, versionView(i))
	}
	fmt.Fprintln(a.stdout, "ghv "+i.String())
	return nil
}
