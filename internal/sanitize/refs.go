package sanitize

import (
	"regexp"
	"strings"
)

const issueRef = `(?:#\d+|[A-Za-z0-9][A-Za-z0-9-]*/[A-Za-z0-9._-]+#\d+|https://github\.com/[A-Za-z0-9-]+/[A-Za-z0-9._-]+/(?:issues|pull)/\d+)`

var (
	closingRE   = regexp.MustCompile(`(?i)\b(close[sd]?|fix(?:e[sd])?|resolve[sd]?):?\s+(` + issueRef + `)`)
	crossRepoRE = regexp.MustCompile(`(?:^|[^A-Za-z0-9/._-])([A-Za-z0-9][A-Za-z0-9-]*/[A-Za-z0-9._-]+#\d+)\b`)
)

func refs(field, text string) []Finding {
	l := newLocator(text)
	var out []Finding
	for _, m := range closingRE.FindAllStringSubmatchIndex(text, -1) {
		out = append(out, l.finding(ClosingKeyword, field, m[0], Line(text[m[0]:m[1]]),
			"closes "+text[m[4]:m[5]]+" when merged"))
	}
	for _, m := range crossRepoRE.FindAllStringSubmatchIndex(text, -1) {
		ref := text[m[2]:m[3]]
		repo, _, _ := strings.Cut(ref, "#")
		out = append(out, l.finding(CrossRepoRef, field, m[2], Line(ref), "links to "+repo))
	}
	return out
}
