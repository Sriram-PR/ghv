package doctor

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"
)

func TestQueue(t *testing.T) {
	home, unsafe := t.TempDir(), t.TempDir()
	uid := os.Getuid()
	for _, c := range []struct {
		name string
		e    *Env
		want string
	}{
		{"unsafe shared queue", &Env{UID: uid, Home: home, SharedRoot: unsafe},
			"INFO the shared queue at " + unsafe + " is not safe to use: " + unsafe + " belongs to uid " + strconv.Itoa(uid) + ", not root; sudo chown root:root " + unsafe + "; ghv and ghv-apply refuse it\n"},
		{"agent's own home", &Env{UID: uid, Home: home, SharedRoot: filepath.Join(home, "absent"), Human: &Human{Name: "sriram", UID: uid + 1}},
			"INFO drafts go to " + filepath.Join(home, ".local", "share", "ghv") + ", which sriram's ghv-apply does not read; set up the shared queue (docs/containment.md)\n"},
		{"the human's own home", &Env{UID: uid, Home: home, Human: &Human{Name: "sriram", UID: uid}}, ""},
		{"no --human-user", &Env{UID: uid, Home: home}, ""},
	} {
		if got := details(queue(context.Background(), c.e)); got != c.want {
			t.Errorf("%s:\n got %q\nwant %q", c.name, got, c.want)
		}
	}
}
