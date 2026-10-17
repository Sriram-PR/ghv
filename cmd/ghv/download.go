package main

import (
	"encoding/hex"
	"errors"
	"path/filepath"
	"strings"
)

var errDigest = errors.New("does not match its digest")

// safeName reports whether name is a single file name, safe to create
// inside a directory.
func safeName(name string) bool {
	return filepath.IsLocal(name) && filepath.Base(name) == name && !strings.Contains(name, `\`)
}

// checkDigest compares sum with GitHub's "sha256:<hex>" digest. Older
// uploads have no digest and are not checked.
func checkDigest(digest string, sum []byte) error {
	want, ok := strings.CutPrefix(digest, "sha256:")
	if ok && !strings.EqualFold(want, hex.EncodeToString(sum)) {
		return errDigest
	}
	return nil
}
