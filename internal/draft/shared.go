package draft

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/user"
	"path/filepath"
	"strconv"
	"syscall"
)

// SharedRoot is where both binaries look for the shared queue, which lets
// an agent running as its own user hand drafts to the human (spec §6.3).
const SharedRoot = "/var/lib/ghv"

var sharedStates = []string{"queue", "unknown", "applied", "rejected"}

// Locate picks the queue: the shared queue when anything is at shared,
// if it passes every check, else the home queue under home. It never
// falls back to the home queue from a shared queue it refuses, where
// drafts would vanish from view. An empty shared means none.
func Locate(shared, home string) (Queue, error) { return locate(shared, home, 0) }

// locate is Locate with the uid that must own the shared queue, so tests
// can stand in for root.
func locate(shared, home string, owner int) (Queue, error) {
	if shared != "" {
		_, err := os.Lstat(shared)
		switch {
		case err == nil:
			gid, err := checkShared(shared, owner)
			if err != nil {
				return Queue{}, fmt.Errorf("the shared queue at %s is not safe to use: %w", shared, err)
			}
			return Queue{Root: shared, shared: true, gid: gid}, nil
		case !errors.Is(err, fs.ErrNotExist):
			return Queue{}, fmt.Errorf("the shared queue at %s: %w", shared, err)
		}
	}
	return Queue{Root: filepath.Join(home, ".local", "share", "ghv")}, nil
}

// Shared reports whether q is the shared queue.
func (q Queue) Shared() bool { return q.shared }

// Group names the shared queue's group, or "" for the home queue.
func (q Queue) Group() string {
	if !q.shared {
		return ""
	}
	return groupName(q.gid)
}

func groupName(gid uint32) string {
	id := strconv.FormatUint(uint64(gid), 10)
	if g, err := user.LookupGroupId(id); err == nil {
		return g.Name
	}
	return id
}

// checkShared checks the layout setup makes and returns its group: root
// owned by owner and writable by no one else, so neither user can replace
// what is inside; four state directories owned by owner, mode 2770, and
// one group; a lock file owned by owner, mode 660, in that group.
func checkShared(root string, owner int) (uint32, error) {
	st, err := lstat(root)
	if err != nil {
		return 0, err
	}
	switch {
	case !st.mode.IsDir():
		return 0, fmt.Errorf("%s is not a directory", root)
	case st.uid != uint32(owner):
		return 0, fmt.Errorf("%s belongs to uid %d, not root; sudo chown root:root %s", root, st.uid, root)
	case st.perm&0o022 != 0:
		return 0, fmt.Errorf("%s can be changed by users other than root; sudo chmod 755 %s", root, root)
	}
	first := filepath.Join(root, sharedStates[0])
	g, err := lstat(first)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, fmt.Errorf("%s is missing; make it as docs/containment.md says", first)
	}
	if err != nil {
		return 0, err
	}
	gid := g.gid
	for _, s := range sharedStates {
		p := filepath.Join(root, s)
		st, err := lstat(p)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			return 0, fmt.Errorf("%s is missing; sudo install -d -o root -g %s -m 2770 %s", p, groupName(gid), p)
		case err != nil:
			return 0, err
		case !st.mode.IsDir():
			return 0, fmt.Errorf("%s is not a directory (a symlink is refused)", p)
		case st.uid != uint32(owner):
			return 0, fmt.Errorf("%s belongs to uid %d, not root; sudo chown root %s", p, st.uid, p)
		case st.gid != gid:
			return 0, fmt.Errorf("%s has group %s, not %s like %s; sudo chgrp %s %s", p, groupName(st.gid), groupName(gid), first, groupName(gid), p)
		case st.perm != 0o2770:
			return 0, fmt.Errorf("%s has mode %04o, not 2770; sudo chmod 2770 %s", p, st.perm, p)
		}
	}
	lock := filepath.Join(root, "lock")
	st, err = lstat(lock)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return 0, fmt.Errorf("%s is missing; sudo install -o root -g %s -m 660 /dev/null %s", lock, groupName(gid), lock)
	case err != nil:
		return 0, err
	case !st.mode.IsRegular():
		return 0, fmt.Errorf("%s is not a regular file (a symlink is refused)", lock)
	case st.uid != uint32(owner):
		return 0, fmt.Errorf("%s belongs to uid %d, not root; sudo chown root %s", lock, st.uid, lock)
	case st.gid != gid:
		return 0, fmt.Errorf("%s has group %s, not %s like %s; sudo chgrp %s %s", lock, groupName(st.gid), groupName(gid), first, groupName(gid), lock)
	case st.perm != 0o660:
		return 0, fmt.Errorf("%s has mode %04o, not 0660; sudo chmod 660 %s", lock, st.perm, lock)
	}
	return gid, nil
}

type ownedMode struct {
	mode     fs.FileMode
	uid, gid uint32
	perm     uint32 // the permission, setuid, setgid, and sticky bits
}

func lstat(p string) (ownedMode, error) {
	fi, err := os.Lstat(p)
	if err != nil {
		return ownedMode{}, err
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok {
		return ownedMode{}, fmt.Errorf("%s: no owner information", p)
	}
	return ownedMode{fi.Mode(), st.Uid, st.Gid, uint32(st.Mode) & 0o7777}, nil
}
