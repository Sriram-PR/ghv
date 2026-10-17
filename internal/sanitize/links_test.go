package sanitize

import (
	"slices"
	"testing"
)

func TestLinks(t *testing.T) {
	tests := []struct {
		name, in string
		want     []string
	}{
		{"none", "no links, just text.", nil},
		{"inline", "see [docs](https://a.example/d) here", []string{"https://a.example/d"}},
		{"inline with title", `[x](https://a.example/t "title")`, []string{"https://a.example/t"}},
		{"image", "![alt](https://img.example/i.png)", []string{"https://img.example/i.png"}},
		{"angle destination", "[x](<https://a.example/with space>)", []string{"https://a.example/with space"}},
		{"balanced parens", "[w](https://en.wikipedia.org/wiki/Go_(language)) end", []string{"https://en.wikipedia.org/wiki/Go_(language)"}},
		{"relative", "[r](../README.md)", []string{"../README.md"}},
		{"label differs from target", "[https://github.com/safe](https://evil.example/x)", []string{"https://github.com/safe", "https://evil.example/x"}},
		{"reference definition", "[x][1]\n\n[1]: https://ref.example/a \"t\"", []string{"https://ref.example/a"}},
		{"autolink", "<https://auto.example/p> <mailto:a@b.example>", []string{"https://auto.example/p", "mailto:a@b.example"}},
		{"bare with punctuation", "go to https://bare.example/x. Or (www.paren.example/y).", []string{"https://bare.example/x", "www.paren.example/y"}},
		{"html", `<a href="https://h.example/1">x</a> <img src='https://h.example/2'> <a href=https://h.example/3>`, []string{"https://h.example/1", "https://h.example/2", "https://h.example/3"}},
		{"javascript href", `<a href="javascript:alert(1)">x</a>`, []string{"javascript:alert(1)"}},
		{"deduplicated", "https://d.example/a and https://d.example/a", []string{"https://d.example/a"}},
		{"order", "[b](https://b.example) then https://a.example", []string{"https://b.example", "https://a.example"}},
		{"empty targets", "[x]: <>\n<a href=\"\">x</a>", nil},
		{"bare inside an outer destination", "[a](https://a.example/<https://b.example/c>/https://d.example/e)", []string{"https://a.example/<https://b.example/c>/https://d.example/e", "https://b.example/c"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Links(tt.in); !slices.Equal(got, tt.want) {
				t.Errorf("Links(%q) =\n%q\nwant\n%q", tt.in, got, tt.want)
			}
		})
	}
}

func TestLinkFindings(t *testing.T) {
	tests := []struct {
		name, in string
		want     []string
	}{
		{"ascii", "https://github.com/o/r", nil},
		{"non-ascii host", "see https://gіthub.com/o/r", []string{"body:1:5: non-ascii https://gіthub.com/o/r (link)"}},
		{"punycode", "[x](https://xn--gthub-n4a.com)", []string{"body:1:5: non-ascii https://xn--gthub-n4a.com (punycode link)"}},
		{"punycode www", "www.xn--gthub-n4a.com/x", []string{"body:1:1: non-ascii www.xn--gthub-n4a.com/x (punycode link)"}},
		{"punycode subdomain with port", "https://user@a.xn--gthub-n4a.com:8080/", []string{"body:1:1: non-ascii https://user@a.xn--gthub-n4a.com:8080/ (punycode link)"}},
		{"xn-- outside the host", "https://a.example/xn--x?q=xn--y", nil},
		{"invisible in url", "https://a.example/\u200bx", []string{"body:1:1: non-ascii https://a.example/<U+200B>x (link)"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := strs(links("body", tt.in)); !slices.Equal(got, tt.want) {
				t.Errorf("links(%q) =\n%q\nwant\n%q", tt.in, got, tt.want)
			}
		})
	}
}
