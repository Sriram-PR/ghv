package main

import (
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"golang.org/x/term"
)

// terminal is where every passphrase and decision comes from: /dev/tty,
// never stdin, so `yes | ghv-apply` approves nothing.
type terminal interface {
	ReadSecret(prompt string) ([]byte, error)
	ReadLine(prompt string) (string, error)
	Printf(format string, a ...any)
	Close() error
}

const maxLine = 1024

type devTTY struct{ f *os.File }

func openDevTTY() (terminal, error) {
	f, err := os.OpenFile("/dev/tty", os.O_RDWR, 0)
	if err == nil && !term.IsTerminal(int(f.Fd())) {
		f.Close()
		err = errors.New("/dev/tty is not a terminal")
	}
	if err != nil {
		return nil, fmt.Errorf("no terminal: passphrases and decisions come from /dev/tty only: %v", err)
	}
	return devTTY{f}, nil
}

func (t devTTY) Close() error { return t.f.Close() }

func (t devTTY) Printf(format string, a ...any) { fmt.Fprintf(t.f, format, a...) }

// ReadSecret reads a line with echo off. An interrupt restores echo before
// exiting, so the shell is not left without it.
func (t devTTY) ReadSecret(prompt string) ([]byte, error) {
	fd := int(t.f.Fd())
	st, err := term.GetState(fd)
	if err != nil {
		return nil, err
	}
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, os.Interrupt, syscall.SIGTERM, syscall.SIGHUP)
	done := make(chan struct{})
	go func() {
		select {
		case <-sig:
			term.Restore(fd, st)
			fmt.Fprintln(t.f)
			os.Exit(130)
		case <-done:
		}
	}()
	defer func() { signal.Stop(sig); close(done) }()
	fmt.Fprint(t.f, prompt)
	b, err := term.ReadPassword(fd)
	fmt.Fprintln(t.f)
	return b, err
}

// ReadLine reads one line a byte at a time, so nothing typed after it is
// buffered here.
func (t devTTY) ReadLine(prompt string) (string, error) {
	fmt.Fprint(t.f, prompt)
	var line []byte
	b := make([]byte, 1)
	for len(line) <= maxLine {
		n, err := t.f.Read(b)
		if err != nil {
			return "", err
		}
		if n == 1 && b[0] == '\n' {
			return strings.TrimSuffix(string(line), "\r"), nil
		}
		line = append(line, b[:n]...)
	}
	return "", fmt.Errorf("line longer than %d bytes", maxLine)
}
