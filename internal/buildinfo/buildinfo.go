// Package buildinfo reads the version both binaries print.
package buildinfo

import (
	"fmt"
	"runtime/debug"
)

type Info struct {
	Version, Go, OS, Arch string
}

// Read gives the module version Go stamped from the checkout: the tag at a
// clean tagged commit, else a pseudo-version, "+dirty" for local changes,
// or "(devel)" without version control. A nil buildInfo means
// debug.ReadBuildInfo.
func Read(buildInfo func() (*debug.BuildInfo, bool)) Info {
	if buildInfo == nil {
		buildInfo = debug.ReadBuildInfo
	}
	bi, ok := buildInfo()
	if !ok {
		return Info{Version: "(devel)"}
	}
	i := Info{Version: bi.Main.Version, Go: bi.GoVersion}
	if i.Version == "" {
		i.Version = "(devel)"
	}
	for _, s := range bi.Settings {
		switch s.Key {
		case "GOOS":
			i.OS = s.Value
		case "GOARCH":
			i.Arch = s.Value
		}
	}
	return i
}

func (i Info) String() string {
	if i.Go == "" {
		return i.Version
	}
	return fmt.Sprintf("%s (%s, %s/%s)", i.Version, i.Go, i.OS, i.Arch)
}
