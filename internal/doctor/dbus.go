package doctor

import (
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"slices"
	"strconv"
	"strings"
	"time"
)

// A minimal D-Bus client: EXTERNAL authentication, and method calls whose
// arguments are a string dictionary, enough to search the keyring. Only
// little-endian messages are read.

const (
	busTimeout  = 5 * time.Second
	busMsgLimit = 1 << 20

	msgCall   = 1
	msgReturn = 2
	msgError  = 3
	msgSignal = 4

	// flagNoAutoStart keeps the call from starting a keyring daemon that
	// is not running.
	flagNoAutoStart = 2
)

type busMsg struct {
	typ, flags          byte
	serial, replySerial uint32
	path, iface, member string
	dest, errName, sig  string
	body                []byte
}

type wireBuf struct{ b []byte }

func (w *wireBuf) pad(n int) {
	for len(w.b)%n != 0 {
		w.b = append(w.b, 0)
	}
}

func (w *wireBuf) u32(v uint32) {
	w.pad(4)
	w.b = binary.LittleEndian.AppendUint32(w.b, v)
}

func (w *wireBuf) str(s string) {
	w.u32(uint32(len(s)))
	w.b = append(append(w.b, s...), 0)
}

func (w *wireBuf) sig(s string) { w.b = append(append(append(w.b, byte(len(s))), s...), 0) }

// array writes an array whose elements align to elemAlign; the length
// counts from the first element, after its padding.
func (w *wireBuf) array(elemAlign int, elems func()) {
	w.u32(0)
	at := len(w.b) - 4
	w.pad(elemAlign)
	start := len(w.b)
	elems()
	binary.LittleEndian.PutUint32(w.b[at:], uint32(len(w.b)-start))
}

func (m busMsg) encode() []byte {
	w := &wireBuf{}
	w.b = append(w.b, 'l', m.typ, m.flags, 1)
	w.u32(uint32(len(m.body)))
	w.u32(m.serial)
	w.array(8, func() {
		field := func(code byte, sig, val string) {
			if val == "" {
				return
			}
			w.pad(8)
			w.b = append(w.b, code)
			w.sig(sig)
			if sig == "g" {
				w.sig(val)
			} else {
				w.str(val)
			}
		}
		field(1, "o", m.path)
		field(2, "s", m.iface)
		field(3, "s", m.member)
		field(4, "s", m.errName)
		if m.replySerial != 0 {
			w.pad(8)
			w.b = append(w.b, 5)
			w.sig("u")
			w.u32(m.replySerial)
		}
		field(6, "s", m.dest)
		field(8, "g", m.sig)
	})
	w.pad(8)
	return append(w.b, m.body...)
}

// wireReader reads values aligned to their offset in the message; base is
// the offset of b's first byte.
type wireReader struct {
	b         []byte
	off, base int
	err       error
}

func (r *wireReader) need(n int) bool {
	if r.err == nil && r.off+n > len(r.b) {
		r.err = errors.New("truncated D-Bus message")
	}
	return r.err == nil
}

func (r *wireReader) align(n int) {
	for (r.base+r.off)%n != 0 {
		r.off++
	}
}

func (r *wireReader) u32() uint32 {
	r.align(4)
	if !r.need(4) {
		return 0
	}
	v := binary.LittleEndian.Uint32(r.b[r.off:])
	r.off += 4
	return v
}

func (r *wireReader) str() string {
	n := int(r.u32())
	if n < 0 || !r.need(n+1) {
		return ""
	}
	s := string(r.b[r.off : r.off+n])
	r.off += n + 1
	return s
}

func (r *wireReader) sig() string {
	if !r.need(1) {
		return ""
	}
	n := int(r.b[r.off])
	r.off++
	if !r.need(n + 1) {
		return ""
	}
	s := string(r.b[r.off : r.off+n])
	r.off += n + 1
	return s
}

func readBusMsg(rd io.Reader) (busMsg, error) {
	var fixed [16]byte
	if _, err := io.ReadFull(rd, fixed[:]); err != nil {
		return busMsg{}, err
	}
	if fixed[0] != 'l' {
		return busMsg{}, errors.New("a big-endian D-Bus message")
	}
	m := busMsg{typ: fixed[1], flags: fixed[2], serial: binary.LittleEndian.Uint32(fixed[8:])}
	bodyLen, fieldsLen := binary.LittleEndian.Uint32(fixed[4:]), binary.LittleEndian.Uint32(fixed[12:])
	if bodyLen > busMsgLimit || fieldsLen > busMsgLimit {
		return busMsg{}, errors.New("an oversized D-Bus message")
	}
	padded := (16 + int(fieldsLen) + 7) &^ 7
	rest := make([]byte, padded-16+int(bodyLen))
	if _, err := io.ReadFull(rd, rest); err != nil {
		return busMsg{}, err
	}
	r := &wireReader{b: rest[:fieldsLen], base: 16}
	for r.off < len(r.b) && r.err == nil {
		r.align(8)
		if !r.need(1) {
			break
		}
		code := r.b[r.off]
		r.off++
		switch sig := r.sig(); sig {
		case "s", "o":
			v := r.str()
			switch code {
			case 1:
				m.path = v
			case 2:
				m.iface = v
			case 3:
				m.member = v
			case 4:
				m.errName = v
			case 6:
				m.dest = v
			}
		case "g":
			if v := r.sig(); code == 8 {
				m.sig = v
			}
		case "u":
			if v := r.u32(); code == 5 {
				m.replySerial = v
			}
		default:
			return busMsg{}, fmt.Errorf("a D-Bus header field of type %q", sig)
		}
	}
	if r.err != nil {
		return busMsg{}, r.err
	}
	m.body = rest[padded-16:]
	return m, nil
}

type busConn struct {
	c      net.Conn
	serial uint32
}

// dialBus connects to the bus at path and authenticates as uid; the bus
// refuses a uid other than the connecting process's.
func dialBus(path string, uid int) (*busConn, error) {
	c, err := net.DialTimeout("unix", path, busTimeout)
	if err != nil {
		return nil, err
	}
	c.SetDeadline(time.Now().Add(busTimeout))
	b := &busConn{c: c}
	if err := b.auth(uid); err != nil {
		c.Close()
		return nil, err
	}
	if _, err := b.call("org.freedesktop.DBus", "/org/freedesktop/DBus", "org.freedesktop.DBus", "Hello", nil); err != nil {
		c.Close()
		return nil, err
	}
	return b, nil
}

func (b *busConn) Close() error { return b.c.Close() }

func (b *busConn) auth(uid int) error {
	if _, err := fmt.Fprintf(b.c, "\x00AUTH EXTERNAL %s\r\n", hex.EncodeToString([]byte(strconv.Itoa(uid)))); err != nil {
		return err
	}
	var line []byte
	one := make([]byte, 1)
	for !strings.HasSuffix(string(line), "\r\n") {
		if len(line) > 512 {
			return errors.New("an overlong D-Bus auth line")
		}
		if _, err := b.c.Read(one); err != nil {
			return err
		}
		line = append(line, one[0])
	}
	if !strings.HasPrefix(string(line), "OK ") {
		return errors.New("the bus refused EXTERNAL authentication")
	}
	_, err := io.WriteString(b.c, "BEGIN\r\n")
	return err
}

// call sends a method call with an a{ss} argument when attrs is not nil,
// and returns the reply, skipping signals and other messages.
func (b *busConn) call(dest, path, iface, member string, attrs map[string]string) (busMsg, error) {
	b.serial++
	m := busMsg{typ: msgCall, flags: flagNoAutoStart, serial: b.serial, path: path, iface: iface, member: member, dest: dest}
	if attrs != nil {
		m.sig, m.body = "a{ss}", encodeDict(attrs)
	}
	if _, err := b.c.Write(m.encode()); err != nil {
		return busMsg{}, err
	}
	for {
		r, err := readBusMsg(b.c)
		if err != nil {
			return busMsg{}, err
		}
		if (r.typ != msgReturn && r.typ != msgError) || r.replySerial != m.serial {
			continue
		}
		if r.typ == msgError {
			msg := ""
			if r.sig == "s" {
				msg = (&wireReader{b: r.body}).str()
			}
			return busMsg{}, fmt.Errorf("%s: %s", r.errName, msg)
		}
		return r, nil
	}
}

func encodeDict(attrs map[string]string) []byte {
	w := &wireBuf{}
	keys := make([]string, 0, len(attrs))
	for k := range attrs {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	w.array(8, func() {
		for _, k := range keys {
			w.pad(8)
			w.str(k)
			w.str(attrs[k])
		}
	})
	return w.b
}

// objectPathArrays reads a body of signature "aoao".
func objectPathArrays(m busMsg) (a, b []string, err error) {
	if m.sig != "aoao" {
		return nil, nil, fmt.Errorf("a reply of signature %q", m.sig)
	}
	r := &wireReader{b: m.body}
	read := func() []string {
		var out []string
		end := int(r.u32())
		end += r.off
		for r.off < end && r.err == nil {
			out = append(out, r.str())
		}
		return out
	}
	a, b = read(), read()
	return a, b, r.err
}
