package vault

import (
	"bytes"
	"crypto/rand"
	"errors"
	"strings"
	"testing"
	"testing/iotest"
)

func TestWordlist(t *testing.T) {
	if len(words) != 7776 || words[0] != "abacus" || words[7775] != "zoom" {
		t.Fatalf("%d words, first %q, last %q", len(words), words[0], words[len(words)-1])
	}
	seen := map[string]bool{}
	for _, w := range words {
		if seen[w] || w == "" || strings.Trim(w, "abcdefghijklmnopqrstuvwxyz-") != "" {
			t.Fatalf("bad or repeated word %q", w)
		}
		seen[w] = true
	}
}

func TestDiceware(t *testing.T) {
	got, err := Diceware(bytes.NewReader(make([]byte, 64)), 6)
	if err != nil || string(got) != "abacus abacus abacus abacus abacus abacus" {
		t.Fatalf("zero reader: %q, %v", got, err)
	}
	p, err := Diceware(rand.Reader, 6)
	if err != nil {
		t.Fatal(err)
	}
	ws := strings.Split(string(p), " ")
	if len(ws) != 6 {
		t.Fatalf("%q has %d words", p, len(ws))
	}
	for _, w := range ws {
		if !inWordlist(w) {
			t.Fatalf("%q is not in the wordlist", w)
		}
	}
	if _, err := Diceware(iotest.ErrReader(errors.New("no entropy")), 6); err == nil {
		t.Fatal("no error from a failing reader")
	}
}

func inWordlist(w string) bool {
	for _, x := range words {
		if x == w {
			return true
		}
	}
	return false
}

func TestCheckPassphrase(t *testing.T) {
	for p, ok := range map[string]bool{
		"nineteen characters":      false,
		"twenty  characters ok":    true,
		"ééééééééééééééééééé":      false,
		"éééééééééééééééééééé":     true,
		"\xff23456789012345678901": false,
	} {
		if err := CheckPassphrase([]byte(p)); (err == nil) != ok {
			t.Errorf("%q: %v", p, err)
		}
	}
}
