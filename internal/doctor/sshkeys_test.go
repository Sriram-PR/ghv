package doctor

import (
	"context"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/pem"
	"path/filepath"
	"strings"
	"testing"
)

// Made with ssh-keygen -m PEM and stored as DER, so no private key block
// sits in the repository; the fingerprints are ssh-keygen -l's.
const (
	rsaDER = "3082025d02010002818100c22f3a0bb9a52e929f9ef10733a2f2a4b86656542c832cec0de3b1f21f28e3ae6dd1f10ab22c48b9f4b4429a37cc0bdd497f191fe01c8d1363c8e6fc76d6ac671b392235a84666a1706c3069c806ca1fd6a84b9cf8053bbb7b0340134bbd30f61dcdb171e4ed6370b5534473984608ed4e174ebc03aa664d5b68fc7bf54ff69502030100010281800a5c8cc35d6794019ce4d545a61ab6c9d55118a02b33e0acc3dcf77ccceed89f511ad7bad3610b8c68a349be82e5af68626896c59da6c85c5581bdd990d9c99f2ae45f609b3e46227a06cd84c7dc4d3c27a11dbb629304371fb935e4708271fbe09ddf74cf06b8f3672ca7eaca9fe689bbb3c6b69393332104c2b4c6cfde2ccd024100f88d03fb88db6a8c5a5db8ece43e7d8631b1c75e0d31a46ce0175084475d26cab871735cc36125db7e92aef8c97c86c3a373e9cc85624d0f2722deca32e2262f024100c801170def2f25e6a978de2710a6c3995c8af37b4a1c5f70deaf7efe7ad48c033c3caf805788dff197162790bad316235a70da1ca3da3d190619ef0643c8c27b024100c4c77035bebe6876c23c9d61ffba71cac2a14b846cd1b6a9f428c13858ffa8ecaa6637bd637a106cdca2e92052f4e20515ed7956d0eb68b2ec7b3103c8913f45024100b9f81981c1676bac80e47c5c82a52db70a4fc17a603ce599c3d61715750cf16b88b53cd2ef91bbf99dece1b9379e533568410aee1a8530ce75d7e36345e004c502402ce29ec2962930bd1f56a079cfc4d2a6fd7f373695fdfec7dcb596b626b09e6836395929638f45350dc7ec8a8ff8dda4b958c3492d8ba4ac1510e6e532729bf3"
	rsaFP  = "SHA256:9KMNI0rqvl3WCzoB5HPtohpDVlw5hbBx//Yn90u2CLQ"
	ecDER  = "307702010104207ac29e3e1dd8637464560fb7125fa3c119bda382fd939b644420efb41bb5168ca00a06082a8648ce3d030107a14403420004e77c5e820cc4a09f8ab82cc05525b586cc980d025c308e89b8974cbef027901befd142133ff8952c600292cb32d6fd729b051450fc075d0de0e1de57f6285154"
	ecFP   = "SHA256:6D/WbyUxbttT2vRESYIFTxK3n3ZSImQ7TXq/JDXdHz0"
)

func sshStr(b []byte) []byte {
	return append(binary.BigEndian.AppendUint32(nil, uint32(len(b))), b...)
}

func ed25519Blob(seed byte) []byte {
	pub := ed25519.NewKeyFromSeed([]byte(strings.Repeat(string(rune(seed)), 32))).Public().(ed25519.PublicKey)
	return append(sshStr([]byte("ssh-ed25519")), sshStr(pub)...)
}

func fingerprint(blob []byte) string {
	sum := sha256.Sum256(blob)
	return "SHA256:" + base64.RawStdEncoding.EncodeToString(sum[:])
}

// opensshKey is an openssh-key-v1 file holding the public blob; the
// private section is never read, so it is left empty.
func opensshKey(cipher string, blob []byte) string {
	b := []byte("openssh-key-v1\x00")
	b = append(b, sshStr([]byte(cipher))...)
	b = append(b, sshStr([]byte("none"))...)
	b = append(b, sshStr(nil)...)
	b = binary.BigEndian.AppendUint32(b, 1)
	b = append(b, sshStr(blob)...)
	b = append(b, sshStr(nil)...)
	return string(pem.EncodeToMemory(&pem.Block{Type: "OPENSSH PRIVATE KEY", Bytes: b}))
}

func pemKey(typ, derHex string, headers map[string]string) string {
	der, _ := hex.DecodeString(derHex)
	return string(pem.EncodeToMemory(&pem.Block{Type: typ, Headers: headers, Bytes: der}))
}

func TestSSHKeys(t *testing.T) {
	home, human := t.TempDir(), t.TempDir()
	dss := append(sshStr([]byte("ssh-dss")), sshStr([]byte("p"))...)
	sk := append(sshStr([]byte("sk-ssh-ed25519@openssh.com")), sshStr([]byte("k"))...)
	plant(t, home, map[string]string{
		".ssh/id_ed25519":     opensshKey("none", ed25519Blob(1)),
		".ssh/id_ed25519.pub": "ssh-ed25519 " + base64.StdEncoding.EncodeToString(ed25519Blob(1)) + " me\n",
		".ssh/id_locked":      opensshKey("aes256-ctr", ed25519Blob(2)),
		".ssh/legacy_rsa":     pemKey("RSA PRIVATE KEY", rsaDER, nil),
		".ssh/rsa_locked":     pemKey("RSA PRIVATE KEY", rsaDER, map[string]string{"Proc-Type": "4,ENCRYPTED", "DEK-Info": "AES-128-CBC,00"}),
		".ssh/ec":             pemKey("EC PRIVATE KEY", ecDER, nil),
		".ssh/pkcs8_locked":   pemKey("ENCRYPTED PRIVATE KEY", ecDER, nil),
		".ssh/old_dsa":        pemKey("DSA PRIVATE KEY", "3000", nil),
		".ssh/old_dsa.pub":    "ssh-dss " + base64.StdEncoding.EncodeToString(dss) + "\n",
		".ssh/broken_dsa":     pemKey("DSA PRIVATE KEY", "3000", nil),
		".ssh/sk":             opensshKey("none", sk),
		".ssh/known_hosts":    "github.com ssh-ed25519 AAAA\n",
		".ssh/notes":          "not a key\n",
		".ssh/config":         "Host *\n  IdentityFile ~/keys/work\n  identityfile=%d/keys/work\n  IdentityFile \"~/keys/missing\"\n  IdentityFile relative/key\n",
		"keys/work":           opensshKey("none", ed25519Blob(3)),
	})
	plant(t, human, map[string]string{".ssh/id_ed25519": opensshKey("none", ed25519Blob(4))})
	p := func(h, name string) string { return filepath.Join(h, name) }
	want := "HIGH " + p(home, ".ssh/broken_dsa") + " has no passphrase (DSA PRIVATE KEY, no public key to fingerprint)\n" +
		"HIGH " + p(home, ".ssh/ec") + " has no passphrase (ecdsa-sha2-nistp256 " + ecFP + ")\n" +
		"HIGH " + p(home, ".ssh/id_ed25519") + " has no passphrase (ssh-ed25519 " + fingerprint(ed25519Blob(1)) + ")\n" +
		"HIGH " + p(home, ".ssh/legacy_rsa") + " has no passphrase (ssh-rsa " + rsaFP + ")\n" +
		"HIGH " + p(home, ".ssh/old_dsa") + " has no passphrase (ssh-dss " + fingerprint(dss) + ")\n" +
		"MEDIUM " + p(home, ".ssh/sk") + " has no passphrase (sk-ssh-ed25519@openssh.com " + fingerprint(sk) + "); using it needs its security key\n" +
		"HIGH " + p(home, "keys/work") + " has no passphrase (ssh-ed25519 " + fingerprint(ed25519Blob(3)) + ")\n" +
		"HIGH " + p(human, ".ssh/id_ed25519") + " has no passphrase (ssh-ed25519 " + fingerprint(ed25519Blob(4)) + ")\n"
	fs := sshKeys(context.Background(), &Env{Home: home, Human: &Human{Home: human}})
	if got := details(fs); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
	if fs[0].Fix != "add a passphrase: ssh-keygen -p -f "+p(home, ".ssh/broken_dsa")+"; or delete the key, and remove it at https://github.com/settings/keys if it is there" {
		t.Fatalf("fix: %s", fs[0].Fix)
	}
}
