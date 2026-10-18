package main

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/Sriram-PR/ghv/internal/vault"
)

func init() {
	commands = append(commands, command{name: "rotate", help: "replace the write token, the passphrase, or both", run: runRotate})
}

func runRotate(ctx context.Context, e env) error {
	tty, err := e.openTTY()
	if err != nil {
		return err
	}
	defer tty.Close()
	path := vault.Path(e.home)
	data, err := vault.Load(path)
	if err != nil {
		return err
	}
	pass, err := tty.ReadSecret("Passphrase: ")
	if err != nil {
		return err
	}
	defer clear(pass)
	s, err := vault.Open(data, pass)
	if err != nil {
		return err
	}
	oldToken := s.Token
	defer clear(oldToken)
	tty.Printf("The vault holds a write token for %s.\n", s.Login)

	ans, err := tty.ReadLine("Replace the [t]oken, the [p]assphrase, or [b]oth? ")
	if err != nil {
		return err
	}
	ans = strings.ToLower(strings.TrimSpace(ans))
	if ans != "t" && ans != "p" && ans != "b" {
		return errors.New("answer t, p, or b")
	}
	if ans != "p" {
		tok, id, err := readToken(ctx, e, tty)
		if err != nil {
			return err
		}
		defer clear(tok)
		if id.Login != s.Login {
			return fmt.Errorf("the new token belongs to %s, but the vault is for %s; to switch accounts, remove %s and run ghv-apply init", id.Login, s.Login, path)
		}
		s.Token, s.Expires, s.Created = tok, id.Expires, e.now().UTC()
	}
	newPass := pass
	if ans != "t" {
		if newPass, err = choosePassphrase(e, tty); err != nil {
			return err
		}
		defer clear(newPass)
	}
	out, err := vault.Seal(s, newPass, e.rand)
	if err != nil {
		return err
	}
	if err := vault.Replace(path, out); err != nil {
		return err
	}
	tty.Printf("Saved the vault to %s.\n", path)
	warnExpiry(e, s)
	return nil
}
