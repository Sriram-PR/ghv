// Package draft is the queued-write format. ghv writes drafts; ghv-apply
// treats every draft as untrusted input.
package draft

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strconv"
	"time"
)

const Schema = 1

var ErrInvalid = errors.New("invalid draft")

type Kind string

const (
	KindFork        Kind = "fork"
	KindPRCreate    Kind = "pr.create"
	KindPREdit      Kind = "pr.edit"
	KindComment     Kind = "comment"
	KindReviewReply Kind = "review.reply"
	KindIssueCreate Kind = "issue.create"
)

// Fields is one of the six typed action kinds. A draft never names a method or path.
type Fields interface {
	Kind() Kind
	fields()
}

type ForkFields struct{}

type PRCreateFields struct {
	Base                string `json:"base"`
	Head                string `json:"head"`
	HeadSHA             string `json:"head_sha"`
	Title               string `json:"title"`
	Body                string `json:"body"`
	Draft               bool   `json:"draft"`
	MaintainerCanModify bool   `json:"maintainer_can_modify"`
}

type PREditFields struct {
	Number int     `json:"number"`
	Title  *string `json:"title,omitempty"`
	Body   *string `json:"body,omitempty"`
}

type CommentFields struct {
	Number int    `json:"number"`
	Body   string `json:"body"`
}

type ReviewReplyFields struct {
	Number    int    `json:"number"`
	CommentID int64  `json:"comment_id"`
	Body      string `json:"body"`
}

type IssueCreateFields struct {
	Title string `json:"title"`
	Body  string `json:"body"`
}

func (ForkFields) Kind() Kind        { return KindFork }
func (PRCreateFields) Kind() Kind    { return KindPRCreate }
func (PREditFields) Kind() Kind      { return KindPREdit }
func (CommentFields) Kind() Kind     { return KindComment }
func (ReviewReplyFields) Kind() Kind { return KindReviewReply }
func (IssueCreateFields) Kind() Kind { return KindIssueCreate }

func (ForkFields) fields()        {}
func (PRCreateFields) fields()    {}
func (PREditFields) fields()      {}
func (CommentFields) fields()     {}
func (ReviewReplyFields) fields() {}
func (IssueCreateFields) fields() {}

type Draft struct {
	ID        string // from the file name, never from the file
	Repo      string
	CreatedAt time.Time
	Fields    Fields
}

func (d Draft) Kind() Kind {
	if d.Fields == nil {
		return ""
	}
	return d.Fields.Kind()
}

type keySet struct{ required, optional []string }

var topLevelKeys = keySet{
	required: []string{"schema", "kind", "repo", "created_at", "fields"},
	optional: []string{"id"},
}

var fieldKeys = map[Kind]keySet{
	KindFork:        {},
	KindPRCreate:    {required: []string{"base", "head", "head_sha", "title", "body", "draft", "maintainer_can_modify"}},
	KindPREdit:      {required: []string{"number"}, optional: []string{"title", "body"}},
	KindComment:     {required: []string{"number", "body"}},
	KindReviewReply: {required: []string{"number", "comment_id", "body"}},
	KindIssueCreate: {required: []string{"title", "body"}},
}

func invalid(format string, args ...any) error {
	return fmt.Errorf("%w: %s", ErrInvalid, fmt.Sprintf(format, args...))
}

// Decode parses and validates a draft. Keys are matched exactly; Go's
// case-insensitive field matching never sees a key that is not listed.
func Decode(data []byte) (Draft, error) {
	if err := checkStrictJSON(data); err != nil {
		return Draft{}, invalid("%v", err)
	}
	var top map[string]json.RawMessage
	if err := json.Unmarshal(data, &top); err != nil {
		return Draft{}, invalid("%v", err)
	}
	if err := checkKeys("draft", top, topLevelKeys); err != nil {
		return Draft{}, err
	}
	if string(top["schema"]) != strconv.Itoa(Schema) {
		return Draft{}, invalid("schema must be %d", Schema)
	}
	var (
		d       Draft
		kind    Kind
		created string
	)
	if err := strictUnmarshal(top["kind"], &kind); err != nil {
		return Draft{}, err
	}
	keys, ok := fieldKeys[kind]
	if !ok {
		return Draft{}, invalid("unknown kind %q", kind)
	}
	if err := strictUnmarshal(top["repo"], &d.Repo); err != nil {
		return Draft{}, err
	}
	if err := strictUnmarshal(top["created_at"], &created); err != nil {
		return Draft{}, err
	}
	t, err := time.Parse(time.RFC3339, created)
	if err != nil {
		return Draft{}, invalid("created_at: %v", err)
	}
	d.CreatedAt = t
	var fieldsObj map[string]json.RawMessage
	if err := json.Unmarshal(top["fields"], &fieldsObj); err != nil {
		return Draft{}, invalid("fields must be an object")
	}
	if err := checkKeys("fields", fieldsObj, keys); err != nil {
		return Draft{}, err
	}
	if d.Fields, err = decodeFields(kind, top["fields"]); err != nil {
		return Draft{}, err
	}
	if err := Validate(d); err != nil {
		return Draft{}, err
	}
	return d, nil
}

func checkKeys(where string, obj map[string]json.RawMessage, set keySet) error {
	for k := range obj {
		if !slices.Contains(set.required, k) && !slices.Contains(set.optional, k) {
			return invalid("unknown key %q in %s", k, where)
		}
	}
	for _, k := range set.required {
		if _, ok := obj[k]; !ok {
			return invalid("missing key %q in %s", k, where)
		}
	}
	return nil
}

func decodeFields(kind Kind, raw json.RawMessage) (Fields, error) {
	switch kind {
	case KindFork:
		return decodeAs[ForkFields](raw)
	case KindPRCreate:
		return decodeAs[PRCreateFields](raw)
	case KindPREdit:
		return decodeAs[PREditFields](raw)
	case KindComment:
		return decodeAs[CommentFields](raw)
	case KindReviewReply:
		return decodeAs[ReviewReplyFields](raw)
	case KindIssueCreate:
		return decodeAs[IssueCreateFields](raw)
	}
	return nil, invalid("unknown kind %q", kind)
}

func decodeAs[T Fields](raw json.RawMessage) (Fields, error) {
	var f T
	if err := strictUnmarshal(raw, &f); err != nil {
		return nil, err
	}
	return f, nil
}

func strictUnmarshal(raw json.RawMessage, v any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return invalid("%v", err)
	}
	return nil
}
