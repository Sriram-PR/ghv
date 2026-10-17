package read

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"os"
	"strings"
	"syscall"
	"time"
)

var ErrTokenFile = errors.New("read token file")

type TokenKind string

const (
	TokenNone        TokenKind = "none"
	TokenClassic     TokenKind = "classic"
	TokenFineGrained TokenKind = "fine-grained"
	TokenOther       TokenKind = "other"
)

type Verdict string

const (
	VerdictAnonymous  Verdict = "anonymous"
	VerdictReadOnly   Verdict = "read-only"
	VerdictUnverified Verdict = "unverified"
	VerdictRefused    Verdict = "refused"
)

// TokenInfo is what GitHub's response headers say about the read token.
// Scopes is nil when GitHub sent no X-OAuth-Scopes header.
type TokenInfo struct {
	Kind    TokenKind  `json:"type"`
	Scopes  []string   `json:"scopes"`
	Expires *time.Time `json:"expires_at"`
	Verdict Verdict    `json:"verdict"`
}

const maxTokenFile = 4096

// LoadToken reads the read token at path. A missing file means anonymous
// reads and returns "". Only classic and fine-grained personal access
// tokens are accepted; OAuth, app, and server tokens can carry write
// permissions ghv cannot see.
func LoadToken(path string) (string, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if errors.Is(err, fs.ErrNotExist) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrTokenFile, err)
	}
	defer f.Close()
	fi, err := f.Stat()
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrTokenFile, err)
	}
	if !fi.Mode().IsRegular() {
		return "", fmt.Errorf("%w: %s is not a regular file", ErrTokenFile, path)
	}
	if fi.Mode().Perm()&0o077 != 0 {
		return "", fmt.Errorf("%w: %s is readable by other users; run chmod 600 %s", ErrTokenFile, path, path)
	}
	raw, err := io.ReadAll(io.LimitReader(f, maxTokenFile+1))
	if err != nil {
		return "", fmt.Errorf("%w: %v", ErrTokenFile, err)
	}
	if len(raw) > maxTokenFile {
		return "", fmt.Errorf("%w: %s is over %d bytes", ErrTokenFile, path, maxTokenFile)
	}
	tok := strings.TrimSpace(string(raw))
	if tok == "" {
		return "", fmt.Errorf("%w: %s is empty", ErrTokenFile, path)
	}
	for i := 0; i < len(tok); i++ {
		c := tok[i]
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '_') {
			return "", fmt.Errorf("%w: %s holds more than one token-shaped word", ErrTokenFile, path)
		}
	}
	if KindOf(tok) == TokenOther {
		return "", fmt.Errorf("%w: %s is not a personal access token (ghp_ or github_pat_)", ErrTokenFile, path)
	}
	return tok, nil
}

// KindOf tells the token type from its prefix. Classic tokens made before
// 2021 are 40 hex characters.
func KindOf(tok string) TokenKind {
	switch {
	case tok == "":
		return TokenNone
	case strings.HasPrefix(tok, "ghp_"):
		return TokenClassic
	case strings.HasPrefix(tok, "github_pat_"):
		return TokenFineGrained
	case len(tok) == 40 && strings.Trim(tok, "0123456789abcdef") == "":
		return TokenClassic
	}
	return TokenOther
}

// Inspect reads the token's scopes and expiry from an authenticated
// api.github.com response. A classic token with no scopes is GitHub's
// read-only access to public information; GitHub reports it as an empty
// X-OAuth-Scopes header. Fine-grained tokens send no such header, so their
// permissions cannot be verified.
func Inspect(kind TokenKind, h http.Header) TokenInfo {
	info := TokenInfo{Kind: kind}
	if kind == TokenNone {
		info.Verdict = VerdictAnonymous
		return info
	}
	if vals, ok := h["X-Oauth-Scopes"]; ok {
		info.Scopes = []string{}
		for _, v := range vals {
			for s := range strings.SplitSeq(v, ",") {
				if s = strings.TrimSpace(s); s != "" {
					info.Scopes = append(info.Scopes, s)
				}
			}
		}
	}
	if exp := h.Get("Github-Authentication-Token-Expiration"); exp != "" {
		for _, layout := range []string{"2006-01-02 15:04:05 MST", "2006-01-02 15:04:05 -0700"} {
			if t, err := time.Parse(layout, exp); err == nil {
				t = t.UTC()
				info.Expires = &t
				break
			}
		}
	}
	switch {
	case len(info.Scopes) > 0:
		info.Verdict = VerdictRefused
	case kind == TokenClassic && info.Scopes != nil:
		info.Verdict = VerdictReadOnly
	default:
		info.Verdict = VerdictUnverified
	}
	return info
}
