package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/Sriram-PR/ghv/internal/doctor"
	"github.com/Sriram-PR/ghv/internal/fetch"
)

func init() {
	commands = append(commands, command{name: "doctor", flags: []string{"--ack-fine-grained", "--json"}, values: []string{"--human-user", "--login"},
		help: "report what this process can reach that ghv cannot guard; run it as the agent's user, with --human-user YOU", run: runDoctor})
}

// runDoctor exits 4 when a finding is HIGH; the report still prints.
func runDoctor(ctx context.Context, e env) error {
	de, err := doctorEnv(e)
	if err != nil {
		return err
	}
	r := doctor.Run(ctx, de)
	if slices.Contains(e.args, "--json") {
		b, err := json.MarshalIndent(r, "", "  ")
		if err != nil {
			return err
		}
		fmt.Fprintf(e.stdout, "%s\n", b)
	} else {
		r.WriteText(e.stdout)
	}
	return r.Err()
}

// doctorEnv reads the environment from ghv-apply's parent: harden
// replaced ghv-apply's own before main. The read token is the running
// user's, from the passwd home.
func doctorEnv(e env) (doctor.Env, error) {
	de := doctor.Env{
		GOOS: e.goos, UID: e.uid, User: e.username, Home: e.home, PID: e.pid, Proc: e.proc,
		TokenPath:      filepath.Join(e.home, ".config", "ghv", "read-token"),
		Scopes:         func(ctx context.Context, tok string) ([]string, bool, error) { return tokenScopes(ctx, e.client, tok) },
		AckFineGrained: slices.Contains(e.args, "--ack-fine-grained"),
		Confirm:        func(prompt string) (bool, error) { return confirm(e, prompt) },
	}
	if i := slices.Index(e.args, "--login"); i >= 0 {
		if de.Login = e.args[i+1]; !doctor.ValidLogin(de.Login) {
			return doctor.Env{}, fmt.Errorf("--login %s is not a GitHub login", de.Login)
		}
	}
	de.UserKeys = func(ctx context.Context, login string) ([]string, error) { return userKeys(ctx, e.client, login) }
	if e.executable != "" {
		de.ApplyPaths = []string{e.executable}
	}
	if e.proc != "" && e.ppid > 0 {
		de.EnvOf = fmt.Sprintf("the environment of ghv-apply's parent, pid %d", e.ppid)
		de.EnvPID = e.ppid
		if vars, err := doctor.Environ(e.proc, e.ppid); err == nil {
			de.Getenv = func(k string) string { return vars[k] }
		} else {
			de.EnvErr = err
		}
	}
	if i := slices.Index(e.args, "--human-user"); i >= 0 {
		name := e.args[i+1]
		u, err := e.lookupUser(name)
		if err != nil {
			return doctor.Env{}, fmt.Errorf("--human-user %s: %v", name, err)
		}
		uid, err := strconv.Atoi(u.Uid)
		if err != nil {
			return doctor.Env{}, fmt.Errorf("--human-user %s: uid %q: %v", name, u.Uid, err)
		}
		de.Human = &doctor.Human{Name: u.Username, UID: uid, Home: u.HomeDir}
	}
	return de, nil
}

// userKeys reads an account's SSH keys anonymously; ghv-apply has no
// login of its own to fall back on.
func userKeys(ctx context.Context, c *http.Client, login string) ([]string, error) {
	var keys []struct {
		Key string `json:"key"`
	}
	if err := fetch.New(c).GetJSON(ctx, "/users/"+url.PathEscape(login)+"/keys?per_page=100", &keys); err != nil {
		return nil, err
	}
	out := make([]string, len(keys))
	for i, k := range keys {
		out[i] = k.Key
	}
	return out, nil
}

// tokenScopes sends the read token in one GET /rate_limit, which does not
// count against the rate limit. It is doctor's only use of the read
// token: review context never sees it.
func tokenScopes(ctx context.Context, c *http.Client, tok string) ([]string, bool, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, "https://api.github.com/rate_limit", nil)
	if err != nil {
		return nil, false, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", "ghv-apply")
	req.Header.Set("Authorization", "Bearer "+tok)
	resp, err := c.Do(req)
	if err != nil {
		return nil, false, err
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != http.StatusOK {
		return nil, false, fmt.Errorf("GitHub answered %d", resp.StatusCode)
	}
	vals, ok := resp.Header["X-Oauth-Scopes"]
	if !ok {
		return nil, false, nil
	}
	scopes := []string{}
	for _, v := range vals {
		for s := range strings.SplitSeq(v, ",") {
			if s = strings.TrimSpace(s); s != "" {
				scopes = append(scopes, s)
			}
		}
	}
	return scopes, true, nil
}

func confirm(e env, prompt string) (bool, error) {
	tty, err := e.openTTY()
	if err != nil {
		return false, err
	}
	defer tty.Close()
	ans, err := tty.ReadLine(prompt)
	if err != nil {
		return false, err
	}
	return strings.EqualFold(strings.TrimSpace(ans), "yes"), nil
}
