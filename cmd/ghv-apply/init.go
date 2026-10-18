package main

import (
	"bytes"
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/Sriram-PR/ghv/internal/vault"
	"github.com/Sriram-PR/ghv/internal/write"
)

func init() {
	commands = append(commands, command{name: "init", help: "store the write token in a new vault, under a passphrase", run: runInit})
}

func runInit(ctx context.Context, e env) error {
	tty, err := e.openTTY()
	if err != nil {
		return err
	}
	defer tty.Close()
	path := vault.Path(e.home)
	if _, err := os.Lstat(path); err == nil {
		return fmt.Errorf("%w at %s; use ghv-apply rotate", vault.ErrExists, path)
	}
	tok, id, err := readToken(ctx, e, tty)
	if err != nil {
		return err
	}
	defer clear(tok)
	pass, err := choosePassphrase(e, tty)
	if err != nil {
		return err
	}
	defer clear(pass)
	s := vault.Secret{Login: id.Login, Token: tok, Type: "classic", Created: e.now().UTC(), Expires: id.Expires}
	data, err := vault.Seal(s, pass, e.rand)
	if err != nil {
		return err
	}
	if err := vault.Create(path, data); err != nil {
		return err
	}
	tty.Printf("Saved the vault to %s.\n", path)
	warnExpiry(e, s)
	return nil
}

// readToken reads the write token from the terminal and checks it with
// GitHub. The returned slice is the caller's to clear.
func readToken(ctx context.Context, e env, tty terminal) ([]byte, write.Identity, error) {
	tty.Printf("Paste a classic personal access token with only the public_repo scope.\n" +
		"Clipboard history may keep a pasted token; clear it afterwards.\n")
	raw, err := tty.ReadSecret("Write token: ")
	if err != nil {
		return nil, write.Identity{}, err
	}
	tok := bytes.TrimSpace(raw)
	id, err := write.Whoami(ctx, e.client, tok)
	if err != nil {
		clear(raw)
		return nil, write.Identity{}, err
	}
	if id.Expires == nil {
		tty.Printf("The token belongs to %s and never expires.\n", id.Login)
	} else {
		tty.Printf("The token belongs to %s and expires on %s.\n", id.Login, id.Expires.Format(time.DateOnly))
	}
	return tok, id, nil
}

// choosePassphrase offers six diceware words or takes a typed passphrase,
// and asks for it again. The returned slice is the caller's to clear.
func choosePassphrase(e env, tty terminal) ([]byte, error) {
	ans, err := tty.ReadLine("Generate a passphrase? [Y/n] ")
	if err != nil {
		return nil, err
	}
	var pass []byte
	switch strings.ToLower(strings.TrimSpace(ans)) {
	case "", "y", "yes":
		if pass, err = vault.Diceware(e.rand, 6); err != nil {
			return nil, err
		}
		tty.Printf("\nYour passphrase:\n\n    %s\n\nWrite it down: nothing can open the vault without it.\n\n", pass)
	case "n", "no":
		if pass, err = tty.ReadSecret(fmt.Sprintf("Passphrase (at least %d characters): ", vault.MinPassphrase)); err != nil {
			return nil, err
		}
		if err := vault.CheckPassphrase(pass); err != nil {
			clear(pass)
			return nil, err
		}
	default:
		return nil, errors.New("answer y or n")
	}
	again, err := tty.ReadSecret("Type the passphrase again: ")
	defer clear(again)
	if err != nil || subtle.ConstantTimeCompare(pass, again) != 1 {
		clear(pass)
		if err == nil {
			err = errors.New("the passphrases do not match")
		}
		return nil, err
	}
	return pass, nil
}

func warnExpiry(e env, s vault.Secret) {
	if w := s.ExpiryWarning(e.now()); w != "" {
		fmt.Fprintf(e.stderr, "ghv-apply: warning: %s\n", w)
	}
}
