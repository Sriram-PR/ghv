package sanitize

import (
	"slices"
	"strings"
	"unicode"
	"unicode/utf8"
)

var scriptNames = func() []string {
	var names []string
	for n := range unicode.Scripts {
		if n != "Common" && n != "Inherited" {
			names = append(names, n)
		}
	}
	slices.Sort(names)
	return names
}()

func scriptOf(r rune) string {
	if r < 0x80 {
		if unicode.IsLetter(r) {
			return "Latin"
		}
		return ""
	}
	for _, n := range scriptNames {
		if unicode.Is(unicode.Scripts[n], r) {
			return n
		}
	}
	return ""
}

// UTS 39's "highly restrictive" profile: one script, or Latin with the
// scripts that Japanese, Chinese, or Korean text mixes in normally.
var allowedMixes = [][]string{
	{"Han", "Hiragana", "Katakana", "Latin"},
	{"Bopomofo", "Han", "Latin"},
	{"Hangul", "Han", "Latin"},
}

func allowedMix(scripts []string) bool {
	if len(scripts) <= 1 {
		return true
	}
	for _, set := range allowedMixes {
		if !slices.ContainsFunc(scripts, func(s string) bool { return !slices.Contains(set, s) }) {
			return true
		}
	}
	return false
}

func mixedScripts(field, text string) []Finding {
	l := newLocator(text)
	var out []Finding
	cache := map[rune]string{}
	inWord := func(r rune) bool { return unicode.IsLetter(r) || unicode.IsMark(r) || unicode.IsDigit(r) }
	for i := 0; i < len(text); {
		r, n := utf8.DecodeRuneInString(text[i:])
		if !inWord(r) {
			i += n
			continue
		}
		start := i
		var scripts []string
		for i < len(text) {
			r, n = utf8.DecodeRuneInString(text[i:])
			if !inWord(r) {
				break
			}
			s, ok := cache[r]
			if !ok {
				s = scriptOf(r)
				cache[r] = s
			}
			if s != "" && !slices.Contains(scripts, s) {
				scripts = append(scripts, s)
			}
			i += n
		}
		slices.Sort(scripts)
		if !allowedMix(scripts) {
			out = append(out, l.finding(MixedScript, field, start, Line(text[start:i]), strings.Join(scripts, "+")))
		}
	}
	return out
}
