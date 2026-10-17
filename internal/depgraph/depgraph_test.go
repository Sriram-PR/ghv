package depgraph

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"slices"
	"testing"
)

var targets = []struct{ goos, goarch string }{
	{"linux", "amd64"},
	{"linux", "arm64"},
	{"darwin", "amd64"},
	{"darwin", "arm64"},
}

func listPackages(t *testing.T, dir, goos, goarch string) []Package {
	t.Helper()
	cmd := exec.Command("go", "list", "-json=ImportPath,Deps", "./...")
	cmd.Dir = dir
	cmd.Env = append(os.Environ(),
		"GOOS="+goos, "GOARCH="+goarch, "CGO_ENABLED=0", "GOWORK=off", "GOFLAGS=")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("go list in %s (%s/%s): %v\n%s", dir, goos, goarch, err, stderr.String())
	}
	var pkgs []Package
	dec := json.NewDecoder(bytes.NewReader(out))
	for {
		var p Package
		err := dec.Decode(&p)
		if errors.Is(err, io.EOF) {
			return pkgs
		}
		if err != nil {
			t.Fatal(err)
		}
		pkgs = append(pkgs, p)
	}
}

func TestRepoImportGraph(t *testing.T) {
	for _, tg := range targets {
		for _, v := range Violations(listPackages(t, "../..", tg.goos, tg.goarch)) {
			t.Errorf("%s/%s: %s", tg.goos, tg.goarch, v)
		}
	}
}

func TestForbiddenFixtureIsCaught(t *testing.T) {
	got := Violations(listPackages(t, "testdata/forbidden", "linux", "amd64"))
	want := "cmd/ghv imports " + Module + "/internal/write"
	if !slices.Contains(got, want) {
		t.Fatalf("Violations = %q, want it to contain %q", got, want)
	}
}

func TestViolations(t *testing.T) {
	const m = Module + "/"
	roots := []Package{{ImportPath: m + "cmd/ghv"}, {ImportPath: m + "cmd/ghv-apply"}}
	tests := []struct {
		name string
		pkg  Package
		want []string
	}{
		{"ghv std and allowed internals",
			Package{m + "cmd/ghv", []string{"net/http", "vendor/golang.org/x/net/http2/hpack", m + "internal/read", m + "internal/draft"}},
			nil},
		{"ghv imports x/term",
			Package{m + "cmd/ghv", []string{"golang.org/x/term"}},
			[]string{"cmd/ghv imports golang.org/x/term"}},
		{"ghv imports fetch",
			Package{m + "cmd/ghv", []string{m + "internal/fetch"}},
			[]string{"cmd/ghv imports " + m + "internal/fetch"}},
		{"ghv imports write",
			Package{m + "cmd/ghv", []string{m + "internal/write"}},
			[]string{"cmd/ghv imports " + m + "internal/write"}},
		{"ghv imports an unlisted internal package",
			Package{m + "cmd/ghv", []string{m + "internal/util"}},
			[]string{"cmd/ghv imports " + m + "internal/util"}},
		{"ghv-apply std, x modules, and its internals",
			Package{m + "cmd/ghv-apply", []string{"crypto/aes", "golang.org/x/crypto/argon2", "golang.org/x/term", "golang.org/x/sys/unix", m + "internal/vault", m + "internal/draft"}},
			nil},
		{"ghv-apply imports read",
			Package{m + "cmd/ghv-apply", []string{m + "internal/read"}},
			[]string{"cmd/ghv-apply imports " + m + "internal/read"}},
		{"ghv-apply imports another module",
			Package{m + "cmd/ghv-apply", []string{"github.com/evil/x"}},
			[]string{"cmd/ghv-apply imports github.com/evil/x"}},
		{"ghv-apply imports a lookalike module",
			Package{m + "cmd/ghv-apply", []string{"golang.org/x/cryptofake"}},
			[]string{"cmd/ghv-apply imports golang.org/x/cryptofake"}},
		{"draft imports fetch",
			Package{m + "internal/draft", []string{m + "internal/fetch"}},
			[]string{"internal/draft imports " + m + "internal/fetch"}},
		{"doctor imports read",
			Package{m + "internal/doctor", []string{m + "internal/read"}},
			[]string{"internal/doctor imports " + m + "internal/read"}},
		{"sanitize imports std",
			Package{m + "internal/sanitize", []string{"unicode"}},
			nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := Violations(append(slices.Clone(roots), tt.pkg))
			if !slices.Equal(got, tt.want) {
				t.Errorf("Violations = %q, want %q", got, tt.want)
			}
		})
	}
	want := []string{"missing package cmd/ghv", "missing package cmd/ghv-apply"}
	if got := Violations(nil); !slices.Equal(got, want) {
		t.Errorf("Violations(nil) = %q, want %q", got, want)
	}
}
