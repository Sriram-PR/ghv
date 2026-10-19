package doctor

import (
	"context"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// plant writes files under home, relative to it, with mode 0600.
func plant(t *testing.T, home string, files map[string]string) {
	t.Helper()
	for name, data := range files {
		p := filepath.Join(home, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCredentialFiles(t *testing.T) {
	home, human := t.TempDir(), t.TempDir()
	plant(t, home, map[string]string{
		".git-credentials":        "https://sriram:abc@github.com\nhttps://u:p@gitlab.com\nhttps://x:y@GIST.GitHub.com\n",
		".config/git/credentials": "https://u:p@example.com\n",
		".netrc":                  "machine api.github.com login u password p\nmachine example.com login a password b\n",
	})
	plant(t, human, map[string]string{".netrc": "default login u password p\n"})
	if err := syscall.Mkfifo(filepath.Join(human, ".git-credentials"), 0o600); err != nil {
		t.Fatal(err)
	}
	e := &Env{Home: home, Human: &Human{Home: human}}
	want := "HIGH " + filepath.Join(home, ".git-credentials") + " holds 2 credentials for github.com\n" +
		"HIGH " + filepath.Join(home, ".netrc") + " holds 1 entry for github.com\n" +
		"HIGH " + filepath.Join(human, ".netrc") + " holds 1 entry for github.com\n"
	if got := details(credentialFiles(context.Background(), e)); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestCredentialFilesUnreadable(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("root reads anything")
	}
	home := t.TempDir()
	plant(t, home, map[string]string{".git-credentials": "https://u:p@github.com\n"})
	os.Chmod(filepath.Join(home, ".git-credentials"), 0)
	if got := credentialFiles(context.Background(), &Env{Home: home}); len(got) != 0 {
		t.Fatalf("a file this process cannot read is not reachable: %v", got)
	}
}
