package main

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/Sriram-PR/ghv/internal/audit"
	"github.com/Sriram-PR/ghv/internal/fetch"
	"github.com/Sriram-PR/ghv/internal/vault"
)

// GitHub keeps a user's public events for 30 days, at most 300.
const (
	eventDays = 30
	maxEvents = 300
)

// made is an event of a kind ghv-apply makes, as --verify reports it.
type made struct {
	at        time.Time
	kind      string
	repo, url string
	review    int64 // a review's ID
}

// madeBy reads an event as one of the writes ghv-apply makes. A pull
// request event has no html_url, so its URL is built from the number.
func madeBy(ev fetch.Event) (made, bool) {
	p := ev.Payload
	m := made{at: ev.CreatedAt, repo: ev.Repo.Name}
	switch {
	case ev.Type == "IssueCommentEvent" && p.Action == "created":
		m.kind, m.url = "comment", p.Comment.HTMLURL
	case ev.Type == "IssuesEvent" && p.Action == "opened":
		m.kind, m.url = "issue", p.Issue.HTMLURL
	case ev.Type == "PullRequestEvent" && p.Action == "opened":
		m.kind, m.url = "pull request", fmt.Sprintf("https://github.com/%s/pull/%d", ev.Repo.Name, p.Number)
	case ev.Type == "PullRequestReviewCommentEvent" && p.Action == "created":
		m.kind, m.url = "review comment", p.Comment.HTMLURL
	case ev.Type == "PullRequestReviewEvent" && p.Action == "created":
		m.kind, m.url, m.review = "review", p.Review.HTMLURL, p.Review.ID
	case ev.Type == "ForkEvent":
		m.kind, m.url = "fork", p.Forkee.HTMLURL
	default:
		return made{}, false
	}
	return m, true
}

// unlockLogin opens the vault for its login, authenticated by the
// passphrase: the header alone is the agent's to edit. The token is not
// needed and is cleared at once.
func unlockLogin(e env) (string, error) {
	data, err := vault.Load(vault.Path(e.home))
	if err != nil {
		return "", err
	}
	tty, err := e.openTTY()
	if err != nil {
		return "", err
	}
	defer tty.Close()
	pass, err := tty.ReadSecret("Passphrase: ")
	if err != nil {
		return "", err
	}
	s, err := vault.Open(data, pass)
	clear(pass)
	if err != nil {
		return "", err
	}
	s.Zero()
	return s.Login, nil
}

// verify compares the audit log with the user's public events, read
// anonymously. It fails when an event of a kind ghv-apply makes is not in
// the log.
func verify(ctx context.Context, e env, es []audit.Entry) error {
	login, err := unlockLogin(e)
	if err != nil {
		return err
	}
	evs, err := fetch.New(e.client).Events(ctx, login)
	if err != nil {
		return err
	}
	from := e.now().UTC().AddDate(0, 0, -eventDays)
	if len(evs) >= maxEvents {
		from = evs[len(evs)-1].CreatedAt
	}
	fmt.Fprintf(e.stdout, "Public events of %s on GitHub: %d, since %s.\n", login, len(evs), from.Format("2006-01-02 15:04 UTC"))

	logged := map[string]bool{}
	for _, en := range es {
		logged[en.URL] = true
	}
	var events []made
	reviews := map[int64]bool{} // reviews GitHub made for logged replies
	other := map[string]int{}
	seen := map[string]bool{}
	for _, ev := range evs {
		m, ok := madeBy(ev)
		if !ok {
			key := ev.Type
			if ev.Payload.Action != "" {
				key += "/" + ev.Payload.Action
			}
			other[key]++
			continue
		}
		seen[m.url] = true
		if m.kind == "review comment" && logged[m.url] {
			reviews[ev.Payload.Comment.PullRequestReviewID] = true
		}
		events = append(events, m)
	}
	var outside [][]string
	slices.SortFunc(events, func(a, b made) int { return a.at.Compare(b.at) })
	for _, m := range events {
		if logged[m.url] || m.kind == "review" && reviews[m.review] {
			continue
		}
		outside = append(outside, []string{"  " + m.at.UTC().Format("2006-01-02 15:04 UTC"), m.kind, m.repo, m.url})
	}

	var missing [][]string
	edits, older := 0, 0
	for _, en := range es {
		switch {
		case en.Kind == "pr.edit":
			edits++
		case en.Time.Before(from):
			older++
		case !seen[en.URL]:
			url := en.URL
			if en.Found {
				url += " (found later)"
			}
			missing = append(missing, []string{"  " + en.Time.UTC().Format("2006-01-02 15:04 UTC"), en.Draft, en.Kind, en.Repo, url})
		}
	}

	if len(outside) == 0 {
		fmt.Fprintln(e.stdout, "\nNothing made outside ghv-apply.")
	} else {
		fmt.Fprintf(e.stdout, "\nMade outside ghv-apply (%d):\n", len(outside))
		writeTable(e, outside)
	}
	if len(other) > 0 {
		keys := make([]string, 0, len(other))
		for k := range other {
			keys = append(keys, k)
		}
		slices.SortFunc(keys, func(a, b string) int {
			if other[a] != other[b] {
				return other[b] - other[a]
			}
			return strings.Compare(a, b)
		})
		parts := make([]string, len(keys))
		for i, k := range keys {
			parts[i] = fmt.Sprintf("%s %d", k, other[k])
		}
		fmt.Fprintf(e.stdout, "\nOther events, of kinds ghv-apply does not make: %s.\n", strings.Join(parts, ", "))
	}
	if len(missing) > 0 {
		fmt.Fprintf(e.stdout, "\nIn the audit log but not in GitHub's events, which can lag by hours and drop a deleted repository's (%d):\n", len(missing))
		writeTable(e, missing)
	}
	var skipped []string
	if edits > 0 {
		skipped = append(skipped, plural(edits, "pr.edit line", "pr.edit lines")+" (GitHub has no event for an edit)")
	}
	if older > 0 {
		skipped = append(skipped, plural(older, "line", "lines")+" older than the events GitHub keeps")
	}
	if len(skipped) > 0 {
		fmt.Fprintf(e.stdout, "\nNot checked: %s.\n", strings.Join(skipped, ", "))
	}
	if len(outside) > 0 {
		return fmt.Errorf("%s made outside ghv-apply", plural(len(outside), "event was", "events were"))
	}
	return nil
}
