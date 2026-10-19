package doctor

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// Tokens are built here, never written out whole, so no secret scanner
// mistakes this file for a leak.
var (
	classic     = "ghp_" + strings.Repeat("A1", 18)
	oauth       = "gho_" + strings.Repeat("b2", 18)
	fineGrained = "github_pat_" + strings.Repeat("C3d4", 20)
)

func TestTokenPatterns(t *testing.T) {
	home, human := t.TempDir(), t.TempDir()
	plant(t, home, map[string]string{
		".bash_history":        "ls\nexport GH_TOKEN=" + classic + "\ncd\ncurl -H 'Authorization: Bearer " + fineGrained + "' x " + classic + "\n",
		".config/gh/hosts.yml": "github.com:\n    oauth_token: " + oauth + "\n",
		".zshrc":               "# ghp_short is not a token\nexport A=xghp_" + strings.Repeat("A", 36) + "\n",
		"notes.txt":            classic + "\n",
	})
	plant(t, human, map[string]string{".claude.json": `{"env":{"GITHUB_PERSONAL_ACCESS_TOKEN":"` + fineGrained + `"}}`})
	e := &Env{Home: home, Human: &Human{Home: human}}
	want := "HIGH " + filepath.Join(home, ".bash_history") + " holds 3 GitHub tokens (ghp_, github_pat_) on lines 2, 4\n" +
		"HIGH " + filepath.Join(home, ".config/gh/hosts.yml") + " holds 1 GitHub token (gho_) on line 2\n" +
		"HIGH " + filepath.Join(human, ".claude.json") + " holds 1 GitHub token (github_pat_) on line 1\n"
	got := tokenPatterns(context.Background(), e)
	if details(got) != want {
		t.Fatalf("got:\n%s\nwant:\n%s", details(got), want)
	}
	for _, f := range got {
		for _, tok := range []string{classic, oauth, fineGrained} {
			if strings.Contains(f.Detail+f.Fix, tok) {
				t.Fatalf("a finding shows a token: %+v", f)
			}
		}
	}
}

func TestDockerGHCR(t *testing.T) {
	home := t.TempDir()
	plant(t, home, map[string]string{".docker/config.json": `{"auths":{"https://index.docker.io/v1/":{},"ghcr.io":{"auth":"eDp5"},"ghcr.io.evil.example":{},"https://ghcr.io/v2/":{}},"credHelpers":{"docker.pkg.github.com":"secretservice"}}`})
	want := "HIGH " + filepath.Join(home, ".docker/config.json") + " has a login for docker.pkg.github.com, which is a GitHub token\n" +
		"HIGH " + filepath.Join(home, ".docker/config.json") + " has a login for ghcr.io, which is a GitHub token\n" +
		"HIGH " + filepath.Join(home, ".docker/config.json") + " has a login for https://ghcr.io/v2/, which is a GitHub token\n"
	if got := details(dockerGHCR(context.Background(), &Env{Home: home})); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}
