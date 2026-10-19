package doctor

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

func init() {
	register("same-user", sameUser)
	register("ptrace", ptraceScope)
}

func sameUser(_ context.Context, e *Env) []Finding {
	switch {
	case e.Human == nil:
		return []Finding{{Severity: High,
			Detail: fmt.Sprintf("no --human-user given, so this counts as the human's own user, %s: the agent can do anything you can", e.User),
			Fix:    "run the agent as its own user, then run doctor there with --human-user YOUR_NAME"}}
	case e.Human.UID == e.UID:
		return []Finding{{Severity: High,
			Detail: fmt.Sprintf("runs as %s, the human's user: the agent can do anything you can", e.User),
			Fix:    "run the agent as its own user (see the README's containment guide)"}}
	}
	return nil
}

const ptraceFix = "echo 'kernel.yama.ptrace_scope = 1' | sudo tee /etc/sysctl.d/10-ptrace.conf && sudo sysctl --system"

func ptraceScope(_ context.Context, e *Env) []Finding {
	if e.GOOS != "linux" {
		return nil
	}
	b, err := os.ReadFile(filepath.Join(e.Proc, "sys/kernel/yama/ptrace_scope"))
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return []Finding{{Severity: High, Detail: "the kernel has no Yama: a process can trace any other process of its user", Fix: "use a kernel with Yama, then " + ptraceFix}}
	case err != nil:
		return []Finding{{Severity: High, Detail: "cannot read kernel.yama.ptrace_scope: " + err.Error(), Fix: ptraceFix}}
	case strings.TrimSpace(string(b)) == "0":
		return []Finding{{Severity: High, Detail: "kernel.yama.ptrace_scope is 0: a process can trace any other process of its user, ghv-apply's passphrase prompt included", Fix: ptraceFix}}
	}
	return nil
}
