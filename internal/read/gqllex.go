package read

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// Lexical grammar: GraphQL spec, October 2021, section 2.1.

type tokKind int

const (
	tokEOF tokKind = iota
	tokPunct
	tokName
	tokInt
	tokFloat
	tokString
	tokBlockString
)

type token struct {
	kind tokKind
	text string
	pos  int
}

func lexGraphQL(src string) ([]token, error) {
	var toks []token
	i := 0
	for i < len(src) {
		r, size := utf8.DecodeRuneInString(src[i:])
		switch {
		case r == '\uFEFF' || r == ' ' || r == '\t' || r == '\n' || r == '\r' || r == ',':
			i += size
		case badSourceRune(r, size):
			return nil, fmt.Errorf("graphql: invalid character at %d", i)
		case r == '#':
			j := i + 1
			for j < len(src) && src[j] != '\n' && src[j] != '\r' {
				cr, n := utf8.DecodeRuneInString(src[j:])
				if badSourceRune(cr, n) {
					return nil, fmt.Errorf("graphql: invalid character in comment at %d", j)
				}
				j += n
			}
			i = j
		case strings.HasPrefix(src[i:], "..."):
			toks = append(toks, token{tokPunct, "...", i})
			i += 3
		case strings.ContainsRune("!$&():=@[]{|}", r):
			toks = append(toks, token{tokPunct, src[i : i+1], i})
			i++
		case isNameStart(r):
			j := i + 1
			for j < len(src) && isNameContinue(rune(src[j])) {
				j++
			}
			toks = append(toks, token{tokName, src[i:j], i})
			i = j
		case r == '-' || isDigit(r):
			kind, j, err := lexNumber(src, i)
			if err != nil {
				return nil, err
			}
			toks = append(toks, token{kind, src[i:j], i})
			i = j
		case strings.HasPrefix(src[i:], `"""`):
			j, err := lexBlockString(src, i)
			if err != nil {
				return nil, err
			}
			toks = append(toks, token{tokBlockString, src[i:j], i})
			i = j
		case r == '"':
			j, err := lexString(src, i)
			if err != nil {
				return nil, err
			}
			toks = append(toks, token{tokString, src[i:j], i})
			i = j
		default:
			return nil, fmt.Errorf("graphql: unexpected %q at %d", r, i)
		}
	}
	return append(toks, token{kind: tokEOF, pos: len(src)}), nil
}

func badSourceRune(r rune, size int) bool {
	return r == utf8.RuneError && size == 1 || r < 0x20 && r != '\t'
}

func isDigit(r rune) bool        { return r >= '0' && r <= '9' }
func isNameStart(r rune) bool    { return r == '_' || r >= 'A' && r <= 'Z' || r >= 'a' && r <= 'z' }
func isNameContinue(r rune) bool { return isNameStart(r) || isDigit(r) }

func isHex(c byte) bool {
	return c >= '0' && c <= '9' || c >= 'a' && c <= 'f' || c >= 'A' && c <= 'F'
}

func lexNumber(src string, i int) (tokKind, int, error) {
	bad := fmt.Errorf("graphql: malformed number at %d", i)
	j := i
	if src[j] == '-' {
		j++
	}
	if j >= len(src) || !isDigit(rune(src[j])) {
		return 0, 0, bad
	}
	if src[j] == '0' {
		j++
	} else {
		for j < len(src) && isDigit(rune(src[j])) {
			j++
		}
	}
	kind := tokInt
	if j < len(src) && src[j] == '.' {
		j++
		start := j
		for j < len(src) && isDigit(rune(src[j])) {
			j++
		}
		if j == start {
			return 0, 0, bad
		}
		kind = tokFloat
	}
	if j < len(src) && (src[j] == 'e' || src[j] == 'E') {
		j++
		if j < len(src) && (src[j] == '+' || src[j] == '-') {
			j++
		}
		start := j
		for j < len(src) && isDigit(rune(src[j])) {
			j++
		}
		if j == start {
			return 0, 0, bad
		}
		kind = tokFloat
	}
	if j < len(src) && (src[j] == '.' || isNameContinue(rune(src[j]))) {
		return 0, 0, bad
	}
	return kind, j, nil
}

func lexString(src string, i int) (int, error) {
	j := i + 1
	for j < len(src) {
		r, size := utf8.DecodeRuneInString(src[j:])
		switch {
		case r == '"':
			return j + 1, nil
		case r == '\n' || r == '\r':
			return 0, fmt.Errorf("graphql: unterminated string at %d", i)
		case badSourceRune(r, size):
			return 0, fmt.Errorf("graphql: invalid character in string at %d", j)
		case r == '\\':
			j++
			if j >= len(src) {
				return 0, fmt.Errorf("graphql: unterminated string at %d", i)
			}
			switch src[j] {
			case '"', '\\', '/', 'b', 'f', 'n', 'r', 't':
				j++
			case 'u':
				j++
				if j < len(src) && src[j] == '{' {
					k := j + 1
					for k < len(src) && isHex(src[k]) {
						k++
					}
					if k == j+1 || k >= len(src) || src[k] != '}' {
						return 0, fmt.Errorf("graphql: bad unicode escape at %d", j)
					}
					j = k + 1
				} else {
					if j+4 > len(src) {
						return 0, fmt.Errorf("graphql: bad unicode escape at %d", j)
					}
					for k := j; k < j+4; k++ {
						if !isHex(src[k]) {
							return 0, fmt.Errorf("graphql: bad unicode escape at %d", j)
						}
					}
					j += 4
				}
			default:
				return 0, fmt.Errorf("graphql: bad escape at %d", j)
			}
			continue
		}
		j += size
	}
	return 0, fmt.Errorf("graphql: unterminated string at %d", i)
}

func lexBlockString(src string, i int) (int, error) {
	j := i + 3
	for j < len(src) {
		if strings.HasPrefix(src[j:], `\"""`) {
			j += 4
			continue
		}
		if strings.HasPrefix(src[j:], `"""`) {
			return j + 3, nil
		}
		r, size := utf8.DecodeRuneInString(src[j:])
		if r != '\n' && r != '\r' && badSourceRune(r, size) {
			return 0, fmt.Errorf("graphql: invalid character in block string at %d", j)
		}
		j += size
	}
	return 0, fmt.Errorf("graphql: unterminated block string at %d", i)
}
