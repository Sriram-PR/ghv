package vault

// Login reads the pinned login from the vault's header without the
// passphrase. Nothing authenticates it until Open succeeds.
func Login(file []byte) (string, error) {
	h, err := parseHeader(file)
	return h.login, err
}
