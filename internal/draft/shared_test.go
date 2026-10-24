package draft

import (
	"io/fs"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
)

// sharedLayout makes a shared queue as setup does, owned by this test's
// uid and primary group in place of root and G.
func sharedLayout(t *testing.T) string {
	t.Helper()
	root := filepath.Join(t.TempDir(), "ghv")
	if err := os.Mkdir(root, 0o755); err != nil {
		t.Fatal(err)
	}
	for _, s := range []string{"queue", "unknown", "applied", "rejected"} {
		p := filepath.Join(root, s)
		if err := os.Mkdir(p, 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.Chmod(p, fs.ModeSetgid|0o770); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "lock"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(root, "lock"), 0o660); err != nil {
		t.Fatal(err)
	}
	return root
}

func testGroup(t *testing.T) string {
	t.Helper()
	gid := strconv.Itoa(os.Getgid())
	if g, err := user.LookupGroupId(gid); err == nil {
		return g.Name
	}
	return gid
}

func TestLocateHomeQueue(t *testing.T) {
	home := t.TempDir()
	for _, shared := range []string{"", filepath.Join(t.TempDir(), "absent")} {
		q, err := locate(shared, home, os.Getuid())
		if err != nil || q.Shared() || q.Root != filepath.Join(home, ".local", "share", "ghv") {
			t.Fatalf("shared %q: %+v, %v", shared, q, err)
		}
	}
}

func TestLocateSharedQueue(t *testing.T) {
	root := sharedLayout(t)
	q, err := locate(root, t.TempDir(), os.Getuid())
	if err != nil || !q.Shared() || q.Root != root || q.Group() != testGroup(t) {
		t.Fatalf("%+v, %v; group %q", q, err, q.Group())
	}
	if _, err := Locate(root, t.TempDir()); err == nil || !strings.Contains(err.Error(), "belongs to uid "+strconv.Itoa(os.Getuid())+", not root") {
		t.Fatalf("Locate, which wants root: %v", err)
	}
}

func TestLocateRefusesAnUnsafeSharedQueue(t *testing.T) {
	uid := strconv.Itoa(os.Getuid())
	g := testGroup(t)
	for _, c := range []struct {
		name   string
		damage func(root string) error
		want   string
	}{
		{"root is a file", func(r string) error {
			if err := os.RemoveAll(r); err != nil {
				return err
			}
			return os.WriteFile(r, nil, 0o644)
		}, "ROOT is not a directory"},
		{"root group-writable", func(r string) error { return os.Chmod(r, 0o775) },
			"ROOT can be changed by users other than root; sudo chmod 755 ROOT"},
		{"queue missing", func(r string) error { return os.Remove(filepath.Join(r, "queue")) },
			"ROOT/queue is missing; make it as docs/containment.md says"},
		{"applied missing", func(r string) error { return os.Remove(filepath.Join(r, "applied")) },
			"ROOT/applied is missing; sudo install -d -o root -g " + g + " -m 2770 ROOT/applied"},
		{"unknown a symlink", func(r string) error {
			if err := os.Remove(filepath.Join(r, "unknown")); err != nil {
				return err
			}
			return os.Symlink(filepath.Join(r, "queue"), filepath.Join(r, "unknown"))
		}, "ROOT/unknown is not a directory (a symlink is refused)"},
		{"rejected without setgid", func(r string) error { return os.Chmod(filepath.Join(r, "rejected"), 0o770) },
			"ROOT/rejected has mode 0770, not 2770; sudo chmod 2770 ROOT/rejected"},
		{"queue world-readable", func(r string) error { return os.Chmod(filepath.Join(r, "queue"), fs.ModeSetgid|0o775) },
			"ROOT/queue has mode 2775, not 2770; sudo chmod 2770 ROOT/queue"},
		{"lock missing", func(r string) error { return os.Remove(filepath.Join(r, "lock")) },
			"ROOT/lock is missing; sudo install -o root -g " + g + " -m 660 /dev/null ROOT/lock"},
		{"lock a symlink", func(r string) error {
			if err := os.Remove(filepath.Join(r, "lock")); err != nil {
				return err
			}
			return os.Symlink("/dev/null", filepath.Join(r, "lock"))
		}, "ROOT/lock is not a regular file (a symlink is refused)"},
		{"lock mode", func(r string) error { return os.Chmod(filepath.Join(r, "lock"), 0o666) },
			"ROOT/lock has mode 0666, not 0660; sudo chmod 660 ROOT/lock"},
	} {
		root := sharedLayout(t)
		if err := c.damage(root); err != nil {
			t.Fatal(err)
		}
		want := "the shared queue at ROOT is not safe to use: " + c.want
		want = strings.ReplaceAll(want, "ROOT", root)
		if _, err := locate(root, t.TempDir(), os.Getuid()); err == nil || err.Error() != want {
			t.Errorf("%s:\n got %v\nwant %s", c.name, err, want)
		}
	}
	root := sharedLayout(t)
	if _, err := locate(root, t.TempDir(), os.Getuid()+1); err == nil ||
		err.Error() != "the shared queue at "+root+" is not safe to use: "+root+" belongs to uid "+uid+", not root; sudo chown root "+root {
		t.Errorf("owner: %v", err)
	}
}

func sharedQueue(t *testing.T) Queue {
	t.Helper()
	q, err := locate(sharedLayout(t), t.TempDir(), os.Getuid())
	if err != nil {
		t.Fatal(err)
	}
	return q
}

// fullMode is a path's permission, setuid, setgid, and sticky bits.
func fullMode(t *testing.T, p string) uint32 {
	t.Helper()
	st, err := lstat(p)
	if err != nil {
		t.Fatal(err)
	}
	return st.perm
}

func TestSharedQueueFilesAreGroupReadable(t *testing.T) {
	old := syscall.Umask(0o077)
	defer syscall.Umask(old)
	q := sharedQueue(t)
	id, err := q.Write(golden["comment"])
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(q.Dir(Pending), id+".json")
	if m := fullMode(t, p); m != 0o640 {
		t.Fatalf("draft mode %04o, want 0640", m)
	}
	if err := q.Move(id, Pending, Rejected, sumOf(t, q, id, Pending), Outcome{Reason: "no"}); err != nil {
		t.Fatal(err)
	}
	if m := fullMode(t, filepath.Join(q.Dir(Rejected), id+".outcome.json")); m != 0o640 {
		t.Fatalf("outcome mode %04o, want 0640", m)
	}
	for _, s := range sharedStates {
		if m := fullMode(t, filepath.Join(q.Root, s)); m != 0o2770 {
			t.Errorf("%s changed to %04o", s, m)
		}
	}
	l, err := q.Lock()
	if err != nil {
		t.Fatal(err)
	}
	l.Release()
	if m := fullMode(t, filepath.Join(q.Root, "lock")); m != 0o660 {
		t.Errorf("lock changed to %04o", m)
	}
}

func TestSharedQueuePermissionHint(t *testing.T) {
	q := sharedQueue(t)
	u, err := user.Current()
	if err != nil {
		t.Fatal(err)
	}
	want := "cannot write to the shared queue at " + q.Root + ": permission denied; is " + u.Username + " in group " + testGroup(t) + "? Log out and in after adding them"
	if err := os.Chmod(q.Dir(Pending), fs.ModeSetgid|0o550); err != nil {
		t.Fatal(err)
	}
	defer os.Chmod(q.Dir(Pending), fs.ModeSetgid|0o770)
	if _, err := q.Write(golden["comment"]); err == nil || err.Error() != want {
		t.Errorf("Write: %v", err)
	}
	if err := os.Chmod(filepath.Join(q.Root, "lock"), 0o440); err != nil {
		t.Fatal(err)
	}
	if _, err := q.Lock(); err == nil || err.Error() != want {
		t.Errorf("Lock: %v", err)
	}
}
