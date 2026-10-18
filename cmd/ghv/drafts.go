package main

import (
	"context"
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Sriram-PR/ghv/internal/draft"
)

func init() {
	register(&command{
		path: "drafts",
		help: "Show every draft: pending, sent with the result unknown, applied, or rejected",
		run:  runDrafts,
	})
}

// draftStatus is one item of ghv drafts --json. An unreadable draft has
// only id, state, outcome, and error.
type draftStatus struct {
	ID        string         `json:"id"`
	State     draft.State    `json:"state"`
	Kind      draft.Kind     `json:"kind,omitempty"`
	Repo      string         `json:"repo,omitempty"`
	Number    int            `json:"number,omitempty"`
	Title     string         `json:"title,omitempty"`
	CreatedAt *time.Time     `json:"created_at,omitempty"`
	Outcome   *draft.Outcome `json:"outcome"`
	Error     string         `json:"error,omitempty"`
}

func runDrafts(ctx context.Context, a *app) error {
	q, err := a.draftQueue()
	if err != nil {
		return err
	}
	entries, err := q.List()
	if err != nil {
		return err
	}
	list := []draftStatus{}
	for _, e := range entries {
		s := draftStatus{ID: e.ID, State: e.State, Outcome: e.Outcome}
		l, err := draft.ReadFile(filepath.Join(q.Dir(e.State), e.ID+".json"))
		if err != nil {
			s.Error = err.Error()
		} else {
			d := l.Draft
			s.Kind, s.Repo, s.CreatedAt = d.Kind(), d.Repo, &d.CreatedAt
			s.Number, s.Title = target(d.Fields)
		}
		list = append(list, s)
	}
	slices.SortStableFunc(list, createdOrder)
	if a.json() {
		return writeJSON(a.stdout, list)
	}
	if len(list) == 0 {
		fmt.Fprintln(a.stdout, "no drafts")
		return nil
	}
	rows := make([][]string, len(list))
	for i, s := range list {
		what := s.Title
		if s.Number > 0 {
			what = strings.TrimSpace("#" + strconv.Itoa(s.Number) + " " + s.Title)
		}
		rows[i] = []string{s.ID, string(s.State), string(s.Kind), s.Repo, what, s.detail()}
	}
	writeTable(a.stdout, rows)
	return nil
}

// createdOrder sorts drafts oldest first, unreadable ones last.
func createdOrder(x, y draftStatus) int {
	switch {
	case x.CreatedAt == nil && y.CreatedAt == nil:
		return 0
	case x.CreatedAt == nil:
		return 1
	case y.CreatedAt == nil:
		return -1
	}
	return x.CreatedAt.Compare(*y.CreatedAt)
}

// target is the number and title a draft acts on, where it has them.
func target(f draft.Fields) (int, string) {
	switch f := f.(type) {
	case draft.PRCreateFields:
		return 0, f.Title
	case draft.PREditFields:
		if f.Title != nil {
			return f.Number, *f.Title
		}
		return f.Number, ""
	case draft.CommentFields:
		return f.Number, ""
	case draft.ReviewReplyFields:
		return f.Number, ""
	case draft.IssueCreateFields:
		return 0, f.Title
	}
	return 0, ""
}

func (s draftStatus) detail() string {
	switch {
	case s.Error != "":
		return "unreadable: " + s.Error
	case s.State == draft.Pending && s.Outcome != nil && s.Outcome.Note != "":
		return "waiting for ghv-apply (" + s.Outcome.Note + ")"
	case s.State == draft.Pending:
		return "waiting for ghv-apply"
	case s.State == draft.Unknown:
		return "sent; ghv-apply checks the result next session"
	case s.Outcome == nil:
		return ""
	case s.State == draft.Applied:
		return s.Outcome.URL
	case s.Outcome.Reason != "":
		return "rejected: " + s.Outcome.Reason
	}
	return ""
}
