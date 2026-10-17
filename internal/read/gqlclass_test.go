package read

import (
	"strings"
	"testing"
)

var classifierSeeds = []struct {
	doc string
	ok  bool
}{
	{"{ viewer { login } }", true},
	{"query { a } query B { b }", true},
	{"query A { a } mutation B { b }", false},
	{"mutation { a }", false},
	{"subscription { a }", false},
	{",,mutation{a}", false},
	{"# mutation\n{ a }", true},
	{`{ a(q: """mutation { x }""") }`, true},
	{"\uFEFF{ a }", true},
	{`{ a(q: "\u006dutation") }`, true},
	{`\u006dutation { a }`, false},
	{"query { ...F } fragment F on Query { a }", true},
	{"{ ... on Query { a } ... @include(if: true) { b } }", true},
	{"{ mutation: viewer { login } }", true},
	{`{ a(q: "mutation") }`, true},
	{"query mutation { a }", true}, // a query operation named "mutation"
	{`query ($id: ID! = "x", $l: [Int!]! = [1, 2]) @live { node(id: $id, o: {k: [true, null, E]}) { id } }`, true},
	{"type Query { a: Int }", false},
	{"extend type Query { a: Int }", false},
	{"schema { query: Q }", false},
	{`"mutation" { a }`, false},
	{"fragment on on Query { a }", false},
	{"", false},
	{"query", false},
	{"{ a ", false},
	{"{}", false},
	{"{ a } }", false},
	{"{ a(x:) }", false},
}

func TestClassifyQuery(t *testing.T) {
	for _, s := range classifierSeeds {
		err := classifyQuery(s.doc)
		if (err == nil) != s.ok {
			t.Errorf("classifyQuery(%q) = %v, want ok=%v", s.doc, err, s.ok)
		}
	}
}

func TestClassifyQueryDepthLimit(t *testing.T) {
	nest := func(n int) string { return strings.Repeat("{a", n) + strings.Repeat("}", n) }
	if err := classifyQuery(nest(10)); err != nil {
		t.Errorf("depth 10: %v", err)
	}
	if err := classifyQuery(nest(100)); err == nil {
		t.Error("depth 100 accepted")
	}
	deepList := "{a(x:" + strings.Repeat("[", 100) + strings.Repeat("]", 100) + ")}"
	if err := classifyQuery(deepList); err == nil {
		t.Error("100 nested lists accepted")
	}
}

// topLevelStarts finds where each top-level definition begins by brace
// counting alone: an oracle independent of the parser.
func topLevelStarts(toks []token) []token {
	var starts []token
	braces, parens := 0, 0
	expectStart := true
	for _, t := range toks {
		if t.kind == tokEOF {
			break
		}
		if braces == 0 && parens == 0 && expectStart {
			starts = append(starts, t)
			expectStart = false
		}
		if t.kind != tokPunct {
			continue
		}
		switch t.text {
		case "(":
			parens++
		case ")":
			parens--
		case "{":
			braces++
		case "}":
			braces--
			if braces == 0 && parens == 0 {
				expectStart = true
			}
		}
	}
	return starts
}

func FuzzClassifyQuery(f *testing.F) {
	for _, s := range classifierSeeds {
		f.Add(s.doc)
	}
	f.Fuzz(func(t *testing.T, doc string) {
		if classifyQuery(doc) != nil {
			return
		}
		toks, err := lexGraphQL(doc)
		if err != nil {
			t.Fatalf("accepted %q, which the lexer rejects: %v", doc, err)
		}
		for _, s := range topLevelStarts(toks) {
			ok := s.kind == tokPunct && s.text == "{" ||
				s.kind == tokName && (s.text == "query" || s.text == "fragment")
			if !ok {
				t.Fatalf("accepted %q with a definition starting %q", doc, s.text)
			}
		}
	})
}
