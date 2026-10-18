package main

import (
	"context"
	"errors"
	"fmt"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/Sriram-PR/ghv/internal/draft"
	"github.com/Sriram-PR/ghv/internal/fetch"
	"github.com/Sriram-PR/ghv/internal/sanitize"
)

// youngAge is when an account or repository is new enough to flag.
const youngAge = 30 * 24 * time.Hour

// reviewContext is what ghv-apply learned live about a draft's target.
// Every string is safe to print. Flags are for the reviewer to weigh;
// Blocks say why the draft cannot be sent.
type reviewContext struct {
	Lines  []string
	Flags  []string
	Blocks []string
	// Diff fetches a pr.create's full diff; nil for other kinds.
	Diff func(context.Context) (string, error)
}

func (rc *reviewContext) line(format string, a ...any) {
	rc.Lines = append(rc.Lines, sanitize.Line(fmt.Sprintf(format, a...)))
}

func (rc *reviewContext) flag(format string, a ...any) {
	rc.Flags = append(rc.Flags, sanitize.Line(fmt.Sprintf(format, a...)))
}

func (rc *reviewContext) block(format string, a ...any) {
	rc.Blocks = append(rc.Blocks, sanitize.Line(fmt.Sprintf(format, a...)))
}

// quote adds multi-line GitHub text, escaped and indented.
func (rc *reviewContext) quote(text string) {
	for l := range strings.SplitSeq(sanitize.Terminal(text), "\n") {
		rc.Lines = append(rc.Lines, "    "+l)
	}
}

// reviewer gathers context for one session. login is the vault's.
type reviewer struct {
	fc    *fetch.Client
	login string
	now   time.Time

	knownDone bool
	known     []string
	knownErr  error
}

func (rv *reviewer) context(ctx context.Context, d draft.Draft) reviewContext {
	var rc reviewContext
	repo, ok := rv.repoContext(ctx, &rc, d.Repo)
	if !ok {
		return rc
	}
	switch f := d.Fields.(type) {
	case draft.CommentFields:
		rv.commentContext(ctx, &rc, repo, f)
	case draft.ReviewReplyFields:
		rv.replyContext(ctx, &rc, repo, f)
	case draft.PREditFields:
		rv.editContext(ctx, &rc, repo, f)
	case draft.ForkFields:
		rv.forkContext(ctx, &rc, repo)
	}
	return rc
}

// repoContext reads the draft's repository; later lookups use the name
// GitHub gives, which is where the request will go.
func (rv *reviewer) repoContext(ctx context.Context, rc *reviewContext, name string) (fetch.Repo, bool) {
	r, err := rv.fc.Repo(ctx, name)
	switch {
	case errors.Is(err, fetch.ErrNotFound):
		rc.block("GitHub has no public repository %s", name)
		return r, false
	case err != nil:
		rc.block("cannot read %s from GitHub: %v", name, err)
		return r, false
	}
	full := r.FullName
	if !strings.EqualFold(full, name) {
		rc.flag("GitHub knows %s as %s (renamed or transferred); ghv-apply sends to %s", name, full, full)
	}
	rc.line("Repository: %s, created %s, %s", full, r.CreatedAt.Format(time.DateOnly), ago(r.CreatedAt, rv.now))
	if rv.now.Sub(r.CreatedAt) < youngAge {
		rc.flag("%s was created %s", full, ago(r.CreatedAt, rv.now))
	}

	owner := r.Owner.Login
	if a, err := rv.fc.User(ctx, owner); err != nil {
		rc.line("Owner: %s (unavailable: %v)", owner, err)
	} else {
		rc.line("Owner: %s (%s), on GitHub since %s, %s", owner, a.Type, a.CreatedAt.Format(time.DateOnly), ago(a.CreatedAt, rv.now))
		if rv.now.Sub(a.CreatedAt) < youngAge {
			rc.flag("%s joined GitHub %s", owner, ago(a.CreatedAt, rv.now))
		}
	}

	if strings.EqualFold(owner, rv.login) {
		rc.line("%s is yours.", full)
	} else if took, err := rv.fc.Involved(ctx, full, rv.login); err != nil {
		rc.line("Your history with %s: unavailable: %v", full, err)
	} else if took {
		rc.line("You have taken part in %s before.", full)
	} else {
		rc.flag("you have not taken part in %s before", full)
	}

	if known, err := rv.knownRepos(ctx); err != nil {
		rc.line("Lookalike check: unavailable: %v", err)
	} else if like := lookalike(full, known); like != "" {
		rc.flag("%s looks like %s, a repository you know", full, like)
	}
	return r, true
}

// knownRepos is the user's own repositories and those in their recent
// public events, read from GitHub once a session.
func (rv *reviewer) knownRepos(ctx context.Context) ([]string, error) {
	if !rv.knownDone {
		rv.knownDone = true
		owned, err := rv.fc.OwnedRepos(ctx, rv.login)
		if err != nil {
			rv.knownErr = err
			return nil, err
		}
		recent, err := rv.fc.RecentRepos(ctx, rv.login)
		if err != nil {
			rv.knownErr = err
			return nil, err
		}
		rv.known = append(owned, recent...)
	}
	return rv.known, rv.knownErr
}

// lookalike returns a known repository one or two edits from name, unless
// name is itself known. Case is ignored, as GitHub ignores it.
func lookalike(name string, known []string) string {
	n := strings.ToLower(name)
	if slices.ContainsFunc(known, func(k string) bool { return strings.ToLower(k) == n }) {
		return ""
	}
	for _, k := range known {
		if d := distance(n, strings.ToLower(k)); d >= 1 && d <= 2 {
			return k
		}
	}
	return ""
}

// distance is the Levenshtein distance between a and b, in runes.
func distance(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	cur := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(rb)]
}

func plural(n int, one, many string) string {
	if n == 1 {
		return "1 " + one
	}
	return fmt.Sprintf("%d %s", n, many)
}

func ago(t, now time.Time) string {
	days := int(now.Sub(t).Hours() / 24)
	switch {
	case days < 1:
		return "less than a day ago"
	case days < 60:
		return plural(days, "day", "days") + " ago"
	case days < 730:
		return fmt.Sprintf("%d months ago", days/30)
	}
	return fmt.Sprintf("%d years ago", days/365)
}

func (rv *reviewer) commentContext(ctx context.Context, rc *reviewContext, repo fetch.Repo, f draft.CommentFields) {
	iss, err := rv.fc.Issue(ctx, repo.FullName, f.Number)
	switch {
	case errors.Is(err, fetch.ErrNotFound):
		rc.block("%s has no issue or pull request #%d", repo.FullName, f.Number)
		return
	case err != nil:
		rc.line("#%d: unavailable: %v", f.Number, err)
		return
	}
	what := "issue"
	if iss.IsPullRequest() {
		what = "pull request"
	}
	rc.line("Comment on %s #%d: %s (%s), opened by %s", what, f.Number, iss.Title, iss.State, iss.User.Login)
	if iss.State == "closed" {
		rc.flag("%s #%d is closed", what, f.Number)
	}
}

func (rv *reviewer) forkContext(ctx context.Context, rc *reviewContext, repo fetch.Repo) {
	if strings.EqualFold(repo.Owner.Login, rv.login) {
		rc.block("%s is yours; GitHub cannot fork a repository into the account that owns it", repo.FullName)
		return
	}
	name := path.Base(repo.FullName)
	mine, err := rv.fc.Repo(ctx, rv.login+"/"+name)
	switch {
	case errors.Is(err, fetch.ErrNotFound):
		rc.line("You have no repository named %s, so the fork will be %s/%s.", name, rv.login, name)
	case err != nil:
		rc.line("Your repository %s/%s: unavailable: %v", rv.login, name, err)
	case mine.Fork && mine.Parent != nil && strings.EqualFold(mine.Parent.FullName, repo.FullName):
		rc.flag("you already have this fork, %s; GitHub returns it rather than making another", mine.FullName)
	default:
		rc.flag("you already have %s; GitHub would name the fork %s-1", mine.FullName, name)
	}
}
