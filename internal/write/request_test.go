package write

import (
	"crypto/sha256"
	"strings"
	"testing"
	"time"

	"github.com/Sriram-PR/ghv/internal/draft"
)

func ptr(s string) *string { return &s }

func TestBuild(t *testing.T) {
	created := time.Date(2026, 10, 16, 5, 5, 12, 0, time.UTC)
	for _, c := range []struct {
		name   string
		fields draft.Fields
		method string
		path   string
		body   string
		want   int
	}{
		{"fork", draft.ForkFields{}, "POST", "/repos/moby/moby/forks", `{}`, 202},
		{"pr.create", draft.PRCreateFields{Base: "release/1.x", Head: "Sriram-PR:fix/a", HeadSHA: strings.Repeat("a", 40),
			Title: "fix: a <b> & c", Body: "line 1\nline 2", Draft: true, MaintainerCanModify: false},
			"POST", "/repos/moby/moby/pulls",
			`{"title":"fix: a <b> & c","head":"Sriram-PR:fix/a","base":"release/1.x","body":"line 1\nline 2","draft":true,"maintainer_can_modify":false}`, 201},
		{"pr.edit title", draft.PREditFields{Number: 12, Title: ptr("New")}, "PATCH", "/repos/moby/moby/pulls/12", `{"title":"New"}`, 200},
		{"pr.edit body", draft.PREditFields{Number: 12, Body: ptr("")}, "PATCH", "/repos/moby/moby/pulls/12", `{"body":""}`, 200},
		{"pr.edit both", draft.PREditFields{Number: 12, Title: ptr("T"), Body: ptr("B")}, "PATCH", "/repos/moby/moby/pulls/12", `{"title":"T","body":"B"}`, 200},
		{"comment", draft.CommentFields{Number: 7, Body: "hi"}, "POST", "/repos/moby/moby/issues/7/comments", `{"body":"hi"}`, 201},
		{"review.reply", draft.ReviewReplyFields{Number: 7, CommentID: 9876543210, Body: "yes"},
			"POST", "/repos/moby/moby/pulls/7/comments/9876543210/replies", `{"body":"yes"}`, 201},
		{"issue.create", draft.IssueCreateFields{Title: "Bug", Body: ""}, "POST", "/repos/moby/moby/issues", `{"title":"Bug","body":""}`, 201},
	} {
		t.Run(c.name, func(t *testing.T) {
			d := draft.Draft{ID: "d-0123456789ab", Repo: "docker/docker", CreatedAt: created, Fields: c.fields}
			r, err := Build(d, "moby/moby")
			if err != nil {
				t.Fatal(err)
			}
			if r.Method != c.method || r.Path != c.path || string(r.Body) != c.body || r.Want != c.want {
				t.Fatalf("got %s %s %s want %d", r.Method, r.Path, r.Body, r.Want)
			}
			sum := sha256.Sum256([]byte(c.method + " " + c.path + "\n" + c.body))
			if r.SHA256() != sum {
				t.Fatalf("SHA256 is not of METHOD PATH LF BODY")
			}
		})
	}
}

func TestBuildEscapesTheName(t *testing.T) {
	d := draft.Draft{Repo: "o/r", CreatedAt: time.Now(), Fields: draft.CommentFields{Number: 1, Body: "x"}}
	r, err := Build(d, "o/a b")
	if err != nil {
		t.Fatal(err)
	}
	if r.Path != "/repos/o/a%20b/issues/1/comments" {
		t.Fatalf("path %q", r.Path)
	}
	for _, bad := range []string{"", "o", "o/", "/r", "o/r/x", "o/..", "./r", "o/r?x=1"} {
		if _, err := Build(d, bad); err == nil {
			t.Errorf("Build accepted the name %q", bad)
		}
	}
}

func TestBuildRefusesAnInvalidDraft(t *testing.T) {
	d := draft.Draft{Repo: "o/r", CreatedAt: time.Now(), Fields: draft.CommentFields{Number: 0, Body: "x"}}
	if _, err := Build(d, "o/r"); err == nil {
		t.Fatal("Build accepted issue number 0")
	}
}
