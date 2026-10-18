// Package vault keeps ghv-apply's write token encrypted at rest under a
// passphrase (Argon2id, AES-256-GCM).
package vault

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"regexp"
	"time"

	"golang.org/x/crypto/argon2"
)

const (
	magic       = "GHV1"
	kdfArgon2id = 1
	argonMemory = 262144 // KiB
	argonTime   = 3
	argonLanes  = 4
	saltLen     = 16
	nonceLen    = 12
	keyLen      = 32
	maxLogin    = 39
	fixedHeader = 4 + 1 + 4 + 4 + 1 + saltLen + nonceLen + 1
)

var (
	ErrFormat          = errors.New("not a v1 ghv vault")
	ErrWrongPassphrase = errors.New("wrong passphrase, or the vault was changed")
)

var loginRE = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,37}[A-Za-z0-9])?$`)

// Secret is a vault's content. Token is the only copy of the write token;
// Zero clears it.
type Secret struct {
	Login   string
	Token   []byte
	Type    string
	Created time.Time
	Expires *time.Time // nil: the token never expires
}

func (s Secret) Zero() { clear(s.Token) }

func (s Secret) check() error {
	switch {
	case !loginRE.MatchString(s.Login):
		return fmt.Errorf("login %q is not a GitHub login", s.Login)
	case len(s.Token) == 0 || bytes.ContainsAny(s.Token, "\n\r"):
		return errors.New("the token is empty or spans lines")
	case s.Type == "" || bytes.ContainsAny([]byte(s.Type), "\n\r"):
		return fmt.Errorf("token type %q is empty or spans lines", s.Type)
	}
	return nil
}

func deriveKey(passphrase, salt []byte) []byte {
	return argon2.IDKey(passphrase, salt, argonTime, argonMemory, argonLanes, keyLen)
}

// Seal encrypts s under passphrase with a new random salt and nonce from r.
// The header, login included, is the GCM additional data, so changing any
// header byte fails Open.
func Seal(s Secret, passphrase []byte, r io.Reader) ([]byte, error) {
	if err := s.check(); err != nil {
		return nil, err
	}
	salt := make([]byte, saltLen)
	if _, err := io.ReadFull(r, salt); err != nil {
		return nil, err
	}
	key := deriveKey(passphrase, salt)
	defer clear(key)
	return seal(s, salt, key, r)
}

func seal(s Secret, salt, key []byte, r io.Reader) ([]byte, error) {
	if err := s.check(); err != nil {
		return nil, err
	}
	nonce := make([]byte, nonceLen)
	if _, err := io.ReadFull(r, nonce); err != nil {
		return nil, err
	}
	h := make([]byte, 0, fixedHeader+len(s.Login))
	h = append(h, magic...)
	h = append(h, kdfArgon2id)
	h = binary.BigEndian.AppendUint32(h, argonMemory)
	h = binary.BigEndian.AppendUint32(h, argonTime)
	h = append(h, argonLanes)
	h = append(h, salt...)
	h = append(h, nonce...)
	h = append(h, byte(len(s.Login)))
	h = append(h, s.Login...)

	pt := encodePlaintext(s)
	defer clear(pt)
	gcm, err := newGCM(key)
	if err != nil {
		return nil, err
	}
	return gcm.Seal(h, nonce, pt, h), nil
}

// Open decrypts a vault. Header fields other than v1's are refused before
// the KDF runs, so a tampered file cannot make it use 4 TiB of memory.
func Open(file, passphrase []byte) (Secret, error) {
	h, err := parseHeader(file)
	if err != nil {
		return Secret{}, err
	}
	key := deriveKey(passphrase, h.salt)
	defer clear(key)
	return openWithKey(file, h, key)
}

type header struct {
	size        int
	salt, nonce []byte
	login       string
}

func parseHeader(file []byte) (header, error) {
	if len(file) < fixedHeader {
		return header{}, fmt.Errorf("%w: too short", ErrFormat)
	}
	if string(file[:4]) != magic {
		return header{}, fmt.Errorf("%w: bad magic", ErrFormat)
	}
	if file[4] != kdfArgon2id || binary.BigEndian.Uint32(file[5:]) != argonMemory ||
		binary.BigEndian.Uint32(file[9:]) != argonTime || file[13] != argonLanes {
		return header{}, fmt.Errorf("%w: KDF parameters are not v1's", ErrFormat)
	}
	n := int(file[fixedHeader-1])
	size := fixedHeader + n
	if n > maxLogin || len(file) < size+16 {
		return header{}, fmt.Errorf("%w: bad login length or truncated", ErrFormat)
	}
	login := string(file[fixedHeader:size])
	if !loginRE.MatchString(login) {
		return header{}, fmt.Errorf("%w: login %q", ErrFormat, login)
	}
	return header{size: size, salt: file[14:30], nonce: file[30:42], login: login}, nil
}

func openWithKey(file []byte, h header, key []byte) (Secret, error) {
	gcm, err := newGCM(key)
	if err != nil {
		return Secret{}, err
	}
	pt, err := gcm.Open(nil, h.nonce, file[h.size:], file[:h.size])
	if err != nil {
		return Secret{}, ErrWrongPassphrase
	}
	s, err := decodePlaintext(pt)
	if err != nil {
		clear(pt)
		return Secret{}, err
	}
	s.Login = h.login
	return s, nil
}

func newGCM(key []byte) (cipher.AEAD, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

// The plaintext is four lines: token, type, created, and expiry (RFC 3339,
// empty for none), so the token can stay in one []byte.
func encodePlaintext(s Secret) []byte {
	var exp string
	if s.Expires != nil {
		exp = s.Expires.UTC().Format(time.RFC3339)
	}
	b := make([]byte, 0, len(s.Token)+64)
	b = append(b, s.Token...)
	b = append(b, '\n')
	b = append(b, s.Type...)
	b = append(b, '\n')
	b = append(b, s.Created.UTC().Format(time.RFC3339)...)
	b = append(b, '\n')
	return append(b, exp...)
}

func decodePlaintext(pt []byte) (Secret, error) {
	parts := bytes.Split(pt, []byte("\n"))
	if len(parts) != 4 || len(parts[0]) == 0 {
		return Secret{}, fmt.Errorf("%w: bad plaintext", ErrFormat)
	}
	created, err := time.Parse(time.RFC3339, string(parts[2]))
	if err != nil {
		return Secret{}, fmt.Errorf("%w: created: %v", ErrFormat, err)
	}
	s := Secret{Token: parts[0], Type: string(parts[1]), Created: created}
	if len(parts[3]) > 0 {
		exp, err := time.Parse(time.RFC3339, string(parts[3]))
		if err != nil {
			return Secret{}, fmt.Errorf("%w: expiry: %v", ErrFormat, err)
		}
		s.Expires = &exp
	}
	return s, nil
}
