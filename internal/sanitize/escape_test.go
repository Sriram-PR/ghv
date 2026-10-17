package sanitize

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTerminal(t *testing.T) {
	tests := []struct{ in, want string }{
		{"plain text\n\twith tab", "plain text\n\twith tab"},
		{"crlf\r\nline", "crlf\nline"},
		{"lone\rcr", "lone<U+000D>cr"},
		{"\x1b]52;c;aGk=\x07", "<U+001B>]52;c;aGk=<U+0007>"},
		{"\x1b]8;;https://evil\x1b\\x\x1b]8;;\x1b\\", "<U+001B>]8;;https://evil<U+001B>\\x<U+001B>]8;;<U+001B>\\"},
		{"del\x7f", "del<U+007F>"},
		{"c1 \u0085 \u009b", "c1 <U+0085> <U+009B>"},
		{"bidi \u202eevil\u202c \u2066x\u2069 \u200f \u061c", "bidi <U+202E>evil<U+202C> <U+2066>x<U+2069> <U+200F> <U+061C>"},
		{"zero\u200bwidth \u200c\u200d \u2060 \ufeff", "zero<U+200B>width <U+200C><U+200D> <U+2060> <U+FEFF>"},
		{"hangul\u3164filler \u115f \uffa0", "hangul<U+3164>filler <U+115F> <U+FFA0>"},
		{"cgj \u034f mongolian \u180e", "cgj <U+034F> mongolian <U+180E>"},
		{"tag \U000E0041\U000E0042\U000E007F \U000E0001", "tag <U+E0041><U+E0042><U+E007F> <U+E0001>"},
		{"vs \u2764\ufe0f \ufe0e \U000E0100 \u180b", "vs \u2764<U+FE0F> <U+FE0E> <U+E0100> <U+180B>"},
		{"soft\u00adhyphen", "soft<U+00AD>hyphen"},
		{"bad \xff\xfe byte", "bad <0xFF><0xFE> byte"},
		{"real \ufffd stays", "real \ufffd stays"},
		{"visible format \u0600 \ufff9 \U00013430", "visible format \u0600 \ufff9 \U00013430"},
		{"nbsp\u00a0and\u2028sep", "nbsp\u00a0and\u2028sep"},
		{"émoji 🙂 中文 ру", "émoji 🙂 中文 ру"},
	}
	for _, tt := range tests {
		if got := Terminal(tt.in); got != tt.want {
			t.Errorf("Terminal(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestLine(t *testing.T) {
	tests := []struct{ in, want string }{
		{"title", "title"},
		{"two\nlines\tand tab", "two<U+000A>lines<U+0009>and tab"},
		{"crlf\r\n", "crlf<U+000A>"},
		{"\u202eevil", "<U+202E>evil"},
	}
	for _, tt := range tests {
		if got := Line(tt.in); got != tt.want {
			t.Errorf("Line(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestEveryControlIsEscaped(t *testing.T) {
	for r := rune(0); r <= 0x10FFFF; r++ {
		if r >= 0xD800 && r <= 0xDFFF {
			continue
		}
		isControl := r < 0x20 || (r >= 0x7f && r <= 0x9f)
		if isControl && r != '\n' && r != '\t' && classify(r) != classControl {
			t.Errorf("classify(%U) = %d, want classControl", r, classify(r))
		}
		if !isControl && classify(r) == classControl {
			t.Errorf("classify(%U) = classControl", r)
		}
	}
}

func TestDefaultIgnorable(t *testing.T) {
	yes := []rune{0x00AD, 0x034F, 0x061C, 0x115F, 0x1160, 0x17B4, 0x17B5, 0x180B, 0x180E, 0x180F,
		0x200B, 0x200F, 0x202A, 0x202E, 0x2060, 0x2064, 0x2066, 0x206F, 0x3164, 0xFE00, 0xFE0F,
		0xFEFF, 0xFFA0, 0x1BCA0, 0x1D173, 0x1D17A, 0xE0000, 0xE0001, 0xE0020, 0xE007F, 0xE0100, 0xE0FFF}
	no := []rune{' ', 'a', 0x00A0, 0x0600, 0x0605, 0x06DD, 0x110BD, 0x2028, 0xFFF9, 0xFFFB, 0x13430, 0x1343F, 0xFFFD}
	for _, r := range yes {
		if !defaultIgnorable(r) {
			t.Errorf("defaultIgnorable(%U) = false, want true", r)
		}
	}
	for _, r := range no {
		if defaultIgnorable(r) {
			t.Errorf("defaultIgnorable(%U) = true, want false", r)
		}
	}
}

func FuzzTerminal(f *testing.F) {
	for _, s := range []string{"", "a\r\nb", "\x1b[31m", "\u202e", "\U000E0041", "\xff", "\u2764\ufe0f", "a\tb\nc"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		for name, out := range map[string]string{"Terminal": Terminal(s), "Line": Line(s)} {
			if !utf8.ValidString(out) {
				t.Fatalf("%s(%q) is not valid UTF-8", name, s)
			}
			for _, r := range out {
				if classify(r) != plain || (name == "Line" && (r == '\n' || r == '\t')) {
					t.Fatalf("%s(%q) = %q keeps %U", name, s, out, r)
				}
			}
			if name == "Terminal" && Terminal(out) != out {
				t.Fatalf("Terminal is not idempotent on %q", s)
			}
			if name == "Line" && Line(out) != out {
				t.Fatalf("Line is not idempotent on %q", s)
			}
		}
		if !strings.Contains(s, "\r") && !strings.ContainsFunc(s, func(r rune) bool { return classify(r) != plain || r == utf8.RuneError }) {
			if Terminal(s) != s {
				t.Fatalf("Terminal changed clean input %q", s)
			}
		}
	})
}
