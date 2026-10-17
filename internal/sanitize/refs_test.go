package sanitize

import (
	"slices"
	"testing"
)

func TestRefs(t *testing.T) {
	tests := []struct {
		name, in string
		want     []string
	}{
		{"none", "see issue 12 and PR #3", nil},
		{"fixes", "Fixes #12", []string{"body:1:1: closing-keyword Fixes #12 (closes #12 when merged)"}},
		{"every keyword", "close #1 closes #2 closed #3 fix #4 fixes #5 fixed #6 resolve #7 resolves #8 resolved #9", []string{
			"body:1:1: closing-keyword close #1 (closes #1 when merged)",
			"body:1:10: closing-keyword closes #2 (closes #2 when merged)",
			"body:1:20: closing-keyword closed #3 (closes #3 when merged)",
			"body:1:30: closing-keyword fix #4 (closes #4 when merged)",
			"body:1:37: closing-keyword fixes #5 (closes #5 when merged)",
			"body:1:46: closing-keyword fixed #6 (closes #6 when merged)",
			"body:1:55: closing-keyword resolve #7 (closes #7 when merged)",
			"body:1:66: closing-keyword resolves #8 (closes #8 when merged)",
			"body:1:78: closing-keyword resolved #9 (closes #9 when merged)",
		}},
		{"colon and case", "RESOLVES: #40", []string{"body:1:1: closing-keyword RESOLVES: #40 (closes #40 when merged)"}},
		{"not a keyword", "prefix #1 suffixes #2", nil},
		{"cross-repo closing", "closes octo/repo#7", []string{
			"body:1:1: closing-keyword closes octo/repo#7 (closes octo/repo#7 when merged)",
			"body:1:8: cross-repo-ref octo/repo#7 (links to octo/repo)",
		}},
		{"issue url", "Fixes https://github.com/o/r/issues/3", []string{
			"body:1:1: closing-keyword Fixes https://github.com/o/r/issues/3 (closes https://github.com/o/r/issues/3 when merged)",
		}},
		{"cross-repo mention", "like (rust-lang/rust#100) said", []string{"body:1:7: cross-repo-ref rust-lang/rust#100 (links to rust-lang/rust)"}},
		{"path is not a ref", "see a/b/c#1 or x.y/z#2", nil},
		{"anchor is not a ref", "docs/page#intro", nil},
		{"second line", "ok\nfixed #1", []string{"body:2:1: closing-keyword fixed #1 (closes #1 when merged)"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := strs(refs("body", tt.in)); !slices.Equal(got, tt.want) {
				t.Errorf("refs(%q) =\n%q\nwant\n%q", tt.in, got, tt.want)
			}
		})
	}
}
