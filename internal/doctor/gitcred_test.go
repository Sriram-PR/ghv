package doctor

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"
)

type execReply struct {
	out string
	err error
}

// fakeExec answers git's credential fill and config calls; a nil fill
// reply waits for the context to end.
func fakeExec(t *testing.T, fill *execReply, config execReply) func(context.Context, string, string, ...string) ([]byte, error) {
	return func(ctx context.Context, stdin, name string, args ...string) ([]byte, error) {
		switch {
		case name == "git" && slices.Equal(args, []string{"-c", "core.askPass=", "-c", "credential.interactive=never", "credential", "fill"}):
			if stdin != "protocol=https\nhost=github.com\n\n" {
				t.Errorf("stdin %q", stdin)
			}
			if fill == nil {
				<-ctx.Done()
				return nil, ctx.Err()
			}
			return []byte(fill.out), fill.err
		case name == "git" && slices.Equal(args, []string{"config", "--get-regexp", `^credential\..*helper$`}):
			return []byte(config.out), config.err
		}
		t.Errorf("unexpected %s %q", name, args)
		return nil, errors.New("unexpected")
	}
}

func TestGitCredentialFill(t *testing.T) {
	defer func(d time.Duration) { gitTimeout = d }(gitTimeout)
	gitTimeout = 50 * time.Millisecond
	pass := "ghp_" + strings.Repeat("z", 36)
	filled := &execReply{out: "protocol=https\nhost=github.com\nusername=octo\npassword=" + pass + "\n"}
	none := &execReply{err: ExitError{Code: 128}}
	helpers := execReply{out: "credential.helper store\n" +
		"credential.https://github.com.helper \n" +
		"credential.https://github.com.helper !/usr/bin/gh auth git-credential\n" +
		"credential.https://gitlab.com.helper libsecret\n" +
		"credential.helper !f() { echo password=" + pass + "; }; f\n" +
		"credential.helper store\n"}
	noHelpers := execReply{err: ExitError{Code: 1}}
	for _, c := range []struct {
		name   string
		fill   *execReply
		config execReply
		want   string
	}{
		{"a password", filled, helpers, "HIGH git credential fill returns a password for https://github.com (credential helpers: store, gh, a custom helper)\n"},
		{"a password, helpers unknown", filled, noHelpers, "HIGH git credential fill returns a password for https://github.com\n"},
		{"helpers, no password", none, helpers, "INFO git credential helpers are set (store, gh, a custom helper), but none returned a password for https://github.com\n"},
		{"nothing", none, noHelpers, ""},
		{"no git", &execReply{err: errors.New(`exec: "git": executable file not found in $PATH`)}, noHelpers,
			"INFO git credential fill could not run: exec: \"git\": executable file not found in $PATH\n"},
		{"hangs", nil, helpers, "INFO git credential fill did not finish in 50ms; credential helpers not checked\n"},
	} {
		fs := gitCredentialFill(context.Background(), &Env{Exec: fakeExec(t, c.fill, c.config)})
		got := details(fs)
		if got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
		for _, f := range fs {
			if strings.Contains(f.Detail+f.Fix, pass) {
				t.Errorf("%s: the password is in the finding", c.name)
			}
		}
		if c.name == "a password" && fs[0].Fix != `erase it: printf 'protocol=https\nhost=github.com\n\n' | git credential reject; remove the helper from your git config, and revoke the token at https://github.com/settings/tokens; or run the agent as its own user` {
			t.Errorf("fix %q", fs[0].Fix)
		}
	}
	if fs := gitCredentialFill(context.Background(), &Env{}); fs != nil {
		t.Fatalf("no Exec: %v", fs)
	}
}
