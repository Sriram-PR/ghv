package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Sriram-PR/ghv/internal/draft"
	"github.com/Sriram-PR/ghv/internal/fetch"
	"github.com/Sriram-PR/ghv/internal/sanitize"
)

// maxDiffCells caps lineDiff's table; past it, every old line is shown
// removed and every new line added.
const maxDiffCells = 1 << 22

func (rv *reviewer) replyContext(ctx context.Context, rc *reviewContext, repo fetch.Repo, f draft.ReviewReplyFields) {
	c, err := rv.fc.ReviewComment(ctx, repo.FullName, f.CommentID)
	switch {
	case errors.Is(err, fetch.ErrNotFound):
		rc.block("%s has no review comment %d", repo.FullName, f.CommentID)
		return
	case err != nil:
		rc.block("cannot read review comment %d: %v", f.CommentID, err)
		return
	}
	if n := c.PullNumber(); n != f.Number {
		rc.block("review comment %d belongs to pull request #%d, not #%d", f.CommentID, n, f.Number)
	}
	if c.InReplyToID != 0 {
		rc.block("review comment %d is itself a reply; GitHub takes replies to a thread's first comment, %d", f.CommentID, c.InReplyToID)
	}
	where := c.Path
	switch {
	case c.Line != nil:
		where += fmt.Sprintf(" line %d", *c.Line)
	case c.OriginalLine != nil:
		where += fmt.Sprintf(" line %d (outdated)", *c.OriginalLine)
	}
	rc.line("Replying in pull request #%d to %s's comment on %s:", f.Number, c.User.Login, where)
	rc.quote(c.Body)
}

func (rv *reviewer) editContext(ctx context.Context, rc *reviewContext, repo fetch.Repo, f draft.PREditFields) {
	pr, err := rv.fc.PullRequest(ctx, repo.FullName, f.Number)
	switch {
	case errors.Is(err, fetch.ErrNotFound):
		rc.block("%s has no pull request #%d", repo.FullName, f.Number)
		return
	case err != nil:
		rc.block("cannot read pull request #%d: %v", f.Number, err)
		return
	}
	rc.line("Editing pull request #%d (%s), opened by %s", f.Number, pr.State, pr.User.Login)
	if !strings.EqualFold(pr.User.Login, rv.login) {
		rc.flag("pull request #%d was opened by %s, not you", f.Number, pr.User.Login)
	}
	if f.Title != nil {
		rc.diff("Title", pr.Title, *f.Title)
	}
	if f.Body != nil {
		rc.diff("Body", strings.ReplaceAll(pr.Body, "\r\n", "\n"), *f.Body)
	}
}

func (rc *reviewContext) diff(what, before, after string) {
	if before == after {
		rc.line("%s: unchanged", what)
		return
	}
	rc.line("%s:", what)
	for _, l := range lineDiff(before, after) {
		rc.Lines = append(rc.Lines, "  "+l[:2]+sanitize.Terminal(l[2:]))
	}
}

// lineDiff marks each line "  " (kept), "- " (removed), or "+ " (added),
// keeping a longest common subsequence of lines.
func lineDiff(before, after string) []string {
	a, b := splitLines(before), splitLines(after)
	var out []string
	if len(a)*len(b) > maxDiffCells {
		for _, l := range a {
			out = append(out, "- "+l)
		}
		for _, l := range b {
			out = append(out, "+ "+l)
		}
		return out
	}
	// lcs[i][j] is the longest common subsequence of a[i:] and b[j:].
	lcs := make([][]int, len(a)+1)
	for i := range lcs {
		lcs[i] = make([]int, len(b)+1)
	}
	for i := len(a) - 1; i >= 0; i-- {
		for j := len(b) - 1; j >= 0; j-- {
			if a[i] == b[j] {
				lcs[i][j] = lcs[i+1][j+1] + 1
			} else {
				lcs[i][j] = max(lcs[i+1][j], lcs[i][j+1])
			}
		}
	}
	i, j := 0, 0
	for i < len(a) && j < len(b) {
		switch {
		case a[i] == b[j]:
			out = append(out, "  "+a[i])
			i, j = i+1, j+1
		case lcs[i+1][j] >= lcs[i][j+1]:
			out = append(out, "- "+a[i])
			i++
		default:
			out = append(out, "+ "+b[j])
			j++
		}
	}
	for ; i < len(a); i++ {
		out = append(out, "- "+a[i])
	}
	for ; j < len(b); j++ {
		out = append(out, "+ "+b[j])
	}
	return out
}

func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(s, "\n")
}
