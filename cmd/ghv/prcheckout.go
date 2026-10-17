package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/Sriram-PR/ghv/internal/sanitize"
)

func init() {
	register(&command{
		path: "pr checkout", args: []string{"N"}, flags: []flag{repoFlag},
		help: "Fetch a pull request into branch pr-N over anonymous https; no token or SSH key is used",
		run:  runPRCheckout,
	})
}

// prCheckout is the stable --json schema of ghv pr checkout.
type prCheckout struct {
	Repo   string `json:"repo"`
	Number int    `json:"number"`
	Branch string `json:"branch"`
}

// checkoutArgs is ghv's only git call. The -c options clear credential
// helpers, askpass, and extra headers from the user's git config, and
// allow https only, so an insteadOf rewrite to SSH cannot reach a key.
func checkoutArgs(repo string, n int) []string {
	return []string{
		"-c", "credential.helper=", "-c", "core.askPass=", "-c", "http.extraHeader=",
		"-c", "protocol.allow=never", "-c", "protocol.https.allow=always",
		"fetch", "https://github.com/" + repo + ".git", fmt.Sprintf("pull/%d/head:pr-%d", n, n),
	}
}

func runPRCheckout(ctx context.Context, a *app) error {
	repo, n, err := a.repoAndNumber()
	if err != nil {
		return err
	}
	if _, err := findGitDir(a.dir); err != nil {
		return usagef("run ghv pr checkout inside a git checkout: %v", err)
	}
	out, err := a.runGit(ctx, a.dir, checkoutArgs(repo, n)...)
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("git fetch failed: %s", msg)
	}
	io.WriteString(a.stderr, sanitize.Terminal(string(out)))
	v := prCheckout{repo, n, fmt.Sprintf("pr-%d", n)}
	if a.json() {
		return writeJSON(a.stdout, v)
	}
	fmt.Fprintf(a.stdout, "fetched %s#%d into branch %s; run git switch %s\n", repo, n, v.Branch, v.Branch)
	return nil
}

func (e env) runGit(ctx context.Context, dir string, args ...string) ([]byte, error) {
	if e.git != nil {
		return e.git(ctx, dir, args...)
	}
	cmd := exec.CommandContext(ctx, "git", args...)
	cmd.Dir = dir
	cmd.Env = gitEnv(os.Environ())
	return cmd.CombinedOutput()
}

// gitEnv drops GIT_* variables and SSH_ASKPASS, which can add config or
// prompt for credentials, and stops git from prompting on the terminal.
func gitEnv(environ []string) []string {
	var out []string
	for _, kv := range environ {
		if strings.HasPrefix(kv, "GIT_") || strings.HasPrefix(kv, "SSH_ASKPASS=") {
			continue
		}
		out = append(out, kv)
	}
	return append(out, "GIT_TERMINAL_PROMPT=0")
}
