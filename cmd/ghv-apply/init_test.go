package main

import (
	"bytes"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/Sriram-PR/ghv/internal/harden"
	"github.com/Sriram-PR/ghv/internal/vault"
)

const testToken = "ghp_abcdefghijklmnopqrstuvwxyz0123456789"

// fakeTerm answers prompts in order. An answer is a function of what the
// terminal has shown so far, so a test can type back a generated
// passphrase.
type fakeTerm struct {
	t       *testing.T
	out     strings.Builder
	prompts []string
	answers []func(shown string) string
	secrets [][]byte
	height  int
}

func answers(t *testing.T, as ...any) *fakeTerm {
	ft := &fakeTerm{t: t}
	for _, a := range as {
		switch a := a.(type) {
		case string:
			ft.answers = append(ft.answers, func(string) string { return a })
		case func(string) string:
			ft.answers = append(ft.answers, a)
		}
	}
	return ft
}

func (f *fakeTerm) answer(prompt string) (string, error) {
	f.out.WriteString(prompt)
	f.prompts = append(f.prompts, prompt)
	if len(f.answers) == 0 {
		return "", fmt.Errorf("unexpected prompt %q", prompt)
	}
	a := f.answers[0](f.out.String())
	f.answers = f.answers[1:]
	f.out.WriteString("\n")
	return a, nil
}

func (f *fakeTerm) ReadSecret(p string) ([]byte, error) {
	a, err := f.answer(p)
	b := []byte(a)
	f.secrets = append(f.secrets, b)
	return b, err
}
func (f *fakeTerm) ReadLine(p string) (string, error) { return f.answer(p) }
func (f *fakeTerm) Printf(format string, a ...any)    { fmt.Fprintf(&f.out, format, a...) }
func (f *fakeTerm) Close() error                      { return nil }
func (f *fakeTerm) Height() int                       { return f.height }
func (f *fakeTerm) unanswered() int                   { return len(f.answers) }
func (f *fakeTerm) shown() string                     { return f.out.String() }

type fakeGitHub struct {
	reqs   []*http.Request
	scopes string
	login  string
	expiry string
}

func (f *fakeGitHub) RoundTrip(r *http.Request) (*http.Response, error) {
	f.reqs = append(f.reqs, r)
	h := http.Header{"X-Oauth-Scopes": {f.scopes}}
	if f.expiry != "" {
		h.Set("Github-Authentication-Token-Expiration", f.expiry)
	}
	body := fmt.Sprintf(`{"login":%q}`, f.login)
	return &http.Response{StatusCode: 200, Header: h, Body: io.NopCloser(strings.NewReader(body)), Request: r}, nil
}

func github(login string) *fakeGitHub {
	return &fakeGitHub{scopes: "public_repo", login: login, expiry: "2027-01-16 08:00:00 UTC"}
}

func ttyEnv(t *testing.T, ft *fakeTerm, gh *fakeGitHub) env {
	e := testEnv(t)
	e.openTTY = func() (terminal, error) { return ft, nil }
	e.client = harden.Client(gh)
	e.rand = rand.Reader
	return e
}

var passphraseRE = regexp.MustCompile(`Your passphrase:\n\n    ([a-z -]+)\n`)

func typeGenerated(shown string) string {
	m := passphraseRE.FindAllStringSubmatch(shown, -1)
	if m == nil {
		return "no passphrase shown"
	}
	return m[len(m)-1][1]
}

func openVault(t *testing.T, home string, pass []byte) vault.Secret {
	t.Helper()
	path := vault.Path(home)
	if fi, err := os.Stat(path); err != nil || fi.Mode().Perm() != 0o600 {
		t.Fatalf("vault: %v, %v", fi, err)
	}
	data, err := vault.Load(path)
	if err != nil {
		t.Fatal(err)
	}
	s, err := vault.Open(data, pass)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestInitClearsTheTokenAsTyped(t *testing.T) {
	ft := answers(t, "  "+testToken+" \t", "", typeGenerated)
	if r := runWith(ttyEnv(t, ft, github("Sriram-PR")), "init"); r.code != 0 {
		t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
	}
	if typed := ft.secrets[0]; strings.Trim(string(typed), "\x00") != "" {
		t.Fatalf("the token as typed is left as %q", typed)
	}
}

func TestInitGeneratedPassphrase(t *testing.T) {
	ft := answers(t, testToken, "", typeGenerated)
	gh := github("Sriram-PR")
	e := ttyEnv(t, ft, gh)
	r := runWith(e, "init")
	if r.code != 0 || r.stdout != "" || r.stderr != "" || ft.unanswered() != 0 {
		t.Fatalf("exit %d, stdout %q, stderr %q, terminal:\n%s", r.code, r.stdout, r.stderr, ft.shown())
	}
	want := []string{"Write token: ", "Generate a passphrase? [Y/n] ", "Type the passphrase again: "}
	if strings.Join(ft.prompts, "|") != strings.Join(want, "|") {
		t.Fatalf("prompts %q", ft.prompts)
	}
	for _, s := range []string{"Clipboard history may keep", "The token belongs to Sriram-PR and expires on 2027-01-16.", "Saved the vault to " + vault.Path(e.home)} {
		if !strings.Contains(ft.shown(), s) {
			t.Fatalf("terminal lacks %q:\n%s", s, ft.shown())
		}
	}
	if len(gh.reqs) != 1 || gh.reqs[0].URL.Path != "/user" {
		t.Fatalf("requests %v", gh.reqs)
	}
	s := openVault(t, e.home, []byte(typeGenerated(ft.shown())))
	exp := time.Date(2027, 1, 16, 8, 0, 0, 0, time.UTC)
	if s.Login != "Sriram-PR" || string(s.Token) != testToken || s.Type != "classic" ||
		!s.Created.Equal(e.now()) || s.Expires == nil || !s.Expires.Equal(exp) {
		t.Fatalf("vault holds %+v", s)
	}
}

func TestInitTypedPassphrase(t *testing.T) {
	pass := "a typed passphrase, long enough"
	ft := answers(t, "  "+testToken+"  ", "n", pass, pass)
	gh := github("Sriram-PR")
	gh.expiry = ""
	e := ttyEnv(t, ft, gh)
	if r := runWith(e, "init"); r.code != 0 {
		t.Fatalf("exit %d, stderr %q, terminal:\n%s", r.code, r.stderr, ft.shown())
	}
	if !strings.Contains(ft.shown(), "The token belongs to Sriram-PR and never expires.") {
		t.Fatalf("terminal:\n%s", ft.shown())
	}
	s := openVault(t, e.home, []byte(pass))
	if string(s.Token) != testToken || s.Expires != nil {
		t.Fatalf("vault holds %+v", s)
	}
}

func TestInitWarnsAboutExpiry(t *testing.T) {
	pass := "a typed passphrase, long enough"
	gh := github("Sriram-PR")
	gh.expiry = "2026-10-21 12:00:00 UTC"
	e := ttyEnv(t, answers(t, testToken, "n", pass, pass), gh)
	r := runWith(e, "init")
	if r.code != 0 || r.stderr != "ghv-apply: warning: the write token expires on 2026-10-21, in 3 days; replace it with ghv-apply rotate\n" {
		t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
	}
}

func TestInitRefusals(t *testing.T) {
	long := "a typed passphrase, long enough"
	tests := []struct {
		name     string
		ft       []any
		scopes   string
		existing bool
		requests int
		want     string
	}{
		{"existing vault", nil, "public_repo", true, 0, "a vault already exists at"},
		{"fine-grained", []any{"github_pat_11ABCDEFG0123456789"}, "public_repo", false, 0, "classic personal access token"},
		{"broad scopes", []any{testToken}, "public_repo, repo", false, 1, "only public_repo"},
		{"bad answer", []any{testToken, "maybe"}, "public_repo", false, 1, "answer y or n"},
		{"short", []any{testToken, "n", "too short"}, "public_repo", false, 1, "at least 20 characters"},
		{"mismatch", []any{testToken, "n", long, long + "!"}, "public_repo", false, 1, "the passphrases do not match"},
		{"generated mismatch", []any{testToken, "y", "not it"}, "public_repo", false, 1, "the passphrases do not match"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ft := answers(t, tt.ft...)
			gh := github("Sriram-PR")
			gh.scopes = tt.scopes
			e := ttyEnv(t, ft, gh)
			path := vault.Path(e.home)
			if tt.existing {
				if err := vault.Create(path, []byte("old")); err != nil {
					t.Fatal(err)
				}
			}
			r := runWith(e, "init")
			if r.code != 1 || !strings.Contains(r.stderr, tt.want) || len(gh.reqs) != tt.requests || ft.unanswered() != 0 {
				t.Fatalf("exit %d, stderr %q, %d requests, terminal:\n%s", r.code, r.stderr, len(gh.reqs), ft.shown())
			}
			data, err := os.ReadFile(path)
			if tt.existing && string(data) != "old" {
				t.Fatalf("existing vault changed to %q, %v", data, err)
			}
			if !tt.existing && !errors.Is(err, fs.ErrNotExist) {
				t.Fatalf("vault written: %v", err)
			}
		})
	}
}

// The real binary, in a new session with no terminal and `yes` on stdin,
// under a hostile environment: it must refuse before touching anything.
func TestInitBinaryNeedsATerminal(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "ghv-apply")
	if out, err := exec.Command("go", "build", "-o", bin, ".").CombinedOutput(); err != nil {
		t.Fatalf("build: %v\n%s", err, out)
	}
	home := t.TempDir()
	cmd := exec.Command(bin, "init")
	cmd.Env = []string{"HOME=" + home, "XDG_CONFIG_HOME=" + home, "HTTPS_PROXY=http://127.0.0.1:1", "GODEBUG=http2client=0"}
	cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
	cmd.Stdin = strings.NewReader(strings.Repeat("yes\n", 100))
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	var ee *exec.ExitError
	if !errors.As(err, &ee) || ee.ExitCode() != 1 || !strings.Contains(stderr.String(), "no terminal: passphrases and decisions come from /dev/tty only") {
		t.Fatalf("%v, stderr %q", err, stderr.String())
	}
	if ents, _ := os.ReadDir(home); len(ents) != 0 {
		t.Fatalf("wrote into $HOME: %v", ents)
	}
}
