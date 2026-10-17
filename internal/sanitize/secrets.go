package sanitize

import (
	"fmt"
	"math"
	"regexp"
	"strings"
)

const (
	minSecretLen     = 40
	minSecretEntropy = 4.0
)

var (
	tokenRE = regexp.MustCompile(`\b(?:gh[pousr]_|github_pat_)[A-Za-z0-9_]*`)
	pemRE   = regexp.MustCompile(`(?s)-----BEGIN[^\n]*.*?(?:-----END[^\n]*|\z)`)
	runRE   = regexp.MustCompile(`[A-Za-z0-9+/=_-]{40,}`)
)

func secrets(field, text string) []Finding {
	l := newLocator(text)
	var out []Finding
	var covered [][]int
	for _, m := range pemRE.FindAllStringIndex(text, -1) {
		header, _, _ := strings.Cut(text[m[0]:m[1]], "\n")
		out = append(out, l.finding(Secret, field, m[0], Line(header), "PEM block"))
		covered = append(covered, m)
	}
	for _, m := range tokenRE.FindAllStringIndex(text, -1) {
		out = append(out, l.finding(Secret, field, m[0], redact(text[m[0]:m[1]]), "GitHub token prefix"))
		covered = append(covered, m)
	}
	for _, m := range runRE.FindAllStringIndex(text, -1) {
		run := text[m[0]:m[1]]
		if overlaps(covered, m) || !looksSecret(run) {
			continue
		}
		out = append(out, l.finding(Secret, field, m[0], redact(run), "high-entropy string"))
	}
	return out
}

func overlaps(spans [][]int, m []int) bool {
	for _, s := range spans {
		if m[0] < s[1] && s[0] < m[1] {
			return true
		}
	}
	return false
}

// looksSecret skips hex (commit SHAs, digests), paths, and strings without
// both letters and digits; long random tokens score at least 4.18 bits.
func looksSecret(s string) bool {
	if strings.Count(s, "/") >= 3 {
		return false
	}
	if strings.Trim(s, "0123456789abcdefABCDEF-") == "" {
		return false
	}
	if !strings.ContainsAny(s, "0123456789") || !strings.ContainsFunc(s, func(r rune) bool { return r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' }) {
		return false
	}
	return entropy(s) >= minSecretEntropy
}

func entropy(s string) float64 {
	var counts [256]int
	for i := 0; i < len(s); i++ {
		counts[s[i]]++
	}
	var h float64
	for _, c := range counts {
		if c > 0 {
			p := float64(c) / float64(len(s))
			h -= p * math.Log2(p)
		}
	}
	return h
}

// redact keeps enough to recognise the value without repeating it.
func redact(s string) string {
	if len(s) <= 8 {
		return s
	}
	return fmt.Sprintf("%s... (%d chars)", s[:6], len(s))
}
