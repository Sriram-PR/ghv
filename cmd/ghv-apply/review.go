package main

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strings"

	"github.com/Sriram-PR/ghv/internal/draft"
	"github.com/Sriram-PR/ghv/internal/fetch"
	"github.com/Sriram-PR/ghv/internal/sanitize"
	"github.com/Sriram-PR/ghv/internal/vault"
	"github.com/Sriram-PR/ghv/internal/write"
)

// queueRoot is where ghv writes drafts, under the passwd home (§6.3).
func queueRoot(home string) string { return filepath.Join(home, ".local", "share", "ghv") }

// item is one pending draft, or the error that kept it from being read.
type item struct {
	id     string
	loaded draft.Loaded
	err    error
}

// runReview shows each pending draft, oldest first, with its live context
// and verdict, and takes the reviewer's answer from the terminal.
func runReview(ctx context.Context, e env) error {
	tty, err := e.openTTY()
	if err != nil {
		return err
	}
	defer tty.Close()
	data, err := vault.Load(vault.Path(e.home))
	if err != nil {
		return err
	}
	q := draft.Queue{Root: queueRoot(e.home)}
	lock, err := q.Lock()
	if err != nil {
		return err
	}
	defer lock.Release()
	items, unknown, err := pending(q)
	if err != nil {
		return err
	}
	if len(items) == 0 {
		tty.Printf("No drafts wait for review.\n")
	} else {
		tty.Printf("%s for review.\n", plural(len(items), "draft waits", "drafts wait"))
		s, err := unlock(ctx, e, tty, data)
		if err != nil {
			return err
		}
		defer s.Zero()
		rv := &reviewer{fc: fetch.New(e.client), login: s.Login, now: e.now()}
		rejected, left := 0, 0
	drafts:
		for i, it := range items {
			tty.Printf("\n== Draft %d of %d ==\n", i+1, len(items))
			act, err := reviewOne(ctx, e, tty, q, rv, it)
			if err != nil {
				return err
			}
			switch act {
			case actReject:
				rejected++
			case actSkip:
				left++
			case actQuit:
				left += len(items) - i
				break drafts
			}
		}
		tty.Printf("\nRejected %d, left %d in the queue.\n", rejected, left)
	}
	if unknown > 0 {
		tty.Printf("%s for its result to be checked, which this ghv-apply cannot do yet.\n",
			plural(unknown, "draft in unknown/ waits", "drafts in unknown/ wait"))
	}
	return nil
}

// unlock opens the vault and checks that GitHub still says the write
// token is the pinned login's, before any draft is shown. The login is in
// the vault's authenticated header, so it is the one init recorded.
func unlock(ctx context.Context, e env, tty terminal, data []byte) (vault.Secret, error) {
	pass, err := tty.ReadSecret("Passphrase: ")
	if err != nil {
		return vault.Secret{}, err
	}
	s, err := vault.Open(data, pass)
	clear(pass)
	if err != nil {
		return vault.Secret{}, err
	}
	id, err := write.Whoami(ctx, e.client, s.Token)
	if err == nil && !strings.EqualFold(id.Login, s.Login) {
		err = fmt.Errorf("the write token belongs to %s, but the vault is pinned to %s; ghv-apply sends nothing", id.Login, s.Login)
	}
	if err != nil {
		s.Zero()
		return vault.Secret{}, fmt.Errorf("checking the write token: %w", err)
	}
	warnExpiry(e, s)
	return s, nil
}

// pending reads every pending draft once; unreadable ones sort last.
func pending(q draft.Queue) ([]item, int, error) {
	entries, err := q.List()
	if err != nil {
		return nil, 0, err
	}
	var items []item
	unknown := 0
	for _, en := range entries {
		switch en.State {
		case draft.Unknown:
			unknown++
		case draft.Pending:
			l, err := draft.ReadFile(filepath.Join(q.Dir(draft.Pending), en.ID+".json"))
			items = append(items, item{id: en.ID, loaded: l, err: err})
		}
	}
	slices.SortStableFunc(items, func(a, b item) int {
		if (a.err != nil) != (b.err != nil) {
			if a.err != nil {
				return 1
			}
			return -1
		}
		return a.loaded.Draft.CreatedAt.Compare(b.loaded.Draft.CreatedAt)
	})
	return items, unknown, nil
}

func reviewOne(ctx context.Context, e env, tty terminal, q draft.Queue, rv *reviewer, it item) (action, error) {
	var diff func(context.Context) (string, error)
	if it.err != nil {
		tty.Printf("Draft %s cannot be read: %s\n", it.id, sanitize.Line(it.err.Error()))
		tty.Printf("ghv-apply will not send it; reject it to clear it from the queue.\n")
	} else {
		rep := sanitize.CheckDraft(it.loaded.Draft)
		rc := rv.context(ctx, it.loaded.Draft)
		lines := []string{"Context from GitHub:"}
		for _, l := range rc.Lines {
			lines = append(lines, "  "+l)
		}
		lines = append(lines, "")
		lines = append(lines, renderDraft(it.loaded, rep)...)
		if err := page(tty, lines); err != nil {
			return 0, err
		}
		for _, l := range renderVerdict(judge(rep, rc)) {
			tty.Printf("%s\n", l)
		}
		diff = rc.Diff
	}
	as := offered(diff != nil)
	for {
		ans, err := tty.ReadLine(prompt(as))
		if err != nil {
			return 0, err
		}
		act, ok := parseAction(ans, as)
		if !ok {
			tty.Printf("Answer with one of the letters in brackets.\n")
			continue
		}
		switch act {
		case actDiff:
			if err := showDiff(ctx, tty, diff); err != nil {
				return 0, err
			}
			continue
		case actReject:
			reason, err := tty.ReadLine("Reason, for the agent to read (Enter for none): ")
			if err != nil {
				return 0, err
			}
			if reason = strings.TrimSpace(reason); reason == "" {
				reason = "rejected in review"
			}
			if err := q.Move(it.id, draft.Pending, draft.Rejected, it.loaded.SHA256, draft.Outcome{At: e.now().UTC(), Reason: reason}); err != nil {
				tty.Printf("%s\n", sanitize.Line(fmt.Sprintf("Cannot reject %s: %v", it.id, err)))
				return actSkip, nil
			}
			tty.Printf("Rejected %s.\n", it.id)
		case actSkip:
			tty.Printf("Left %s in the queue.\n", it.id)
		}
		return act, nil
	}
}

// showDiff pages a pull request's full diff. A failed fetch is shown, not
// fatal: the reviewer can still reject or skip.
func showDiff(ctx context.Context, tty terminal, diff func(context.Context) (string, error)) error {
	d, err := diff(ctx)
	if err != nil {
		tty.Printf("%s\n", sanitize.Line(fmt.Sprintf("cannot fetch the diff: %v", err)))
		return nil
	}
	lines := strings.Split(sanitize.Terminal(strings.TrimSuffix(d, "\n")), "\n")
	lines = append(lines, "-- end of the diff: "+plural(len(lines), "line", "lines")+" --")
	return page(tty, lines)
}
