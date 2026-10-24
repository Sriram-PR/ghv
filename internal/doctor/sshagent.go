package doctor

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"time"
)

func init() { register("ssh-agent", sshAgents) }

const (
	agentTimeout = 2 * time.Second
	agentLimit   = 256 << 10
)

func sshAgents(ctx context.Context, e *Env) []Finding {
	var out []Finding
	for _, sock := range agentSockets(e) {
		blobs, err := agentIdentities(sock)
		if err != nil {
			continue
		}
		if len(blobs) == 0 {
			out = append(out, Finding{Severity: Info, Detail: fmt.Sprintf("ssh-agent at %s is reachable and holds no keys now; keys added later will be reachable too", sock)})
			continue
		}
		var keys []string
		for _, b := range blobs {
			keys = append(keys, sshKey{blob: b}.describe())
		}
		f := Finding{Severity: High,
			Detail: fmt.Sprintf("ssh-agent at %s holds %s this user can use without a passphrase: %s%s", sock, plural(len(blobs), "key", "keys"), list(keys), e.countOnGitHub(ctx, blobs)),
			Fix:    fmt.Sprintf("load keys with ssh-add -c so each use asks you, or remove them: SSH_AUTH_SOCK=%s ssh-add -D; or run the agent as its own user", sock)}
		switch {
		case e.Confirm == nil:
			f.Detail += "; not asked whether they were added with ssh-add -c"
		default:
			yes, err := e.Confirm(fmt.Sprintf("ssh-agent at %s holds %s. Were they all added with ssh-add -c, so each use asks you first? Type yes if so: ", sock, plural(len(blobs), "key", "keys")))
			if err != nil {
				f.Detail += "; cannot ask whether they were added with ssh-add -c: " + err.Error()
			} else if yes {
				f.Severity = Medium
				f.Detail += "; you said each use asks you first (ssh-add -c)"
			}
		}
		out = append(out, f)
	}
	return out
}

// agentSockets are the ssh-agent sockets this process might reach: its
// SSH_AUTH_SOCK, those of other processes, and where common agents put
// theirs. Each socket is listed once.
func agentSockets(e *Env) []string {
	var cands []string
	if e.Getenv != nil {
		cands = append(cands, e.Getenv("SSH_AUTH_SOCK"))
	}
	if e.Proc != "" {
		ents, _ := os.ReadDir(e.Proc)
		for _, ent := range ents {
			if pid, err := strconv.Atoi(ent.Name()); err == nil && pid != e.PID && pid != e.EnvPID {
				if vars, err := Environ(e.Proc, pid); err == nil {
					cands = append(cands, vars["SSH_AUTH_SOCK"])
				}
			}
		}
	}
	if e.Runtime != "" {
		uids := []int{e.UID}
		if e.Human != nil && e.Human.UID != e.UID {
			uids = append(uids, e.Human.UID)
		}
		for _, uid := range uids {
			for _, n := range []string{"keyring/ssh", "gcr/ssh", "openssh_agent", "ssh-agent.socket", "ssh-agent/socket", "gnupg/S.gpg-agent.ssh"} {
				cands = append(cands, filepath.Join(e.Runtime, strconv.Itoa(uid), n))
			}
		}
	}
	if e.Tmp != "" {
		for _, pat := range []string{"ssh-*/agent.*", "com.apple.launchd.*/Listeners"} {
			m, _ := filepath.Glob(filepath.Join(e.Tmp, pat))
			cands = append(cands, m...)
		}
	}
	for _, home := range e.homes() {
		for _, n := range []string{".1password/agent.sock", ".bitwarden-ssh-agent.sock", ".gnupg/S.gpg-agent.ssh", "Library/Group Containers/2BUA8C4S2C.com.1password/t/agent.sock"} {
			cands = append(cands, filepath.Join(home, n))
		}
	}
	var out []string
	var seen []os.FileInfo
	for _, p := range cands {
		fi, err := os.Stat(p)
		if !filepath.IsAbs(p) || err != nil || fi.Mode()&os.ModeSocket == 0 ||
			slices.ContainsFunc(seen, func(s os.FileInfo) bool { return os.SameFile(s, fi) }) {
			continue
		}
		seen = append(seen, fi)
		out = append(out, p)
	}
	return out
}

// agentIdentities asks the agent at sock for its public keys
// (SSH_AGENTC_REQUEST_IDENTITIES, draft-miller-ssh-agent §3.5).
func agentIdentities(sock string) ([][]byte, error) {
	c, err := net.DialTimeout("unix", sock, agentTimeout)
	if err != nil {
		return nil, err
	}
	defer c.Close()
	c.SetDeadline(time.Now().Add(agentTimeout))
	if _, err := c.Write([]byte{0, 0, 0, 1, 11}); err != nil {
		return nil, err
	}
	var hdr [4]byte
	if _, err := io.ReadFull(c, hdr[:]); err != nil {
		return nil, err
	}
	n := binary.BigEndian.Uint32(hdr[:])
	if n < 5 || n > agentLimit {
		return nil, fmt.Errorf("an answer of %d bytes", n)
	}
	body := make([]byte, n)
	if _, err := io.ReadFull(c, body); err != nil {
		return nil, err
	}
	if body[0] != 12 {
		return nil, fmt.Errorf("answer type %d", body[0])
	}
	count, rest := binary.BigEndian.Uint32(body[1:5]), body[5:]
	var blobs [][]byte
	for range count {
		blob, r, ok := sshString(rest)
		if !ok {
			return nil, errors.New("truncated answer")
		}
		if _, rest, ok = sshString(r); !ok {
			return nil, errors.New("truncated answer")
		}
		blobs = append(blobs, blob)
	}
	return blobs, nil
}
