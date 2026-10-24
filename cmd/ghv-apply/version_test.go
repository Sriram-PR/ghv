package main

import (
	"runtime/debug"
	"testing"
)

func TestVersion(t *testing.T) {
	e := testEnv(t)
	e.buildInfo = func() (*debug.BuildInfo, bool) {
		return &debug.BuildInfo{GoVersion: "go1.27.2", Main: debug.Module{Path: "github.com/Sriram-PR/ghv", Version: "v0.1.0"},
			Settings: []debug.BuildSetting{{Key: "GOOS", Value: "darwin"}, {Key: "GOARCH", Value: "amd64"}}}, true
	}
	if r := runWith(e, "version"); r.code != 0 || r.stdout != "ghv-apply v0.1.0 (go1.27.2, darwin/amd64)\n" || r.stderr != "" {
		t.Fatalf("exit %d, stdout %q, stderr %q", r.code, r.stdout, r.stderr)
	}
	e.buildInfo = func() (*debug.BuildInfo, bool) { return nil, false }
	if r := runWith(e, "version"); r.stdout != "ghv-apply (devel)\n" {
		t.Fatalf("no build info: stdout %q", r.stdout)
	}
	if r := runWith(e, "version", "--json"); r.code != 2 || r.stderr != "ghv-apply: version takes no arguments\n" {
		t.Fatalf("an argument: exit %d, stderr %q", r.code, r.stderr)
	}
}
