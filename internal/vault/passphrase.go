package vault

import (
	"crypto/rand"
	_ "embed"
	"errors"
	"fmt"
	"io"
	"math/big"
	"strings"
	"unicode/utf8"
)

// MinPassphrase is the shortest typed passphrase init accepts, in
// characters. Six generated words carry about 77 bits.
const MinPassphrase = 20

// eff_large_wordlist.txt is the EFF's large wordlist for dice-generated
// passphrases (https://www.eff.org/dice), licensed CC BY 3.0 US.
//
//go:embed eff_large_wordlist.txt
var wordlistFile string

var words = parseWordlist(wordlistFile)

func parseWordlist(f string) []string {
	lines := strings.Split(strings.TrimSuffix(f, "\n"), "\n")
	ws := make([]string, len(lines))
	for i, l := range lines {
		_, w, ok := strings.Cut(l, "\t")
		if !ok {
			panic(fmt.Sprintf("wordlist line %d: %q", i+1, l))
		}
		ws[i] = w
	}
	return ws
}

// Diceware returns n words chosen uniformly from the EFF large wordlist
// with randomness from r, separated by spaces.
func Diceware(r io.Reader, n int) ([]byte, error) {
	count := big.NewInt(int64(len(words)))
	var p []byte
	for i := range n {
		k, err := rand.Int(r, count)
		if err != nil {
			clear(p)
			return nil, err
		}
		if i > 0 {
			p = append(p, ' ')
		}
		p = append(p, words[k.Int64()]...)
	}
	return p, nil
}

// CheckPassphrase applies init's rule for a typed passphrase.
func CheckPassphrase(p []byte) error {
	if !utf8.Valid(p) {
		return errors.New("the passphrase is not valid UTF-8")
	}
	if n := utf8.RuneCount(p); n < MinPassphrase {
		return fmt.Errorf("a passphrase needs at least %d characters; this one has %d", MinPassphrase, n)
	}
	return nil
}
