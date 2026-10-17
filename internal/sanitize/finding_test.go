package sanitize

import (
	"slices"
	"testing"
)

func strs(fs []Finding) []string {
	var out []string
	for _, f := range fs {
		out = append(out, f.String())
	}
	return out
}

func TestChars(t *testing.T) {
	tests := []struct {
		name, in string
		want     []string
	}{
		{"clean", "hello\n\tworld", nil},
		{"escape on line 2", "ok\nab\x1b[31m", []string{"body:2:3: control <U+001B>"}},
		{"carriage return", "a\r\nb", []string{"body:1:2: control <U+000D>"}},
		{"invalid byte", "a\xffb", []string{"body:1:2: invalid-utf8 <0xFF>"}},
		{"bidi run", "x\u202e\u2066y", []string{"body:1:2: bidi <U+202E><U+2066>"}},
		{"classes split runs", "\u200b\u202e", []string{"body:1:1: invisible <U+200B>", "body:1:2: bidi <U+202E>"}},
		{"column counts runes", "héllo\u200b", []string{"body:1:6: invisible <U+200B>"}},
		{"soft hyphen", "a\u00adb", []string{"body:1:2: soft-hyphen <U+00AD>"}},
		{"variation selector", "❤\ufe0f", []string{"body:1:2: variation-selector <U+FE0F>"}},
		{"tags spell text", "hi\U000E0069\U000E0067\U000E006E", []string{`body:1:3: tag <U+E0069><U+E0067><U+E006E> (spells "ign")`}},
		{"long run is cut", "\u200b\u200b\u200b\u200b\u200b\u200b\u200b\u200b\u200b\u200b", []string{
			"body:1:1: invisible <U+200B><U+200B><U+200B><U+200B><U+200B><U+200B><U+200B><U+200B> +2 more"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := strs(chars("body", tt.in)); !slices.Equal(got, tt.want) {
				t.Errorf("chars(%q) =\n%q\nwant\n%q", tt.in, got, tt.want)
			}
		})
	}
}

func TestRejects(t *testing.T) {
	for _, c := range []Class{Control, InvalidUTF8} {
		if !c.Rejects() {
			t.Errorf("%s.Rejects() = false", c)
		}
	}
	for _, c := range []Class{Bidi, Tag, VariationSelector, SoftHyphen, Invisible, NonASCII, MixedScript,
		Mention, BotMention, SlashCommand, BotCommand, ClosingKeyword, CrossRepoRef, Secret} {
		if c.Rejects() {
			t.Errorf("%s.Rejects() = true", c)
		}
	}
}

func TestLocator(t *testing.T) {
	text := "ab\nçd\n\nxyz"
	l := newLocator(text)
	for _, tt := range []struct{ off, line, col int }{
		{0, 1, 1}, {1, 1, 2}, {3, 2, 1}, {5, 2, 2}, {9, 4, 2}, {2, 1, 3}, {8, 4, 1}, {7, 3, 1},
	} {
		if line, col := l.at(tt.off); line != tt.line || col != tt.col {
			t.Errorf("at(%d) = %d:%d, want %d:%d", tt.off, line, col, tt.line, tt.col)
		}
	}
}
