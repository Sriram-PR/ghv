package main

import (
	"slices"
	"testing"

	"github.com/Sriram-PR/ghv/internal/sanitize"
)

func TestJudge(t *testing.T) {
	rep := sanitize.Report{Findings: []sanitize.Finding{
		{Class: sanitize.Mention, Field: "body", Line: 1, Col: 1, Text: "@a"},
		{Class: sanitize.Control, Field: "body", Line: 1, Col: 3, Text: "<U+001B>"},
		{Class: sanitize.Secret, Field: "body", Line: 2, Col: 1, Text: "ghp_ab... (40 characters)"},
		{Class: sanitize.Mention, Field: "title", Line: 1, Col: 5, Text: "@b"},
	}}
	rc := reviewContext{Blocks: []string{"o/r has no issue or pull request #8"}, Flags: []string{"pull request #7 is closed"}}
	v := judge(rep, rc)
	wantBlocks := []string{
		"the draft holds a character ghv-apply refuses: body:1:3: control <U+001B>",
		"o/r has no issue or pull request #8",
	}
	wantFlags := []string{"pull request #7 is closed", "findings to read: mention 2, secret 1"}
	if !slices.Equal(v.Blocks, wantBlocks) || !slices.Equal(v.Flags, wantFlags) {
		t.Fatalf("blocks %q\nflags %q", v.Blocks, v.Flags)
	}
	if v := judge(sanitize.Report{}, reviewContext{}); v.Blocks != nil || v.Flags != nil {
		t.Fatalf("clean draft: %+v", v)
	}
}

func TestRenderVerdict(t *testing.T) {
	got := renderVerdict(verdict{Blocks: []string{"b1"}, Flags: []string{"f1", "f2"}})
	want := []string{"ghv-apply will not send this draft:", "  - b1", "Check before deciding:", "  - f1", "  - f2"}
	if !slices.Equal(got, want) {
		t.Fatalf("%q", got)
	}
	if got := renderVerdict(verdict{}); !slices.Equal(got, []string{"Nothing flagged."}) {
		t.Fatalf("%q", got)
	}
}

func TestActions(t *testing.T) {
	plain, withDiff, all := offered(false, false), offered(false, true), offered(true, true)
	if prompt(plain) != "[r]eject, [s]kip, or [q]uit? " || prompt(withDiff) != "[r]eject, [s]kip, [d]iff, or [q]uit? " ||
		prompt(all) != "[a]pprove, [r]eject, [s]kip, [d]iff, or [q]uit? " {
		t.Fatalf("prompts %q, %q, %q", prompt(plain), prompt(withDiff), prompt(all))
	}
	for _, tt := range []struct {
		answer string
		as     []action
		want   action
		ok     bool
	}{
		{"r", plain, actReject, true},
		{" Reject \t", plain, actReject, true},
		{"S", plain, actSkip, true},
		{"quit", plain, actQuit, true},
		{"d", withDiff, actDiff, true},
		{"d", plain, 0, false},
		{"", plain, 0, false},
		{"y", plain, 0, false},
		{"a", plain, 0, false},
		{"a", all, actApprove, true},
		{"Approve", all, actApprove, true},
		{"rs", plain, 0, false},
	} {
		got, ok := parseAction(tt.answer, tt.as)
		if got != tt.want || ok != tt.ok {
			t.Errorf("parseAction(%q) = %q, %v", tt.answer, got, ok)
		}
	}
}
