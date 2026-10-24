package main

import (
	"runtime/debug"
	"testing"
)

func fakeBuild(version string) func() (*debug.BuildInfo, bool) {
	return func() (*debug.BuildInfo, bool) {
		return &debug.BuildInfo{GoVersion: "go1.27.2", Main: debug.Module{Path: "github.com/Sriram-PR/ghv", Version: version},
			Settings: []debug.BuildSetting{{Key: "GOOS", Value: "linux"}, {Key: "GOARCH", Value: "arm64"}}}, true
	}
}

func TestVersion(t *testing.T) {
	e, _, _ := testEnv(t, nil)
	for _, c := range []struct {
		build      func() (*debug.BuildInfo, bool)
		text, json string
	}{
		{fakeBuild("v0.1.0"), "ghv v0.1.0 (go1.27.2, linux/arm64)\n",
			"{\n  \"version\": \"v0.1.0\",\n  \"go\": \"go1.27.2\",\n  \"os\": \"linux\",\n  \"arch\": \"arm64\"\n}\n"},
		{fakeBuild("v0.1.1-0.20261024032222-016f06dd13e0+dirty"), "ghv v0.1.1-0.20261024032222-016f06dd13e0+dirty (go1.27.2, linux/arm64)\n", ""},
		{fakeBuild(""), "ghv (devel) (go1.27.2, linux/arm64)\n", ""},
		{func() (*debug.BuildInfo, bool) { return nil, false }, "ghv (devel)\n", "{\n  \"version\": \"(devel)\"\n}\n"},
	} {
		e.buildInfo = c.build
		if r := runWith(t, registry, e, "version"); r.code != 0 || r.stdout != c.text || r.stderr != "" {
			t.Errorf("exit %d, stdout %q, stderr %q; want %q", r.code, r.stdout, r.stderr, c.text)
		}
		if c.json == "" {
			continue
		}
		if r := runWith(t, registry, e, "version", "--json"); r.code != 0 || r.stdout != c.json {
			t.Errorf("--json: exit %d, stdout %q; want %q", r.code, r.stdout, c.json)
		}
	}
	if r := runWith(t, registry, e, "version", "extra"); r.code != 2 {
		t.Errorf("an argument: exit %d, stderr %q", r.code, r.stderr)
	}
}
