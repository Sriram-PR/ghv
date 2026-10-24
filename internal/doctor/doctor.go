// Package doctor reports what the process running it can reach that ghv
// cannot guard: other GitHub credentials, the human's own user, a
// ghv-apply it could replace. Both binaries run it; each injects its own
// GitHub reads, so this package sends nothing itself.
package doctor

import (
	"context"
	"fmt"
	"io"
	"slices"

	"github.com/Sriram-PR/ghv/internal/sanitize"
)

type Severity int

const (
	Info Severity = iota
	Medium
	High
)

func (s Severity) String() string {
	switch s {
	case High:
		return "HIGH"
	case Medium:
		return "MEDIUM"
	}
	return "INFO"
}

func (s Severity) MarshalText() ([]byte, error) { return []byte(s.String()), nil }

// Finding is one thing the process can reach. Detail and Fix never hold a
// secret's value: a path, a variable's name, a token's prefix at most.
type Finding struct {
	Check    string   `json:"check"`
	Severity Severity `json:"severity"`
	Detail   string   `json:"detail"`
	Fix      string   `json:"fix,omitempty"`
}

// Human is the user the agent must not act as, from --human-user.
type Human struct {
	Name string
	UID  int
	Home string
}

// Env is everything the checks read, so tests can point them at planted
// homes and a fake /proc.
type Env struct {
	GOOS  string
	UID   int
	User  string // the running user's name
	Home  string // the running user's passwd home
	Human *Human // nil when --human-user was not given
	PID   int
	// Getenv reads the environment this process was started with; EnvOf
	// names it in findings. nil when it cannot be read; EnvErr says why.
	Getenv func(string) string
	EnvOf  string
	EnvErr error
	EnvPID int    // the process Getenv reads; the /proc scan skips it
	Proc   string // "/proc", or "" where there is none
	// Runtime holds each user's runtime directory ("/run/user" on Linux),
	// and Tmp is where ssh-agent and launchd put sockets ("/tmp", or
	// "/private/tmp" on macOS); "" skips them.
	Runtime string
	Tmp     string
	// Writable reports whether this process can change path; nil uses
	// the kernel's answer.
	Writable func(path string) bool
	// ApplyPaths are the ghv-apply binaries to check.
	ApplyPaths []string
	// TokenPath is the read token file; its acknowledgement sits beside it.
	TokenPath string
	// Scopes sends token to GitHub once and returns its X-OAuth-Scopes;
	// reported is false when GitHub sent no such header.
	Scopes func(ctx context.Context, token string) (scopes []string, reported bool, err error)
	// AckFineGrained asks, through Confirm, to record that a fine-grained
	// read token is read-only. Confirm is nil without a terminal.
	AckFineGrained bool
	Confirm        func(prompt string) (bool, error)
	// Login is the GitHub account whose SSH keys are compared, from
	// --login; WhoAmI finds one when it is empty, and may be nil.
	// UserKeys lists an account's keys as "TYPE BASE64" lines; nil leaves
	// keys uncompared.
	Login    string
	WhoAmI   func(ctx context.Context) (string, error)
	UserKeys func(ctx context.Context, login string) ([]string, error)

	gh *githubKeys
}

type check struct {
	name string
	run  func(context.Context, *Env) []Finding
}

// checks is filled by each check file's init, in file name order.
var checks []check

func register(name string, run func(context.Context, *Env) []Finding) {
	checks = append(checks, check{name, run})
}

// Report is what Run found. Verdict is "Contained" when nothing is HIGH.
type Report struct {
	User      string    `json:"user"`
	UID       int       `json:"uid"`
	HumanUser string    `json:"human_user,omitempty"`
	Findings  []Finding `json:"findings"`
	Verdict   string    `json:"verdict"`
}

func Run(ctx context.Context, e Env) Report {
	return run(ctx, &e, checks)
}

func run(ctx context.Context, e *Env, cs []check) Report {
	r := Report{User: sanitize.Line(e.User), UID: e.UID, Findings: []Finding{}}
	if e.Human != nil {
		r.HumanUser = sanitize.Line(e.Human.Name)
	}
	for _, c := range cs {
		for _, f := range c.run(ctx, e) {
			f.Check = c.name
			f.Detail, f.Fix = sanitize.Line(f.Detail), sanitize.Line(f.Fix)
			r.Findings = append(r.Findings, f)
		}
	}
	slices.SortStableFunc(r.Findings, func(a, b Finding) int { return int(b.Severity) - int(a.Severity) })
	r.Verdict = "Contained"
	if r.High() > 0 {
		r.Verdict = "Uncontained"
	}
	return r
}

func (r Report) High() int {
	n := 0
	for _, f := range r.Findings {
		if f.Severity == High {
			n++
		}
	}
	return n
}

// HighError is the error a doctor command returns when a finding is HIGH.
type HighError struct{ N int }

func (e HighError) Error() string {
	if e.N == 1 {
		return "1 HIGH finding"
	}
	return fmt.Sprintf("%d HIGH findings", e.N)
}

// Err is a HighError when a finding is HIGH, else nil.
func (r Report) Err() error {
	if n := r.High(); n > 0 {
		return HighError{n}
	}
	return nil
}

func (r Report) WriteText(w io.Writer) {
	who := fmt.Sprintf("%s (uid %d)", r.User, r.UID)
	if r.HumanUser != "" {
		who += "; the human is " + r.HumanUser
	}
	fmt.Fprintf(w, "What this process can reach, running as %s:\n\n", who)
	if len(r.Findings) == 0 {
		fmt.Fprintln(w, "Nothing found.")
	}
	width := 0
	for _, f := range r.Findings {
		width = max(width, len(f.Check))
	}
	for _, f := range r.Findings {
		fmt.Fprintf(w, "%-6s  %-*s  %s\n", f.Severity, width, f.Check, f.Detail)
		if f.Fix != "" {
			fmt.Fprintf(w, "%-6s  %-*s  fix: %s\n", "", width, "", f.Fix)
		}
	}
	if n := r.High(); n > 0 {
		fmt.Fprintf(w, "\nVerdict: Uncontained (%s). The agent can act as you on GitHub by these routes; fix them, or run it as its own user.\n", HighError{n})
	} else {
		fmt.Fprintln(w, "\nVerdict: Contained (no HIGH findings).")
	}
}
