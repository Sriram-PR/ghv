package draft

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

var created = time.Date(2026, 10, 16, 5, 5, 12, 0, time.UTC)

func ptr(s string) *string { return &s }

var golden = map[string]Draft{
	"fork": {Repo: "reticlehq/reticle", CreatedAt: created, Fields: ForkFields{}},
	"pr.create": {Repo: "reticlehq/reticle", CreatedAt: created, Fields: PRCreateFields{
		Base:                "main",
		Head:                "Sriram-PR:fix/sequence-late-anchor",
		HeadSHA:             "0123456789abcdef0123456789abcdef01234567",
		Title:               "fix(server): keep late anchors in sequence",
		Body:                "Fixes #12.\n\n<details>raw markdown</details>\n",
		Draft:               false,
		MaintainerCanModify: true,
	}},
	"pr.edit": {Repo: "reticlehq/reticle", CreatedAt: created, Fields: PREditFields{
		Number: 1638, Title: ptr("fix(server): clearer title"),
	}},
	"comment": {Repo: "reticlehq/reticle", CreatedAt: created, Fields: CommentFields{
		Number: 1638, Body: "Rebased on main; CI is green now.\n",
	}},
	"review.reply": {Repo: "reticlehq/reticle", CreatedAt: created, Fields: ReviewReplyFields{
		Number: 1638, CommentID: 2210987654, Body: "Done in 0123456.\n",
	}},
	"issue.create": {Repo: "reticlehq/reticle", CreatedAt: created, Fields: IssueCreateFields{
		Title: "pr checks: --watch ignores --interval",
		Body:  "Steps:\n\n1. run `ghv pr checks 1 --watch`\n",
	}},
}

func equalDraft(a, b Draft) bool {
	return a.ID == b.ID && a.Repo == b.Repo && a.CreatedAt.Equal(b.CreatedAt) &&
		reflect.DeepEqual(a.Fields, b.Fields)
}

func readGolden(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("testdata", "golden", name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestDecodeGolden(t *testing.T) {
	for name, want := range golden {
		t.Run(name, func(t *testing.T) {
			got, err := Decode(readGolden(t, name))
			if err != nil {
				t.Fatal(err)
			}
			if !equalDraft(got, want) {
				t.Fatalf("Decode = %+v, want %+v", got, want)
			}
			if string(got.Kind()) != name {
				t.Fatalf("Kind = %q, want %q", got.Kind(), name)
			}
		})
	}
}

func doc(kind, fields string) string {
	return `{"schema":1,"kind":"` + kind + `","repo":"o/r","created_at":"2026-10-16T05:05:12Z","fields":` + fields + `}`
}

const prFields = `"base":"main","head":"me:fix/x","head_sha":"0123456789abcdef0123456789abcdef01234567","title":"t","body":"b","draft":false,"maintainer_can_modify":true`

func pr(old, replacement string) string {
	return doc("pr.create", "{"+strings.Replace(prFields, old, replacement, 1)+"}")
}

func TestDecodeAcceptsBaseCases(t *testing.T) {
	for _, in := range []string{
		doc("pr.create", "{"+prFields+"}"),
		doc("comment", `{"number":5,"body":"hi"}`),
		doc("pr.edit", `{"number":5,"body":"new"}`),
		doc("fork", `{}`),
		doc("pr.create", "{"+strings.Replace(prFields, "0123456789abcdef0123456789abcdef01234567", strings.Repeat("ab", 32), 1)+"}"),
	} {
		if _, err := Decode([]byte(in)); err != nil {
			t.Errorf("Decode(%s): %v", in, err)
		}
	}
}

func TestDecodeIgnoresIDInside(t *testing.T) {
	in := `{"id":"d-ffffffffffff","schema":1,"kind":"comment","repo":"o/r","created_at":"2026-10-16T05:05:12Z","fields":{"number":5,"body":"hi"}}`
	d, err := Decode([]byte(in))
	if err != nil {
		t.Fatal(err)
	}
	if d.ID != "" {
		t.Fatalf("ID = %q, want empty (IDs come from file names)", d.ID)
	}
}

func TestDecodeRejects(t *testing.T) {
	tests := []struct{ name, in string }{
		{"unknown top-level field", `{"schema":1,"kind":"comment","repo":"o/r","created_at":"2026-10-16T05:05:12Z","fields":{"number":5,"body":"hi"},"method":"DELETE"}`},
		{"unknown field", doc("comment", `{"number":5,"body":"hi","path":"/x"}`)},
		{"field in other case", doc("comment", `{"Number":5,"body":"hi"}`)},
		{"top-level key in other case", `{"schema":1,"Kind":"comment","repo":"o/r","created_at":"2026-10-16T05:05:12Z","fields":{"number":5,"body":"hi"}}`},
		{"duplicate top-level key", `{"schema":1,"kind":"comment","kind":"issue.create","repo":"o/r","created_at":"2026-10-16T05:05:12Z","fields":{"number":5,"body":"hi"}}`},
		{"duplicate field", doc("comment", `{"number":5,"body":"hi","body":"other"}`)},
		{"schema 2", `{"schema":2,"kind":"comment","repo":"o/r","created_at":"2026-10-16T05:05:12Z","fields":{"number":5,"body":"hi"}}`},
		{"schema as string", `{"schema":"1","kind":"comment","repo":"o/r","created_at":"2026-10-16T05:05:12Z","fields":{"number":5,"body":"hi"}}`},
		{"schema 1.0", `{"schema":1.0,"kind":"comment","repo":"o/r","created_at":"2026-10-16T05:05:12Z","fields":{"number":5,"body":"hi"}}`},
		{"missing schema", `{"kind":"comment","repo":"o/r","created_at":"2026-10-16T05:05:12Z","fields":{"number":5,"body":"hi"}}`},
		{"missing kind", `{"schema":1,"repo":"o/r","created_at":"2026-10-16T05:05:12Z","fields":{"number":5,"body":"hi"}}`},
		{"missing repo", `{"schema":1,"kind":"comment","created_at":"2026-10-16T05:05:12Z","fields":{"number":5,"body":"hi"}}`},
		{"missing created_at", `{"schema":1,"kind":"comment","repo":"o/r","fields":{"number":5,"body":"hi"}}`},
		{"missing fields", `{"schema":1,"kind":"comment","repo":"o/r","created_at":"2026-10-16T05:05:12Z"}`},
		{"unknown kind", doc("pr.merge", `{"number":5}`)},
		{"kind in other case", doc("Comment", `{"number":5,"body":"hi"}`)},
		{"null value", doc("comment", `{"number":5,"body":null}`)},
		{"trailing object", doc("comment", `{"number":5,"body":"hi"}`) + `{}`},
		{"trailing garbage", doc("comment", `{"number":5,"body":"hi"}`) + ` x`},
		{"invalid UTF-8", doc("comment", "{\"number\":5,\"body\":\"\xff\"}")},
		{"escaped lone surrogate", doc("comment", `{"number":5,"body":"\ud800"}`)},
		{"escaped lone surrogate in key", doc("comment", `{"number":5,"body":"hi","\udc00":1}`)},
		{"U+FFFD", doc("comment", `{"number":5,"body":"\ufffd"}`)},
		{"array", `[]`},
		{"string", `"x"`},
		{"empty", ``},
		{"empty object", `{}`},
		{"fields not an object", doc("comment", `[]`)},
		{"bad created_at", `{"schema":1,"kind":"comment","repo":"o/r","created_at":"yesterday","fields":{"number":5,"body":"hi"}}`},
		{"repo without name", `{"schema":1,"kind":"fork","repo":"o","created_at":"2026-10-16T05:05:12Z","fields":{}}`},
		{"repo with extra segment", `{"schema":1,"kind":"fork","repo":"o/r/x","created_at":"2026-10-16T05:05:12Z","fields":{}}`},
		{"owner starting with hyphen", `{"schema":1,"kind":"fork","repo":"-o/r","created_at":"2026-10-16T05:05:12Z","fields":{}}`},
		{"repo named ..", `{"schema":1,"kind":"fork","repo":"o/..","created_at":"2026-10-16T05:05:12Z","fields":{}}`},
		{"repo with space", `{"schema":1,"kind":"fork","repo":"o/r x","created_at":"2026-10-16T05:05:12Z","fields":{}}`},
		{"Cyrillic owner", `{"schema":1,"kind":"fork","repo":"оwner/r","created_at":"2026-10-16T05:05:12Z","fields":{}}`},
		{"number 0", doc("comment", `{"number":0,"body":"hi"}`)},
		{"number negative", doc("comment", `{"number":-1,"body":"hi"}`)},
		{"number as string", doc("comment", `{"number":"5","body":"hi"}`)},
		{"number fractional", doc("comment", `{"number":5.5,"body":"hi"}`)},
		{"comment_id 0", doc("review.reply", `{"number":5,"comment_id":0,"body":"hi"}`)},
		{"pr.edit with neither title nor body", doc("pr.edit", `{"number":5}`)},
		{"pr.edit empty title", doc("pr.edit", `{"number":5,"title":""}`)},
		{"issue blank title", doc("issue.create", `{"title":"  ","body":"b"}`)},
		{"head without owner", pr(`"head":"me:fix/x"`, `"head":"fix/x"`)},
		{"head with bad branch", pr(`"head":"me:fix/x"`, `"head":"me:a..b"`)},
		{"head with bad owner", pr(`"head":"me:fix/x"`, `"head":"m e:fix/x"`)},
		{"base starting with hyphen", pr(`"base":"main"`, `"base":"-x"`)},
		{"base ending in .lock", pr(`"base":"main"`, `"base":"main.lock"`)},
		{"short head_sha", pr(`"head_sha":"0123456789abcdef0123456789abcdef01234567"`, `"head_sha":"abc"`)},
		{"uppercase head_sha", pr(`"head_sha":"0123456789abcdef0123456789abcdef01234567"`, `"head_sha":"0123456789ABCDEF0123456789abcdef01234567"`)},
		{"missing draft flag", pr(`,"draft":false`, ``)},
		{"draft flag as string", pr(`"draft":false`, `"draft":"false"`)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := Decode([]byte(tt.in))
			if !errors.Is(err, ErrInvalid) {
				t.Fatalf("Decode(%s) = %v, want ErrInvalid", tt.in, err)
			}
		})
	}
}
