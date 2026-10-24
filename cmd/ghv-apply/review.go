package main

import (
	"context"
	"fmt"
	"path/filepath"
	"reflect"
	"slices"
	"strings"

	"github.com/Sriram-PR/ghv/internal/draft"
	"github.com/Sriram-PR/ghv/internal/fetch"
	"github.com/Sriram-PR/ghv/internal/sanitize"
	"github.com/Sriram-PR/ghv/internal/vault"
	"github.com/Sriram-PR/ghv/internal/write"
)

// item is one pending draft, or the error that kept it from being read.
type item struct {
	id     string
	loaded draft.Loaded
	err    error
}

// tally is what became of one draft in a session.
type tally int

const (
	left tally = iota
	rejected
	sent
	unsure // sent, with no answer from GitHub to say what happened
)

// session is one review: the terminal, the queue, the context gatherer,
// and the unlocked write token.
type session struct {
	e      env
	tty    terminal
	q      draft.Queue
	rv     *reviewer
	tok    []byte
	watch  *limitWatch // nil once the reviewer has been asked about the write token
	counts [4]int
}

// runReview shows each pending draft, oldest first, with its live context
// and verdict, and takes the reviewer's answer from the terminal.
func runReview(ctx context.Context, e env) error {
	q, err := draft.Locate(e.sharedRoot, e.home)
	if err != nil {
		return err
	}
	tty, err := e.openTTY()
	if err != nil {
		return err
	}
	defer tty.Close()
	data, err := vault.Load(vault.Path(e.home))
	if err != nil {
		return err
	}
	lock, err := q.Lock()
	if err != nil {
		return err
	}
	defer lock.Release()
	items, unknowns, err := pending(q)
	if err != nil {
		return err
	}
	if len(items) == 0 && len(unknowns) == 0 {
		tty.Printf("No drafts wait for review.\n")
		return nil
	}
	if len(unknowns) > 0 {
		tty.Printf("%s.\n", waitsToBeChecked(len(unknowns)))
	}
	if len(items) > 0 {
		tty.Printf("%s for review.\n", plural(len(items), "draft waits", "drafts wait"))
	}
	s, err := unlock(ctx, e, tty, data)
	if err != nil {
		return err
	}
	defer s.Zero()
	watch := &limitWatch{rt: e.client.Transport}
	ss := &session{e: e, tty: idleTerm{tty, e.afterFunc, func() {
		s.Zero()
		tty.Printf("\nNo answer for 5 minutes: the session ended, and the write token was cleared from memory.\n")
		e.exit(1)
	}, &idleGate{}}, q: q, tok: s.Token, watch: watch,
		rv: &reviewer{fc: fetch.New(withTransport(e.client, watch)), login: s.Login, now: e.now()}}
	still := 0
	if len(unknowns) > 0 {
		kept, requeued, err := ss.recoverUnknown(ctx, unknowns)
		if err != nil {
			return err
		}
		still = kept
		if requeued > 0 {
			if items, _, err = pending(q); err != nil {
				return err
			}
			tty.Printf("%s for review.\n", plural(len(items), "draft waits", "drafts wait"))
		}
	}
	if len(items) > 0 {
		for i, it := range items {
			tty.Printf("\n== Draft %d of %d ==\n", i+1, len(items))
			t, stop, err := ss.reviewOne(ctx, it)
			if err != nil {
				return err
			}
			ss.counts[t]++
			if stop {
				ss.counts[left] += len(items) - i - 1
				break
			}
		}
		tty.Printf("\nSent %d, rejected %d, left %d in the queue.\n", ss.counts[sent], ss.counts[rejected], ss.counts[left])
	}
	if still += ss.counts[unsure]; still == 1 {
		tty.Printf("%s; the next ghv-apply session checks it again.\n", waitsToBeChecked(still))
	} else if still > 1 {
		tty.Printf("%s; the next ghv-apply session checks them again.\n", waitsToBeChecked(still))
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

// pending reads every pending draft once; unreadable ones sort last. It
// also lists the drafts in unknown/.
func pending(q draft.Queue) ([]item, []draft.Entry, error) {
	entries, err := q.List()
	if err != nil {
		return nil, nil, err
	}
	var items []item
	var unknowns []draft.Entry
	for _, en := range entries {
		switch en.State {
		case draft.Unknown:
			unknowns = append(unknowns, en)
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
	return items, unknowns, nil
}

// shown is what the reviewer is looking at: the draft as it would be
// sent, which an edit can change, and what ghv-apply learned about it.
type shown struct {
	d       draft.Draft
	edited  bool
	rc      reviewContext
	req     *write.Request
	canSend bool
}

// reviewOne shows a draft and acts on the answer. stop ends the session.
func (ss *session) reviewOne(ctx context.Context, it item) (t tally, stop bool, err error) {
	tty := ss.tty
	var cur *shown
	if it.err != nil {
		tty.Printf("Draft %s cannot be read: %s\n", it.id, sanitize.Line(it.err.Error()))
		tty.Printf("ghv-apply will not send it; reject it to clear it from the queue.\n")
	} else {
		rc, err := ss.gather(ctx, it.loaded.Draft)
		if err != nil {
			return left, false, err
		}
		cur = &shown{d: it.loaded.Draft, rc: rc}
		if err := ss.present(it, cur); err != nil {
			return left, false, err
		}
	}
	for {
		as := offered(cur != nil && cur.canSend, cur != nil && editable(cur.d), cur != nil && cur.rc.Diff != nil)
		ans, err := tty.ReadLine(prompt(as))
		if err != nil {
			return left, false, err
		}
		act, ok := parseAction(ans, as)
		if !ok {
			tty.Printf("Answer with one of the letters in brackets.\n")
			continue
		}
		switch act {
		case actApprove:
			t, stop := ss.send(ctx, it, cur.d, cur.rc.FullName, *cur.req, cur.edited)
			return t, stop, nil
		case actEdit:
			if err := ss.edit(ctx, it, cur); err != nil {
				return left, false, err
			}
			continue
		case actDiff:
			if err := showDiff(ctx, tty, cur.rc.Diff); err != nil {
				return left, false, err
			}
			continue
		case actReject:
			reason, err := tty.ReadLine("Reason, for the agent to read (Enter for none): ")
			if err != nil {
				return left, false, err
			}
			if reason = strings.TrimSpace(reason); reason == "" {
				reason = "rejected in review"
			}
			if err := ss.q.Move(it.id, draft.Pending, draft.Rejected, it.loaded.SHA256, draft.Outcome{At: ss.e.now().UTC(), Reason: reason}); err != nil {
				tty.Printf("%s\n", sanitize.Line(fmt.Sprintf("Cannot reject %s: %v", it.id, err)))
				return left, false, nil
			}
			tty.Printf("Rejected %s.\n", it.id)
			return rejected, false, nil
		case actSkip:
			tty.Printf("Left %s in the queue.\n", it.id)
			return left, false, nil
		}
		return left, true, nil
	}
}

// present builds cur's request and verdict and pages the preview.
func (ss *session) present(it item, cur *shown) error {
	rep := sanitize.CheckDraft(cur.d)
	cur.req = nil
	var buildErr error
	if cur.rc.FullName != "" {
		if r, err := write.Build(cur.d, cur.rc.FullName); err != nil {
			buildErr = err
		} else {
			cur.req = &r
		}
	}
	lines := []string{"Context from GitHub:"}
	for _, l := range cur.rc.Lines {
		lines = append(lines, "  "+l)
	}
	lines = append(lines, "")
	l := draft.Loaded{Draft: cur.d, Raw: it.loaded.Raw, SHA256: it.loaded.SHA256}
	lines = append(lines, renderDraft(l, rep, cur.req)...)
	if err := page(ss.tty, lines); err != nil {
		return err
	}
	v := judge(rep, cur.rc)
	if buildErr != nil {
		v.Blocks = append(v.Blocks, sanitize.Line("cannot build the request: "+buildErr.Error()))
	}
	for _, l := range renderVerdict(v) {
		ss.tty.Printf("%s\n", l)
	}
	cur.canSend = cur.req != nil && len(v.Blocks) == 0
	return nil
}

// edit lets the reviewer change the draft's text in their editor. The
// result is read once, checked like a new draft, and shown again in full
// before it can be approved. The queue keeps the agent's file.
func (ss *session) edit(ctx context.Context, it item, cur *shown) error {
	text, err := editFile(ss.e.edit, it.id, editText(cur.d))
	if err != nil {
		ss.say("%v", err)
		return nil
	}
	d, err := applyEdit(cur.d, text)
	if err != nil {
		ss.say("The edit was not used: %v", err)
		return nil
	}
	if reflect.DeepEqual(d, cur.d) {
		ss.say("No changes.")
		return nil
	}
	cur.d, cur.edited = d, true
	if d.Kind() == draft.KindPREdit {
		if cur.rc, err = ss.gather(ctx, d); err != nil {
			return err
		}
	}
	ss.say("Edited. ghv-apply sends this version; the queue keeps the agent's file.")
	return ss.present(it, cur)
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
