package doctor

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

// fakeBus serves the session bus at path: it accepts uid only, answers
// Hello after a signal, and answers SearchItems with search's paths.
func fakeBus(t *testing.T, path string, uid int, search func(attrs map[string]string) (unlocked, locked []string, errName string)) {
	t.Helper()
	os.MkdirAll(filepath.Dir(path), 0o700)
	l, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	go func() {
		for {
			c, err := l.Accept()
			if err != nil {
				return
			}
			go serveBus(t, c, uid, search)
		}
	}()
}

func serveBus(t *testing.T, c net.Conn, uid int, search func(map[string]string) ([]string, []string, string)) {
	defer c.Close()
	var line []byte
	for one := make([]byte, 1); !strings.HasSuffix(string(line), "\r\n"); line = append(line, one[0]) {
		if _, err := c.Read(one); err != nil {
			return
		}
	}
	if string(line) != fmt.Sprintf("\x00AUTH EXTERNAL %x\r\n", fmt.Sprint(uid)) {
		io.WriteString(c, "REJECTED EXTERNAL\r\n")
		return
	}
	io.WriteString(c, "OK 0123456789abcdef0123456789abcdef\r\n")
	begin := make([]byte, 7)
	if _, err := io.ReadFull(c, begin); err != nil || string(begin) != "BEGIN\r\n" {
		t.Errorf("after OK: %q", begin)
		return
	}
	for {
		m, err := readBusMsg(c)
		if err != nil {
			return
		}
		reply := busMsg{typ: msgReturn, serial: m.serial + 100, replySerial: m.serial}
		switch {
		case m.member == "Hello" && m.dest == "org.freedesktop.DBus":
			c.Write(busMsg{typ: msgSignal, serial: 1, path: "/org/freedesktop/DBus", iface: "org.freedesktop.DBus", member: "NameAcquired", sig: "s", body: (&wireBuf{}).strBody(":1.7")}.encode())
			reply.sig, reply.body = "s", (&wireBuf{}).strBody(":1.7")
		case m.member == "SearchItems" && m.dest == "org.freedesktop.secrets" && m.path == "/org/freedesktop/secrets" &&
			m.iface == "org.freedesktop.Secret.Service" && m.sig == "a{ss}" && m.flags == flagNoAutoStart:
			u, l, e := search(decodeDict(t, m.body))
			if e != "" {
				reply = busMsg{typ: msgError, serial: m.serial + 100, replySerial: m.serial, errName: e, sig: "s", body: (&wireBuf{}).strBody("no")}
				break
			}
			w := &wireBuf{}
			for _, ps := range [][]string{u, l} {
				w.array(4, func() {
					for _, p := range ps {
						w.str(p)
					}
				})
			}
			reply.sig, reply.body = "aoao", w.b
		default:
			t.Errorf("unexpected call %+v", m)
			return
		}
		c.Write(reply.encode())
	}
}

func (w *wireBuf) strBody(s string) []byte { w.str(s); return w.b }

func decodeDict(t *testing.T, body []byte) map[string]string {
	r := &wireReader{b: body}
	end := int(r.u32())
	r.align(8)
	end += r.off
	out := map[string]string{}
	for r.off < end && r.err == nil {
		r.align(8)
		k := r.str()
		out[k] = r.str()
	}
	if r.err != nil {
		t.Errorf("a{ss}: %v", r.err)
	}
	return out
}

func TestKeyring(t *testing.T) {
	run := t.TempDir()
	mine := filepath.Join(run, "1001/bus")
	fakeBus(t, mine, 1001, func(a map[string]string) ([]string, []string, string) {
		switch {
		case slices.Equal(mapKeys(a), []string{"service"}) && a["service"] == "gh:github.com":
			return []string{"/i/1"}, nil, ""
		case slices.Equal(mapKeys(a), []string{"server"}) && a["server"] == "github.com":
			return nil, []string{"/i/2", "/i/3"}, ""
		case slices.Equal(mapKeys(a), []string{"service"}) && a["service"] == "git:https://github.com":
			return []string{"/i/1"}, nil, ""
		}
		t.Errorf("search %v", a)
		return nil, nil, ""
	})
	human := filepath.Join(run, "1000/bus")
	fakeBus(t, human, 1000, func(map[string]string) ([]string, []string, string) { return []string{"/h/1"}, nil, "" })
	e := Env{GOOS: "linux", UID: 1001, Runtime: run, Human: &Human{UID: 1000, Home: t.TempDir()},
		Getenv: func(k string) string {
			return map[string]string{"DBUS_SESSION_BUS_ADDRESS": "unix:path=" + mine + ",guid=0123"}[k]
		}}
	want := "HIGH the keyring (Secret Service) on the session bus at " + mine + " holds 3 items for github.com: gh 1, git-credential-libsecret 2; 2 are locked, but asking for one can show you an unlock prompt\n"
	fs := keyring(context.Background(), &e)
	if got := details(fs); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
	if fs[0].Fix != `remove them: gh auth logout, and printf 'protocol=https\nhost=github.com\n\n' | git credential reject; or run the agent as its own user, which cannot reach your session bus` {
		t.Fatalf("fix %q", fs[0].Fix)
	}

	for name, search := range map[string]func(map[string]string) ([]string, []string, string){
		"nothing": func(map[string]string) ([]string, []string, string) { return nil, nil, "" },
		"not running": func(map[string]string) ([]string, []string, string) {
			return nil, nil, "org.freedesktop.DBus.Error.ServiceUnknown"
		},
	} {
		run := t.TempDir()
		fakeBus(t, filepath.Join(run, "1001/bus"), 1001, search)
		if fs := keyring(context.Background(), &Env{GOOS: "linux", UID: 1001, Runtime: run}); fs != nil {
			t.Errorf("%s: %v", name, fs)
		}
	}
	unlocked := t.TempDir()
	fakeBus(t, filepath.Join(unlocked, "1001/bus"), 1001, func(a map[string]string) ([]string, []string, string) {
		if a["service"] == "gh:github.com" {
			return []string{"/i/1"}, nil, ""
		}
		return nil, nil, ""
	})
	if got := details(keyring(context.Background(), &Env{GOOS: "linux", UID: 1001, Runtime: unlocked})); got !=
		"HIGH the keyring (Secret Service) on the session bus at "+filepath.Join(unlocked, "1001/bus")+" holds 1 item for github.com: gh 1; it is unlocked\n" {
		t.Fatalf("unlocked: %s", got)
	}
}

func mapKeys(m map[string]string) []string {
	var ks []string
	for k := range m {
		ks = append(ks, k)
	}
	slices.Sort(ks)
	return ks
}

func TestKeychain(t *testing.T) {
	var calls []string
	exec := func(_ context.Context, stdin, name string, args ...string) ([]byte, error) {
		calls = append(calls, name+" "+strings.Join(args, " "))
		if args[len(args)-1] == "gh:github.com" {
			return []byte("keychain: \"/Users/u/Library/Keychains/login.keychain-db\"\n"), nil
		}
		return nil, ExitError{Code: 44}
	}
	fs := keyring(context.Background(), &Env{GOOS: "darwin", Exec: exec})
	if got := details(fs); got != "HIGH the login keychain holds GitHub items from gh\n" ||
		strings.Join(calls, "; ") != "security find-generic-password -s gh:github.com; security find-internet-password -s github.com; security find-generic-password -s git:https://github.com" {
		t.Fatalf("got %q, calls %q", got, calls)
	}
	if fs[0].Fix != "remove them in Keychain Access (gh auth logout for gh's); or run the agent as its own user, with its own keychain" {
		t.Fatalf("fix %q", fs[0].Fix)
	}
	none := func(context.Context, string, string, ...string) ([]byte, error) {
		return nil, errors.New("no security")
	}
	if fs := keyring(context.Background(), &Env{GOOS: "darwin", Exec: none}); fs != nil {
		t.Fatalf("not found: %v", fs)
	}
}
