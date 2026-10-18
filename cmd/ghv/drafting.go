package main

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf8"

	"github.com/Sriram-PR/ghv/internal/draft"
	"github.com/Sriram-PR/ghv/internal/sanitize"
)

// GitHub refuses issue, pull request, and comment bodies over this many
// characters.
const maxBodyChars = 65536

const maxRefusalsShown = 5

var (
	draftRepoFlag = flag{name: "repo", short: "R", kind: stringFlag, arg: "OWNER/REPO", help: "repository the draft is for (required)"}
	titleFlag     = flag{name: "title", kind: stringFlag, arg: "T", help: "title"}
	bodyFileFlag  = flag{name: "body-file", kind: stringFlag, arg: "F", help: "read the body from file F, or from stdin for -"}
)

// draftResult is the stable --json schema of every drafting command.
type draftResult struct {
	ID   string     `json:"id"`
	Kind draft.Kind `json:"kind"`
	Repo string     `json:"repo"`
}

// draftRepo returns -R, which every draft needs: a write's target is not
// guessed from the checkout.
func (a *app) draftRepo() (string, error) {
	r := a.inv.str("repo")
	if r == "" {
		return "", usagef("drafts need -R OWNER/REPO")
	}
	if !validRepo(r) {
		return "", usagef("-R %q is not OWNER/REPO", r)
	}
	return r, nil
}

func (a *app) requireFlags(names ...string) error {
	for _, n := range names {
		if _, ok := a.inv.values[n]; !ok {
			return usagef("ghv %s needs --%s", a.inv.cmd.path, n)
		}
	}
	return nil
}

// body reads --body-file, relative to the working directory, or stdin for
// "-". CRLF line endings become LF, since ghv-apply refuses a \r.
func (a *app) body() (string, error) {
	name := a.inv.str("body-file")
	var r io.Reader = a.stdin
	if name != "-" {
		p := name
		if !filepath.IsAbs(p) {
			p = filepath.Join(a.dir, p)
		}
		f, err := os.Open(p)
		if err != nil {
			return "", usageError{err.Error()}
		}
		defer f.Close()
		r = f
	}
	if r == nil {
		return "", usagef("no stdin to read the body from")
	}
	b, err := io.ReadAll(io.LimitReader(r, draft.MaxSize+1))
	if err != nil {
		return "", err
	}
	if len(b) > draft.MaxSize {
		return "", usagef("%s is over %d bytes", name, draft.MaxSize)
	}
	s := string(b)
	if strings.Contains(s, "\r\n") {
		s = strings.ReplaceAll(s, "\r\n", "\n")
		fmt.Fprintf(a.stderr, "ghv: note: %s has CRLF line endings; the draft uses LF\n", sanitize.Line(name))
	}
	if n := utf8.RuneCountInString(s); n > maxBodyChars {
		fmt.Fprintf(a.stderr, "ghv: warning: %s is %d characters; GitHub refuses bodies over %d\n", sanitize.Line(name), n, maxBodyChars)
	}
	return s, nil
}

// submit queues d for ghv-apply and reports its ID. Whatever ghv-apply
// would refuse is a usage error here, so the agent can fix it; risk flags
// are warnings, since the human decides on those.
func (a *app) submit(d draft.Draft) error {
	d.CreatedAt = a.now().UTC()
	if err := draft.Validate(d); err != nil {
		return usageError{err.Error()}
	}
	report := sanitize.CheckDraft(d)
	var refused []string
	for _, f := range report.Findings {
		if f.Class.Rejects() {
			refused = append(refused, f.String())
		}
	}
	if len(refused) > 0 {
		more := ""
		if len(refused) > maxRefusalsShown {
			more = fmt.Sprintf("; and %d more", len(refused)-maxRefusalsShown)
			refused = refused[:maxRefusalsShown]
		}
		return usagef("ghv-apply would refuse this draft: %s%s", strings.Join(refused, "; "), more)
	}
	for _, f := range report.Findings {
		fmt.Fprintf(a.stderr, "ghv: warning: %s\n", sanitize.Line(f.String()))
	}
	id, err := a.draftQueue().Write(d)
	if errors.Is(err, draft.ErrInvalid) {
		return usageError{err.Error()}
	}
	if err != nil {
		return err
	}
	if a.json() {
		return writeJSON(a.stdout, draftResult{ID: id, Kind: d.Kind(), Repo: d.Repo})
	}
	fmt.Fprintf(a.stdout, "Drafted %s: ask the human to run ghv-apply.\n", id)
	return nil
}
