package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Sriram-PR/ghv/internal/draft"
	"github.com/Sriram-PR/ghv/internal/fetch"
)

// skew is how far before the send time recovery looks, since this
// machine's clock and GitHub's can differ.
const skew = 5 * time.Minute

// found is what recovery learned about a draft whose send had no clear
// answer: its URL when GitHub shows it was made, else what was checked.
type found struct {
	URL, FullName string
	Checked       string
}

// sameText compares a body GitHub stored, which can have CRLF line endings
// and lose surrounding whitespace, with the draft's.
func sameText(stored, drafted string) bool {
	norm := func(s string) string { return strings.TrimSpace(strings.ReplaceAll(s, "\r\n", "\n")) }
	return norm(stored) == norm(drafted)
}

// findSent looks on GitHub for what d made when it was sent at sentAt.
// Authors must be the pinned login. When the reviewer edited the draft,
// the file holds the agent's text, not what was sent, so the text is not
// compared.
func (rv *reviewer) findSent(ctx context.Context, d draft.Draft, sentAt time.Time, edited bool) (found, error) {
	after := sentAt.Add(-skew)
	mine := func(m fetch.Made) bool {
		return strings.EqualFold(m.User.Login, rv.login) && !m.CreatedAt.Before(after)
	}
	when := " since " + after.UTC().Format("2006-01-02 15:04 UTC")
	if !edited {
		when += ", matching the draft's text"
	}
	switch f := d.Fields.(type) {
	case draft.CommentFields:
		ms, err := rv.fc.IssueComments(ctx, d.Repo, f.Number, after)
		if err != nil {
			return found{}, err
		}
		for _, m := range ms {
			if mine(m) && (edited || sameText(m.Body, f.Body)) {
				return found{URL: m.HTMLURL}, nil
			}
		}
		return found{Checked: fmt.Sprintf("your comments on %s#%d%s", d.Repo, f.Number, when)}, nil
	case draft.ReviewReplyFields:
		ms, err := rv.fc.ReviewComments(ctx, d.Repo, f.Number, after)
		if err != nil {
			return found{}, err
		}
		for _, m := range ms {
			if mine(m) && m.InReplyToID == f.CommentID && (edited || sameText(m.Body, f.Body)) {
				return found{URL: m.HTMLURL}, nil
			}
		}
		return found{Checked: fmt.Sprintf("your replies to review comment %d on %s#%d%s", f.CommentID, d.Repo, f.Number, when)}, nil
	case draft.IssueCreateFields:
		ms, err := rv.fc.IssuesBy(ctx, d.Repo, rv.login, after)
		if err != nil {
			return found{}, err
		}
		for _, m := range ms {
			if mine(m) && !m.IsPullRequest() && (edited || m.Title == f.Title && sameText(m.Body, f.Body)) {
				return found{URL: m.HTMLURL}, nil
			}
		}
		return found{Checked: fmt.Sprintf("the issues you opened in %s%s", d.Repo, when)}, nil
	case draft.PRCreateFields:
		ms, err := rv.fc.PullsByHead(ctx, d.Repo, f.Head)
		if err != nil {
			return found{}, err
		}
		for _, m := range ms {
			if mine(m) && m.Head.SHA == f.HeadSHA {
				return found{URL: m.HTMLURL}, nil
			}
		}
		return found{Checked: fmt.Sprintf("your pull requests from %s at %s", f.Head, short(f.HeadSHA))}, nil
	case draft.PREditFields:
		url := fmt.Sprintf("https://github.com/%s/pull/%d", d.Repo, f.Number)
		if edited {
			return found{Checked: "nothing: an edited pr.edit cannot be matched by its text; see " + url}, nil
		}
		pr, err := rv.fc.PullRequest(ctx, d.Repo, f.Number)
		if err != nil {
			return found{}, err
		}
		if (f.Title == nil || pr.Title == *f.Title) && (f.Body == nil || sameText(pr.Body, *f.Body)) {
			return found{URL: url}, nil
		}
		return found{Checked: fmt.Sprintf("whether %s#%d now has the draft's title and body", d.Repo, f.Number)}, nil
	case draft.ForkFields:
		ms, err := rv.fc.Forks(ctx, d.Repo)
		if err != nil {
			return found{}, err
		}
		for _, m := range ms {
			if strings.EqualFold(m.Owner.Login, rv.login) {
				return found{URL: m.HTMLURL, FullName: m.FullName}, nil
			}
		}
		return found{Checked: fmt.Sprintf("the newest 100 forks of %s", d.Repo)}, nil
	}
	return found{}, fmt.Errorf("draft kind %s has no recovery check", d.Kind())
}
