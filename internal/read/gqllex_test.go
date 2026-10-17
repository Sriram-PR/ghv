package read

import (
	"slices"
	"testing"
)

var kindName = map[tokKind]string{
	tokEOF: "eof", tokPunct: "punct", tokName: "name", tokInt: "int",
	tokFloat: "float", tokString: "string", tokBlockString: "block",
}

func describe(toks []token) []string {
	var out []string
	for _, t := range toks {
		out = append(out, kindName[t.kind]+":"+t.text)
	}
	return out
}

func TestLexGraphQL(t *testing.T) {
	tests := []struct {
		src  string
		want []string
	}{
		{"query { a }", []string{"name:query", "punct:{", "name:a", "punct:}", "eof:"}},
		{"\uFEFF{a}", []string{"punct:{", "name:a", "punct:}", "eof:"}},
		{"a,b,,c", []string{"name:a", "name:b", "name:c", "eof:"}},
		{"# mutation { x }\n{a}", []string{"punct:{", "name:a", "punct:}", "eof:"}},
		{"#c\r{a}", []string{"punct:{", "name:a", "punct:}", "eof:"}},
		{`{a(s:"mutation \"x\" \\ \u00e9 \u{1F600}")}`, []string{
			"punct:{", "name:a", "punct:(", "name:s", "punct::",
			`string:"mutation \"x\" \\ \u00e9 \u{1F600}"`, "punct:)", "punct:}", "eof:"}},
		{`{a(s:"""block "" \""" mutation""")}`, []string{
			"punct:{", "name:a", "punct:(", "name:s", "punct::",
			`block:"""block "" \""" mutation"""`, "punct:)", "punct:}", "eof:"}},
		{"{a(n:-1.5e+3,m:0,k:12,f:0.5)}", []string{
			"punct:{", "name:a", "punct:(",
			"name:n", "punct::", "float:-1.5e+3",
			"name:m", "punct::", "int:0",
			"name:k", "punct::", "int:12",
			"name:f", "punct::", "float:0.5",
			"punct:)", "punct:}", "eof:"}},
		{"...F", []string{"punct:...", "name:F", "eof:"}},
		{"\"tab\tok\"", []string{"string:\"tab\tok\"", "eof:"}},
		{"", []string{"eof:"}},
	}
	for _, tt := range tests {
		got, err := lexGraphQL(tt.src)
		if err != nil {
			t.Errorf("lexGraphQL(%q): %v", tt.src, err)
			continue
		}
		if d := describe(got); !slices.Equal(d, tt.want) {
			t.Errorf("lexGraphQL(%q) = %q, want %q", tt.src, d, tt.want)
		}
	}
}

func TestLexGraphQLErrors(t *testing.T) {
	for _, src := range []string{
		`"open`, `"""open`, `"\q"`, `"\u12"`, `"\u{}"`, `"\u{12"`,
		"\"line\nbreak\"", "\"ctl\x01\"", "01", "1.", "1e", "1.e3", "1a", "0x1", "1.5.",
		".", "..", `\u006dutation`, "é", "a\x00", "\xff", "#bad\x01comment", "-", "-a",
	} {
		if _, err := lexGraphQL(src); err == nil {
			t.Errorf("lexGraphQL(%q) = nil error, want an error", src)
		}
	}
}
