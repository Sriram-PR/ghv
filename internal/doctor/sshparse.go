package doctor

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/binary"
	"encoding/pem"
	"errors"
	"math/big"
	"strings"
)

// sshKey is a private key file with no passphrase. blob is its public key
// in SSH wire form, nil when it could not be found.
type sshKey struct {
	path, pemType string
	blob          []byte
}

func (k sshKey) kind() string {
	if t, _, ok := sshString(k.blob); ok {
		return string(t)
	}
	return ""
}

func (k sshKey) describe() string {
	if k.blob == nil {
		return k.pemType + ", no public key to fingerprint"
	}
	return k.kind() + " " + sshFingerprint(k.blob)
}

// securityKey is a FIDO key: the file alone cannot sign.
func (k sshKey) securityKey() bool { return strings.HasPrefix(k.kind(), "sk-") }

func sshFingerprint(blob []byte) string {
	sum := sha256.Sum256(blob)
	return "SHA256:" + base64.RawStdEncoding.EncodeToString(sum[:])
}

func sshString(b []byte) (s, rest []byte, ok bool) {
	if len(b) < 4 {
		return nil, nil, false
	}
	n := binary.BigEndian.Uint32(b)
	if uint64(n) > uint64(len(b)-4) {
		return nil, nil, false
	}
	return b[4 : 4+n], b[4+n:], true
}

// unprotectedKey reads a private key file: ok is false for anything else,
// and for a key protected by a passphrase.
func unprotectedKey(path string, data []byte) (sshKey, bool) {
	block, _ := pem.Decode(data)
	if block == nil {
		return sshKey{}, false
	}
	k := sshKey{path: path, pemType: block.Type}
	switch block.Type {
	case "OPENSSH PRIVATE KEY":
		cipher, blob, err := parseOpenSSH(block.Bytes)
		if err != nil || cipher != "none" {
			return sshKey{}, false
		}
		k.blob = blob
	case "RSA PRIVATE KEY", "EC PRIVATE KEY", "DSA PRIVATE KEY":
		if strings.Contains(block.Headers["Proc-Type"], "ENCRYPTED") {
			return sshKey{}, false
		}
		k.blob = pemBlob(block)
	case "PRIVATE KEY":
		k.blob = pemBlob(block)
	default:
		return sshKey{}, false
	}
	if k.blob == nil {
		k.blob = pubFileBlob(path + ".pub")
	}
	return k, true
}

// parseOpenSSH reads the cipher and first public key of an openssh-key-v1
// file; both sit before the encrypted part.
func parseOpenSSH(b []byte) (cipher string, blob []byte, err error) {
	rest, ok := bytes.CutPrefix(b, []byte("openssh-key-v1\x00"))
	if !ok {
		return "", nil, errors.New("not an openssh-key-v1 key")
	}
	var c []byte
	if c, rest, ok = sshString(rest); !ok {
		return "", nil, errors.New("truncated")
	}
	for range 2 { // kdfname, kdfoptions
		if _, rest, ok = sshString(rest); !ok {
			return "", nil, errors.New("truncated")
		}
	}
	if len(rest) < 4 || binary.BigEndian.Uint32(rest) < 1 {
		return "", nil, errors.New("no keys")
	}
	if blob, _, ok = sshString(rest[4:]); !ok {
		return "", nil, errors.New("truncated")
	}
	return string(c), blob, nil
}

func pemBlob(block *pem.Block) []byte {
	var pub any
	switch block.Type {
	case "RSA PRIVATE KEY":
		if k, err := x509.ParsePKCS1PrivateKey(block.Bytes); err == nil {
			pub = &k.PublicKey
		}
	case "EC PRIVATE KEY":
		if k, err := x509.ParseECPrivateKey(block.Bytes); err == nil {
			pub = &k.PublicKey
		}
	case "PRIVATE KEY":
		if k, err := x509.ParsePKCS8PrivateKey(block.Bytes); err == nil {
			switch k := k.(type) {
			case *rsa.PrivateKey:
				pub = &k.PublicKey
			case *ecdsa.PrivateKey:
				pub = &k.PublicKey
			case ed25519.PrivateKey:
				pub = k.Public()
			}
		}
	}
	return wireBlob(pub)
}

// wireBlob is a public key in SSH wire form (RFC 4253 §6.6, RFC 5656 §3.1,
// RFC 8709 §4).
func wireBlob(pub any) []byte {
	str := func(b []byte) []byte { return append(binary.BigEndian.AppendUint32(nil, uint32(len(b))), b...) }
	mpint := func(n *big.Int) []byte {
		b := n.Bytes()
		if len(b) > 0 && b[0]&0x80 != 0 {
			b = append([]byte{0}, b...)
		}
		return str(b)
	}
	switch k := pub.(type) {
	case *rsa.PublicKey:
		return bytes.Join([][]byte{str([]byte("ssh-rsa")), mpint(big.NewInt(int64(k.E))), mpint(k.N)}, nil)
	case *ecdsa.PublicKey:
		curve := map[elliptic.Curve]string{elliptic.P256(): "nistp256", elliptic.P384(): "nistp384", elliptic.P521(): "nistp521"}[k.Curve]
		point, err := k.Bytes()
		if curve == "" || err != nil {
			return nil
		}
		return bytes.Join([][]byte{str([]byte("ecdsa-sha2-" + curve)), str([]byte(curve)), str(point)}, nil)
	case ed25519.PublicKey:
		return bytes.Join([][]byte{str([]byte("ssh-ed25519")), str(k)}, nil)
	}
	return nil
}

func pubFileBlob(path string) []byte {
	b, err := readCapped(path, 16<<10)
	if err != nil {
		return nil
	}
	fields := strings.Fields(string(b))
	if len(fields) < 2 {
		return nil
	}
	blob, err := base64.StdEncoding.DecodeString(fields[1])
	if err != nil {
		return nil
	}
	return blob
}
