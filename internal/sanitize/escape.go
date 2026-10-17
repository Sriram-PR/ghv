// Package sanitize makes GitHub and draft text safe to show and flags what
// a human reviewer should look at twice (spec §5.4).
package sanitize

import (
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"
)

type class int

const (
	plain class = iota
	classControl
	classBidi
	classTag
	classVariationSelector
	classSoftHyphen
	classInvisible
)

func classify(r rune) class {
	switch {
	case r == '\n' || r == '\t':
		return plain
	case unicode.Is(unicode.Cc, r):
		return classControl
	case unicode.Is(unicode.Bidi_Control, r):
		return classBidi
	case r >= 0xE0000 && r <= 0xE007F:
		return classTag
	case unicode.Is(unicode.Variation_Selector, r):
		return classVariationSelector
	case r == 0x00AD:
		return classSoftHyphen
	case defaultIgnorable(r):
		return classInvisible
	}
	return plain
}

// defaultIgnorable follows the derivation of Default_Ignorable_Code_Point
// in Unicode's DerivedCoreProperties.txt, which Go has no table for.
func defaultIgnorable(r rune) bool {
	if !unicode.In(r, unicode.Other_Default_Ignorable_Code_Point, unicode.Cf, unicode.Variation_Selector) {
		return false
	}
	switch {
	case unicode.Is(unicode.White_Space, r),
		r >= 0xFFF9 && r <= 0xFFFB,
		r >= 0x13430 && r <= 0x1343F,
		unicode.Is(unicode.Prepended_Concatenation_Mark, r):
		return false
	}
	return true
}

func escapeRune(r rune) string { return fmt.Sprintf("<U+%04X>", r) }

func escapeByte(b byte) string { return fmt.Sprintf("<0x%02X>", b) }

// Terminal returns s with every control character other than \n and \t,
// every invisible or direction-changing code point, and every invalid
// UTF-8 byte replaced by a visible escape. \r\n becomes \n first.
func Terminal(s string) string {
	return escape(strings.ReplaceAll(s, "\r\n", "\n"), false)
}

// Line is Terminal for one-line values such as titles and names: \n and \t
// are escaped too.
func Line(s string) string {
	return escape(strings.ReplaceAll(s, "\r\n", "\n"), true)
}

func escape(s string, oneLine bool) string {
	var b strings.Builder
	for i := 0; i < len(s); {
		r, n := utf8.DecodeRuneInString(s[i:])
		switch {
		case r == utf8.RuneError && n == 1:
			b.WriteString(escapeByte(s[i]))
		case classify(r) != plain, oneLine && (r == '\n' || r == '\t'):
			b.WriteString(escapeRune(r))
		default:
			b.WriteString(s[i : i+n])
		}
		i += n
	}
	return b.String()
}
