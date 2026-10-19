package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"unicode/utf8"

	"github.com/Sriram-PR/ghv/internal/doctor"
	"github.com/Sriram-PR/ghv/internal/read"
	"github.com/Sriram-PR/ghv/internal/sanitize"
)

func writeJSON(w io.Writer, v any) error {
	b, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	_, err = w.Write(escapeJSON(append(b, '\n')))
	return err
}

// escapeJSON \u-escapes every rune sanitize.Terminal would escape, so JSON
// output is as terminal-safe as text and still decodes to the same value.
// b must be valid JSON: there, such runes only occur inside strings, and a
// raw \r only as whitespace, which is dropped.
func escapeJSON(b []byte) []byte {
	var out bytes.Buffer
	out.Grow(len(b))
	for i := 0; i < len(b); {
		r, n := utf8.DecodeRune(b[i:])
		switch {
		case r == '\r':
		case r == '\n' || r == '\t' || (r >= 0x20 && r < 0x7f):
			out.WriteByte(byte(r))
		case r == utf8.RuneError && n == 1:
			out.WriteString("\ufffd")
		case sanitize.Terminal(string(r)) != string(r):
			if r > 0xFFFF {
				r -= 0x10000
				fmt.Fprintf(&out, `\u%04x\u%04x`, 0xD800+(r>>10), 0xDC00+(r&0x3FF))
			} else {
				fmt.Fprintf(&out, `\u%04x`, r)
			}
		default:
			out.Write(b[i : i+n])
		}
		i += n
	}
	return out.Bytes()
}

// errorCode returns the stable --json code and the exit status for err:
// 1 API error, 2 usage error, 3 token invalid, expired, missing, or
// refused, 4 doctor found something HIGH.
func errorCode(err error) (string, int) {
	var ue usageError
	var te *read.TokenError
	var ae *read.APIError
	var he doctor.HighError
	switch {
	case errors.As(err, &ue):
		return "usage", 2
	case errors.As(err, &he):
		return "doctor_high", 4
	case errors.As(err, &te):
		return "token_refused", 3
	case errors.Is(err, read.ErrTokenFile):
		return "token_file", 3
	case errors.As(err, &ae):
		if ae.Code == read.CodeTokenInvalid || ae.Code == read.CodeTokenRequired {
			return string(ae.Code), 3
		}
		return string(ae.Code), 1
	case errors.Is(err, read.ErrReadOnly):
		return "read_only", 1
	}
	return "error", 1
}

type jsonError struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func report(e env, cmd *command, err error, asJSON bool) int {
	code, exit := errorCode(err)
	msg := err.Error()
	if code == string(read.CodeTokenRequired) {
		msg += "; save a classic token with no scopes in " + e.tokenPath()
	}
	if asJSON {
		var j jsonError
		j.Error.Code, j.Error.Message = code, msg
		writeJSON(e.stderr, j)
		return exit
	}
	fmt.Fprintf(e.stderr, "ghv: %s\n", sanitize.Line(msg))
	if code == "usage" {
		help := "ghv --help"
		if cmd != nil {
			help = "ghv " + cmd.path + " --help"
		}
		fmt.Fprintf(e.stderr, "Run '%s' for usage.\n", help)
	}
	return exit
}
