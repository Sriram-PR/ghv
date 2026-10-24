// Package doccmd finds the ghv and ghv-apply command lines in Markdown, for
// tests that run them through the real argument parsers.
package doccmd

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// Line is one command line: Args starts with "ghv" or "ghv-apply".
type Line struct {
	File string
	N    int
	Args []string
}

// shipped are the docs, from the repository root, whose command lines
// must parse.
var shipped = []string{
	"README.md",
	"SECURITY.md",
	"docs/containment.md",
	"docs/smoke-test.md",
	"docs/workflow.md",
}

// Shipped extracts the command lines of every shipped doc under root.
func Shipped(root string) ([]Line, error) {
	var out []Line
	for _, f := range shipped {
		lines, err := Extract(filepath.Join(root, f))
		if err != nil {
			return nil, err
		}
		out = append(out, lines...)
	}
	return out, nil
}

func Extract(file string) ([]Line, error) {
	b, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	return Parse(file, b)
}

// Parse takes every line of a fenced code block, with a leading "$ "
// dropped and backslash continuations joined, and every inline code span
// that starts with "ghv " or "ghv-apply". It keeps those whose command,
// after any VAR=value words and sudo with its options, is ghv or
// ghv-apply (by base name), up to the first unquoted |, ;, &, <, or >.
func Parse(file string, md []byte) ([]Line, error) {
	var out []Line
	lines := strings.Split(string(md), "\n")
	fence := ""
	for i := 0; i < len(lines); i++ {
		n := i + 1
		text := strings.TrimSpace(lines[i])
		var cands []string
		switch {
		case fence == "" && (strings.HasPrefix(text, "```") || strings.HasPrefix(text, "~~~")):
			fence = text[:3]
			continue
		case fence == "":
			for _, s := range spans(lines[i]) {
				if strings.HasPrefix(s, "ghv ") || s == "ghv-apply" || strings.HasPrefix(s, "ghv-apply ") {
					cands = append(cands, s)
				}
			}
		case strings.HasPrefix(text, fence) && strings.Trim(text, fence[:1]) == "":
			fence = ""
			continue
		default:
			for strings.HasSuffix(text, `\`) && i+1 < len(lines) {
				i++
				text = strings.TrimSuffix(text, `\`) + " " + strings.TrimSpace(lines[i])
			}
			cands = append(cands, strings.TrimPrefix(text, "$ "))
		}
		for _, c := range cands {
			words, err := split(c)
			if err != nil {
				return nil, fmt.Errorf("%s:%d: %v", file, n, err)
			}
			if args := command(words); args != nil {
				out = append(out, Line{file, n, args})
			}
		}
	}
	return out, nil
}

// spans are the text between pairs of backticks.
func spans(line string) []string {
	parts := strings.Split(line, "`")
	var out []string
	for i := 1; i < len(parts)-1; i += 2 {
		out = append(out, parts[i])
	}
	return out
}

var errUnclosed = errors.New("unclosed quote")

// split splits s into words as a POSIX shell would, without expansions,
// stopping at a comment or at the first unquoted shell operator. A word
// of digits before a redirection ("2>") is dropped.
func split(s string) ([]string, error) {
	var words []string
	var cur strings.Builder
	inWord := false
	flush := func() {
		if inWord {
			words = append(words, cur.String())
		}
		cur.Reset()
		inWord = false
	}
	for i := 0; i < len(s); i++ {
		switch c := s[i]; {
		case c == ' ' || c == '\t':
			flush()
		case c == '#' && !inWord:
			return words, nil
		case strings.IndexByte("|;&<>", c) >= 0:
			if w := cur.String(); c == '>' || c == '<' {
				if w != "" && strings.Trim(w, "0123456789") == "" {
					inWord = false
				}
			}
			flush()
			return words, nil
		case c == '\'':
			j := strings.IndexByte(s[i+1:], '\'')
			if j < 0 {
				return nil, errUnclosed
			}
			cur.WriteString(s[i+1 : i+1+j])
			i += j + 1
			inWord = true
		case c == '"':
			for i++; i < len(s) && s[i] != '"'; i++ {
				if s[i] == '\\' && i+1 < len(s) && strings.IndexByte("\"\\$`", s[i+1]) >= 0 {
					i++
				}
				cur.WriteByte(s[i])
			}
			if i == len(s) {
				return nil, errUnclosed
			}
			inWord = true
		case c == '\\' && i+1 < len(s):
			i++
			cur.WriteByte(s[i])
			inWord = true
		default:
			cur.WriteByte(c)
			inWord = true
		}
	}
	flush()
	return words, nil
}

func command(words []string) []string {
	i := 0
	for i < len(words) && isAssignment(words[i]) {
		i++
	}
	if i < len(words) && words[i] == "sudo" {
		for i++; i < len(words) && strings.HasPrefix(words[i], "-"); i++ {
			if words[i] == "-u" || words[i] == "-g" {
				i++
			}
		}
	}
	if i >= len(words) {
		return nil
	}
	name := path.Base(words[i])
	if name != "ghv" && name != "ghv-apply" {
		return nil
	}
	return append([]string{name}, words[i+1:]...)
}

func isAssignment(w string) bool {
	name, _, ok := strings.Cut(w, "=")
	if !ok || name == "" {
		return false
	}
	for i, r := range name {
		if r != '_' && !(r >= 'A' && r <= 'Z') && !(r >= 'a' && r <= 'z') && !(i > 0 && r >= '0' && r <= '9') {
			return false
		}
	}
	return true
}
