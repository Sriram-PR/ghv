package sanitize

import (
	"cmp"
	"fmt"
	"slices"

	"github.com/Sriram-PR/ghv/internal/draft"
)

// maxPerClass bounds what one field can put in front of the reviewer; the
// rest of a class is summed up in one last finding.
const maxPerClass = 100

// Scan runs every text check over one field.
func Scan(field, text string) []Finding {
	var out []Finding
	for _, check := range []func(field, text string) []Finding{
		chars, mentions, commands, refs, links, mixedScripts, secrets,
	} {
		out = append(out, check(field, text)...)
	}
	slices.SortStableFunc(out, func(a, b Finding) int {
		return cmp.Or(cmp.Compare(a.Line, b.Line), cmp.Compare(a.Col, b.Col))
	})
	return capPerClass(out)
}

func capPerClass(fs []Finding) []Finding {
	total := map[Class]int{}
	for _, f := range fs {
		total[f.Class]++
	}
	seen := map[Class]int{}
	var out []Finding
	for _, f := range fs {
		seen[f.Class]++
		switch n := seen[f.Class]; {
		case n <= maxPerClass:
			out = append(out, f)
		case n == maxPerClass+1:
			f.Text, f.Detail = "...", fmt.Sprintf("%d more not shown", total[f.Class]-maxPerClass)
			out = append(out, f)
		}
	}
	return out
}

type Report struct {
	Findings []Finding
	Links    []string
}

func (r Report) Rejected() bool {
	return slices.ContainsFunc(r.Findings, func(f Finding) bool { return f.Class.Rejects() })
}

// CheckDraft scans each human-written field of d. Branch names get the
// character and non-ASCII checks only: they are names, not prose.
func CheckDraft(d draft.Draft) Report {
	var r Report
	name := func(field, v string) {
		r.Findings = append(r.Findings, chars(field, v)...)
		if !isASCII(v) {
			r.Findings = append(r.Findings, Finding{Class: NonASCII, Field: field, Line: 1, Col: 1, Text: Line(v), Detail: field + " name"})
		}
	}
	prose := func(field, v string) {
		r.Findings = append(r.Findings, Scan(field, v)...)
		for _, u := range Links(v) {
			if !slices.Contains(r.Links, u) {
				r.Links = append(r.Links, u)
			}
		}
	}
	name("repo", d.Repo)
	switch f := d.Fields.(type) {
	case draft.PRCreateFields:
		name("base", f.Base)
		name("head", f.Head)
		prose("title", f.Title)
		prose("body", f.Body)
	case draft.PREditFields:
		if f.Title != nil {
			prose("title", *f.Title)
		}
		if f.Body != nil {
			prose("body", *f.Body)
		}
	case draft.CommentFields:
		prose("body", f.Body)
	case draft.ReviewReplyFields:
		prose("body", f.Body)
	case draft.IssueCreateFields:
		prose("title", f.Title)
		prose("body", f.Body)
	}
	return r
}
