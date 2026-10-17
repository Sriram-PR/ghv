package sanitize

import (
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"
)

type Class string

const (
	Control           Class = "control"
	InvalidUTF8       Class = "invalid-utf8"
	Bidi              Class = "bidi"
	Tag               Class = "tag"
	VariationSelector Class = "variation-selector"
	SoftHyphen        Class = "soft-hyphen"
	Invisible         Class = "invisible"
	NonASCII          Class = "non-ascii"
	MixedScript       Class = "mixed-script"
	Mention           Class = "mention"
	BotMention        Class = "bot-mention"
	SlashCommand      Class = "slash-command"
	BotCommand        Class = "bot-command"
	ClosingKeyword    Class = "closing-keyword"
	CrossRepoRef      Class = "cross-repo-ref"
	Secret            Class = "secret"
)

// Rejects reports whether a finding of this class stops the draft being sent.
func (c Class) Rejects() bool { return c == Control || c == InvalidUTF8 }

// Finding is one thing in a field the reviewer should see. Line and Col are
// 1-based; Col counts runes. Text is already safe to print.
type Finding struct {
	Class  Class
	Field  string
	Line   int
	Col    int
	Text   string
	Detail string
}

func (f Finding) String() string {
	s := fmt.Sprintf("%s:%d:%d: %s %s", f.Field, f.Line, f.Col, f.Class, f.Text)
	if f.Detail != "" {
		s += " (" + f.Detail + ")"
	}
	return s
}

func isASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] >= 0x80 {
			return false
		}
	}
	return true
}

// locator turns byte offsets into line and column. Checks ask in mostly
// increasing order, so it counts runes on from the last answer instead of
// from the start of the line, which keeps one long line linear.
type locator struct {
	text           string
	starts         []int
	off, line, col int
}

func newLocator(text string) *locator {
	l := &locator{text: text, starts: []int{0}, line: 1, col: 1}
	for i := 0; i < len(text); i++ {
		if text[i] == '\n' {
			l.starts = append(l.starts, i+1)
		}
	}
	return l
}

func (l *locator) at(off int) (line, col int) {
	i := sort.Search(len(l.starts), func(i int) bool { return l.starts[i] > off }) - 1
	if i+1 != l.line || off < l.off {
		l.off, l.line, l.col = l.starts[i], i+1, 1
	}
	l.col += utf8.RuneCountInString(l.text[l.off:off])
	l.off = off
	return l.line, l.col
}

func (l *locator) finding(c Class, field string, off int, text, detail string) Finding {
	line, col := l.at(off)
	return Finding{Class: c, Field: field, Line: line, Col: col, Text: text, Detail: detail}
}

var classNames = map[class]Class{
	classControl:           Control,
	classBidi:              Bidi,
	classTag:               Tag,
	classVariationSelector: VariationSelector,
	classSoftHyphen:        SoftHyphen,
	classInvisible:         Invisible,
}

const maxRunShown = 8

// chars reports each run of consecutive same-class characters once. A run
// of tag characters also shows the ASCII it spells, since that is what a
// model reading the text sees.
func chars(field, text string) []Finding {
	l := newLocator(text)
	var out []Finding
	var (
		cur     Class
		start   int
		escaped []string
		hidden  strings.Builder
	)
	flush := func() {
		if cur == "" {
			return
		}
		shown := strings.Join(escaped[:min(len(escaped), maxRunShown)], "")
		if len(escaped) > maxRunShown {
			shown += fmt.Sprintf(" +%d more", len(escaped)-maxRunShown)
		}
		detail := ""
		if cur == Tag && hidden.Len() > 0 {
			detail = "spells " + fmt.Sprintf("%q", hidden.String())
		}
		out = append(out, l.finding(cur, field, start, shown, detail))
		cur, escaped = "", nil
		hidden.Reset()
	}
	for i := 0; i < len(text); {
		r, n := utf8.DecodeRuneInString(text[i:])
		c, esc := Class(""), ""
		if r == utf8.RuneError && n == 1 {
			c, esc = InvalidUTF8, escapeByte(text[i])
		} else if k := classify(r); k != plain {
			c, esc = classNames[k], escapeRune(r)
		}
		if c != cur {
			flush()
			cur, start = c, i
		}
		if c != "" {
			escaped = append(escaped, esc)
			if c == Tag && r >= 0xE0020 && r <= 0xE007E {
				hidden.WriteRune(r - 0xE0000)
			}
		}
		i += n
	}
	flush()
	return out
}
