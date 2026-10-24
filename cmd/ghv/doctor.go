package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Sriram-PR/ghv/internal/doctor"
	"github.com/Sriram-PR/ghv/internal/read"
)

var (
	humanUserFlag = flag{name: "human-user", kind: stringFlag, arg: "NAME", help: "the human's user name, to check the agent does not run as it"}
	ackFlag       = flag{name: "ack-fine-grained", kind: boolFlag, help: "after asking on the terminal, record that a fine-grained read token is read-only"}
	loginFlag     = flag{name: "login", kind: stringFlag, arg: "LOGIN", help: "your GitHub login, to compare SSH keys with your account's (default: the read token's)"}
)

func init() {
	register(&command{path: "doctor", flags: []flag{humanUserFlag, ackFlag, loginFlag},
		help: "Report what this process can reach that ghv cannot guard: other GitHub credentials, the human's own user, a writable ghv-apply", run: runDoctor})
}

// runDoctor exits 4 when a finding is HIGH; the report still prints.
func runDoctor(ctx context.Context, a *app) error {
	e, err := a.doctorEnv()
	if err != nil {
		return err
	}
	r := doctor.Run(ctx, e)
	if a.json() {
		if err := writeJSON(a.stdout, r); err != nil {
			return err
		}
	} else {
		r.WriteText(a.stdout)
	}
	return r.Err()
}

func (a *app) doctorEnv() (doctor.Env, error) {
	e := doctor.Env{
		GOOS: a.goos, UID: a.uid, User: a.username, Home: a.passwdHome, PID: a.pid,
		Getenv: a.getenv, EnvOf: "this process's environment", EnvPID: a.pid, Proc: a.proc, Runtime: a.runtimeDir, Tmp: a.tmpDir,
		ApplyPaths: a.applyPaths(), TokenPath: a.tokenPath(), Scopes: a.tokenScopes,
		AckFineGrained: a.inv.bool("ack-fine-grained"), Confirm: a.confirm,
		Login: a.inv.str("login"), WhoAmI: a.whoAmI, UserKeys: a.userKeys, Exec: a.runProgram,
	}
	if e.Login != "" && !doctor.ValidLogin(e.Login) {
		return doctor.Env{}, usagef("--login %s is not a GitHub login", e.Login)
	}
	if e.Home == "" {
		e.Home = a.home
	}
	if name := a.inv.str("human-user"); name != "" {
		u, err := a.lookupUser(name)
		if err != nil {
			return doctor.Env{}, usagef("--human-user %s: %v", name, err)
		}
		uid, err := strconv.Atoi(u.Uid)
		if err != nil {
			return doctor.Env{}, fmt.Errorf("--human-user %s: uid %q: %v", name, u.Uid, err)
		}
		e.Human = &doctor.Human{Name: u.Username, UID: uid, Home: u.HomeDir}
	}
	return e, nil
}

// applyPaths are the ghv-apply binaries on PATH and in systemBin.
func (a *app) applyPaths() []string {
	var out []string
	for _, d := range append(filepath.SplitList(a.getenv("PATH")), a.systemBin) {
		p := filepath.Join(d, "ghv-apply")
		if fi, err := os.Stat(p); filepath.IsAbs(d) && err == nil && fi.Mode().IsRegular() && !slices.Contains(out, p) {
			out = append(out, p)
		}
	}
	return out
}

// tokenScopes sends the read token once, uncached, and reads its scopes.
func (a *app) tokenScopes(ctx context.Context, tok string) ([]string, bool, error) {
	c := read.NewClient(read.Options{Token: tok, Transport: a.transport})
	resp, err := c.Get(ctx, "rate_limit", "")
	var te *read.TokenError
	if errors.As(err, &te) {
		return te.Info.Scopes, true, nil
	}
	if err != nil {
		return nil, false, err
	}
	info := read.Inspect(read.KindOf(tok), resp.Header)
	return info.Scopes, info.Scopes != nil, nil
}

// whoAmI is the read token's login, or "" without a token.
func (a *app) whoAmI(ctx context.Context) (string, error) {
	c, err := a.read()
	if err != nil || !c.Authenticated() {
		return "", err
	}
	resp, err := c.Get(ctx, "user", "")
	if err != nil {
		return "", err
	}
	var u struct {
		Login string `json:"login"`
	}
	if err := json.Unmarshal(resp.Body, &u); err != nil {
		return "", fmt.Errorf("GET user: %v", err)
	}
	return u.Login, nil
}

func (a *app) userKeys(ctx context.Context, login string) ([]string, error) {
	c, err := a.read()
	if err != nil {
		return nil, err
	}
	resp, err := c.Get(ctx, "users/"+url.PathEscape(login)+"/keys?per_page=100", "")
	if err != nil {
		return nil, err
	}
	var keys []struct {
		Key string `json:"key"`
	}
	if err := json.Unmarshal(resp.Body, &keys); err != nil {
		return nil, fmt.Errorf("GET users/%s/keys: %v", login, err)
	}
	out := make([]string, len(keys))
	for i, k := range keys {
		out[i] = k.Key
	}
	return out, nil
}

// runProgram runs git from PATH, or macOS's security, for doctor. git's
// environment is gitEnv's, with Git Credential Manager's prompts off too.
func runProgram(ctx context.Context, stdin, name string, args ...string) ([]byte, error) {
	path := map[string]string{"git": "git", "security": "/usr/bin/security"}[name]
	if path == "" {
		return nil, fmt.Errorf("doctor runs only git and security, not %q", name)
	}
	cmd := exec.CommandContext(ctx, path, args...)
	cmd.Env = append(gitEnv(os.Environ()), "GCM_INTERACTIVE=never")
	cmd.Stdin = strings.NewReader(stdin)
	// A helper may start a daemon that keeps stdout open.
	cmd.WaitDelay = time.Second
	out, err := cmd.Output()
	var ee *exec.ExitError
	if errors.As(err, &ee) {
		return out, doctor.ExitError{Code: ee.ExitCode()}
	}
	return out, err
}

// confirmOnTTY asks on /dev/tty, never stdin, so the agent cannot answer.
// A line is read a byte at a time to its end, so nothing is left for the
// shell.
func confirmOnTTY(prompt string) (bool, error) {
	f, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err != nil {
		return false, fmt.Errorf("no terminal: %v", err)
	}
	defer f.Close()
	fmt.Fprint(f, prompt)
	var line []byte
	b := make([]byte, 1)
	for {
		n, err := f.Read(b)
		if n == 1 && b[0] == '\n' {
			break
		}
		if n == 1 && len(line) < 256 {
			line = append(line, b[0])
		}
		if err != nil {
			return false, err
		}
	}
	return strings.EqualFold(strings.TrimSpace(string(line)), "yes"), nil
}
