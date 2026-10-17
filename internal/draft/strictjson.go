package draft

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"
)

const maxJSONDepth = 32

// checkStrictJSON rejects what encoding/json would quietly accept:
// duplicate keys (last one wins), null (leaves the zero value), invalid
// UTF-8 or an escaped lone surrogate (both become U+FFFD), and trailing
// data. A literal U+FFFD is refused too, since it cannot be told apart.
func checkStrictJSON(data []byte) error {
	if !utf8.Valid(data) {
		return errors.New("not valid UTF-8")
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber()
	if err := walkJSON(dec, 0); err != nil {
		return err
	}
	if _, err := dec.Token(); err != io.EOF {
		return errors.New("trailing data after the JSON value")
	}
	return nil
}

func walkJSON(dec *json.Decoder, depth int) error {
	if depth > maxJSONDepth {
		return errors.New("nested too deeply")
	}
	tok, err := dec.Token()
	if err != nil {
		return err
	}
	switch tok {
	case nil:
		return errors.New("null is not allowed")
	case json.Delim('{'):
		seen := map[string]bool{}
		for dec.More() {
			k, err := dec.Token()
			if err != nil {
				return err
			}
			key, _ := k.(string)
			if strings.ContainsRune(key, utf8.RuneError) {
				return errors.New("key contains U+FFFD")
			}
			if seen[key] {
				return fmt.Errorf("duplicate key %q", key)
			}
			seen[key] = true
			if err := walkJSON(dec, depth+1); err != nil {
				return err
			}
		}
		_, err = dec.Token()
		return err
	case json.Delim('['):
		for dec.More() {
			if err := walkJSON(dec, depth+1); err != nil {
				return err
			}
		}
		_, err = dec.Token()
		return err
	}
	if s, ok := tok.(string); ok && strings.ContainsRune(s, utf8.RuneError) {
		return errors.New("string contains U+FFFD")
	}
	return nil
}
