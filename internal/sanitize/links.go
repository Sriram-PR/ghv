package sanitize

import (
	"regexp"
	"slices"
	"sort"
	"strings"
)

type span struct {
	off int
	url string
}

var (
	refDefRE   = regexp.MustCompile(`(?m)^ {0,3}\[[^\]\n]+\]:[ \t]*(?:<([^>\n]*)>|(\S+))`)
	autolinkRE = regexp.MustCompile(`<([A-Za-z][A-Za-z0-9+.-]{1,31}:[^<>\s]*)>`)
	bareRE     = regexp.MustCompile(`(?i)(?:https?://|www\.)[^\s<>"'\[\]` + "`" + `]+`)
	htmlAttrRE = regexp.MustCompile(`(?i)\b(?:href|src|srcset|action|formaction|poster)\s*=\s*(?:"([^"]*)"|'([^']*)'|([^\s"'>]+))`)
)

// Links lists every link target in text once, in order of first appearance:
// inline and image links, reference definitions, autolinks, bare URLs, and
// URL attributes in HTML. It over-reports rather than misses.
func Links(text string) []string {
	var out []string
	seen := map[string]bool{}
	for _, s := range linkSpans(text) {
		if !seen[s.url] {
			seen[s.url] = true
			out = append(out, s.url)
		}
	}
	return out
}

func linkSpans(text string) []span {
	var out []span
	out = append(out, inlineLinks(text)...)
	for _, m := range refDefRE.FindAllStringSubmatchIndex(text, -1) {
		out = append(out, firstGroup(text, m))
	}
	for _, m := range autolinkRE.FindAllStringSubmatchIndex(text, -1) {
		out = append(out, firstGroup(text, m))
	}
	for _, m := range htmlAttrRE.FindAllStringSubmatchIndex(text, -1) {
		out = append(out, firstGroup(text, m))
	}
	marked := slices.Clone(out)
	slices.SortFunc(marked, func(a, b span) int { return a.off - b.off })
	for _, m := range bareRE.FindAllStringIndex(text, -1) {
		if u := trimBare(text[m[0]:m[1]]); u != "" && !inside(marked, m[0]) {
			out = append(out, span{m[0], u})
		}
	}
	slices.SortStableFunc(out, func(a, b span) int { return a.off - b.off })
	return out
}

// inside reports whether off falls within a destination already found, so
// "<https://a/b c>" is not reported again as the bare "https://a/b".
func inside(sorted []span, off int) bool {
	i := sort.Search(len(sorted), func(i int) bool { return sorted[i].off > off }) - 1
	return i >= 0 && off < sorted[i].off+len(sorted[i].url)
}

func firstGroup(text string, m []int) span {
	for i := 2; i+1 < len(m); i += 2 {
		if m[i] >= 0 {
			return span{m[i], text[m[i]:m[i+1]]}
		}
	}
	return span{m[0], ""}
}

// inlineLinks finds "](" destinations, allowing balanced parentheses inside
// them as CommonMark does.
func inlineLinks(text string) []span {
	var out []span
	for i := 0; ; {
		j := strings.Index(text[i:], "](")
		if j < 0 {
			return out
		}
		k := i + j + 2
		for k < len(text) && (text[k] == ' ' || text[k] == '\t' || text[k] == '\n') {
			k++
		}
		start, end := k, k
		if k < len(text) && text[k] == '<' {
			start++
			end = start
			for end < len(text) && text[end] != '>' && text[end] != '\n' {
				end++
			}
		} else {
			depth := 0
		dest:
			for end < len(text) {
				switch c := text[end]; {
				case c == '(':
					depth++
				case c == ')':
					if depth == 0 {
						break dest
					}
					depth--
				case c <= ' ':
					break dest
				}
				end++
			}
		}
		if end > start {
			out = append(out, span{start, text[start:end]})
		}
		i = max(end, k)
	}
}

// trimBare drops trailing punctuation and unbalanced closing parentheses,
// following GitHub's autolink extension.
func trimBare(u string) string {
	for u != "" {
		last := u[len(u)-1]
		switch {
		case strings.IndexByte("?!.,:*_~;", last) >= 0:
			u = u[:len(u)-1]
		case last == ')' && strings.Count(u, ")") > strings.Count(u, "("):
			u = u[:len(u)-1]
		default:
			return u
		}
	}
	return u
}

func links(field, text string) []Finding {
	l := newLocator(text)
	var out []Finding
	seen := map[string]bool{}
	for _, s := range linkSpans(text) {
		if seen[s.url] {
			continue
		}
		seen[s.url] = true
		switch {
		case !isASCII(s.url):
			out = append(out, l.finding(NonASCII, field, s.off, Line(s.url), "link"))
		case strings.Contains(strings.ToLower(s.url), "xn--"):
			out = append(out, l.finding(NonASCII, field, s.off, Line(s.url), "punycode link"))
		}
	}
	return out
}
