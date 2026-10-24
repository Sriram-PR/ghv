package doctor

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"
)

// fakeAgent answers SSH_AGENTC_REQUEST_IDENTITIES at path with blobs.
func fakeAgent(t *testing.T, path string, blobs ...[]byte) {
	t.Helper()
	os.MkdirAll(filepath.Dir(path), 0o700)
	l, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			req := make([]byte, 5)
			if _, err := io.ReadFull(c, req); err != nil || string(req) != "\x00\x00\x00\x01\x0b" {
				c.Close()
				continue
			}
			body := binary.BigEndian.AppendUint32([]byte{12}, uint32(len(blobs)))
			for _, b := range blobs {
				body = append(body, sshStr(b)...)
				body = append(body, sshStr([]byte("a comment"))...)
			}
			c.Write(append(binary.BigEndian.AppendUint32(nil, uint32(len(body))), body...))
			c.Close()
		}
	}()
}

func TestSSHAgents(t *testing.T) {
	home, run, tmp, proc := t.TempDir(), t.TempDir(), t.TempDir(), t.TempDir()
	onePassword := filepath.Join(home, ".1password/agent.sock")
	fakeAgent(t, onePassword, ed25519Blob(1), ed25519Blob(2))
	gcr := filepath.Join(run, "1001/gcr/ssh")
	fakeAgent(t, gcr, ed25519Blob(3))
	empty := filepath.Join(tmp, "ssh-XXXXabcd/agent.123")
	fakeAgent(t, empty)
	fromProc := filepath.Join(tmp, "elsewhere.sock")
	fakeAgent(t, fromProc, ed25519Blob(4))
	plant(t, proc, map[string]string{"77/environ": "SSH_AUTH_SOCK=" + fromProc + "\x00"})
	stale := filepath.Join(home, ".gnupg/S.gpg-agent.ssh")
	os.MkdirAll(filepath.Dir(stale), 0o700)
	if l, err := net.Listen("unix", stale); err == nil {
		l.(*net.UnixListener).SetUnlinkOnClose(false)
		l.Close()
	}
	plant(t, home, map[string]string{".bitwarden-ssh-agent.sock": "not a socket"})
	registered := "ssh-ed25519 " + base64.StdEncoding.EncodeToString(ed25519Blob(1))
	var prompts []string
	answer := func(yes bool, err error) func(string) (bool, error) {
		return func(p string) (bool, error) { prompts = append(prompts, p); return yes, err }
	}
	e := Env{Home: home, UID: 1001, Runtime: run, Tmp: tmp, Proc: proc, PID: 1,
		Getenv: func(k string) string { return map[string]string{"SSH_AUTH_SOCK": onePassword}[k] },
		Login:  "octo", UserKeys: func(context.Context, string) ([]string, error) { return []string{registered}, nil }}
	k := func(seed byte) string { return "ssh-ed25519 " + fingerprint(ed25519Blob(seed)) }
	agents := []string{
		"ssh-agent at " + onePassword + " holds 2 keys this user can use without a passphrase: " + k(1) + ", " + k(2) + "; 1 of them registered on GitHub for octo",
		"ssh-agent at " + fromProc + " holds 1 key this user can use without a passphrase: " + k(4) + "; none of them registered on GitHub for octo",
		"ssh-agent at " + gcr + " holds 1 key this user can use without a passphrase: " + k(3) + "; none of them registered on GitHub for octo",
	}
	for _, c := range []struct {
		name, severity, suffix string
		confirm                func(string) (bool, error)
	}{
		{"no", "HIGH", "", answer(false, nil)},
		{"yes", "MEDIUM", "; you said each use asks you first (ssh-add -c)", answer(true, nil)},
		{"no terminal", "HIGH", "; cannot ask whether they were added with ssh-add -c: no terminal", answer(false, errors.New("no terminal"))},
		{"no Confirm", "HIGH", "; not asked whether they were added with ssh-add -c", nil},
	} {
		prompts = nil
		e := e
		e.Confirm = c.confirm
		want := ""
		for _, a := range agents {
			want += c.severity + " " + a + c.suffix + "\n"
		}
		want += "INFO ssh-agent at " + empty + " is reachable and holds no keys now; keys added later will be reachable too\n"
		fs := sshAgents(context.Background(), &e)
		if got := details(fs); got != want {
			t.Errorf("%s: got:\n%s\nwant:\n%s", c.name, got, want)
		}
		if c.name == "no" && (len(prompts) != 3 ||
			prompts[0] != "ssh-agent at "+onePassword+" holds 2 keys. Were they all added with ssh-add -c, so each use asks you first? Type yes if so: " ||
			fs[0].Fix != "load keys with ssh-add -c so each use asks you, or remove them: SSH_AUTH_SOCK="+onePassword+" ssh-add -D; or run the agent as its own user") {
			t.Errorf("no: prompts %q, fix %q", prompts, fs[0].Fix)
		}
	}
}
