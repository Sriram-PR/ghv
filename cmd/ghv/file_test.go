package main

import (
	"encoding/base64"
	"io"
	"net/http"
	"strings"
	"testing"
)

func b64(s string) string {
	e := base64.StdEncoding.EncodeToString([]byte(s))
	return e[:10] + `\n` + e[10:]
}

func TestFile(t *testing.T) {
	rt := newRoutes(t, map[string]string{
		"/repos/o/r/contents/docs/a%20b.md?ref=v1": `{"type":"file","encoding":"base64","size":17,"name":"a b.md","path":"docs/a b.md","sha":"abc","content":"` + b64("# Title\n\x1b[2Jbody\n") + `"}`,
	})
	e, _, _ := testEnv(t, rt)
	r := runWith(t, registry, e, "file", "o/r", "docs/a b.md", "--ref", "v1")
	if r.code != 0 || r.stdout != "# Title\n<U+001B>[2Jbody\n" {
		t.Fatalf("exit %d, stderr %q, asked %q, stdout %q", r.code, r.stderr, rt.uris(), r.stdout)
	}
	r = runWith(t, registry, e, "file", "o/r", "docs/a b.md", "--ref", "v1", "--json")
	for _, s := range []string{`"type": "file"`, `"path": "docs/a b.md"`, `"ref": "v1"`, `"content": "# Title\n\u001b[2Jbody\n"`} {
		if !strings.Contains(r.stdout, s) {
			t.Fatalf("missing %s in:\n%s", s, r.stdout)
		}
	}
}

func TestFileDirectory(t *testing.T) {
	rt := newRoutes(t, map[string]string{
		"/repos/o/r/contents": `[` +
			`{"type":"dir","name":"cmd","path":"cmd","size":0,"sha":"d1"},` +
			`{"type":"file","name":"README.md","path":"README.md","size":6284,"sha":"f1"},` +
			`{"type":"symlink","name":"link\u202e","path":"link","size":9,"sha":"s1"}]`,
	})
	e, _, _ := testEnv(t, rt)
	r := runWith(t, registry, e, "file", "o/r", "/")
	want := "dir      0     cmd/\nfile     6284  README.md\nsymlink  9     link<U+202E>\n"
	if r.code != 0 || r.stdout != want {
		t.Fatalf("exit %d, stderr %q, asked %q, stdout:\n%s", r.code, r.stderr, rt.uris(), r.stdout)
	}
	r = runWith(t, registry, e, "file", "o/r", ".", "--json")
	if !strings.Contains(r.stdout, `"type": "dir"`) || !strings.Contains(r.stdout, `"entries": [`) || !strings.Contains(r.stdout, `"name": "README.md"`) {
		t.Fatalf("json:\n%s", r.stdout)
	}
}

func TestFileLargeUsesRawMediaType(t *testing.T) {
	var accepts []string
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		accept := r.Header.Get("Accept")
		accepts = append(accepts, accept)
		if accept == "application/vnd.github.raw+json" {
			io.WriteString(w, "big contents\n")
			return
		}
		io.WriteString(w, `{"type":"file","encoding":"none","size":2000000,"name":"big.txt","path":"big.txt","sha":"abc","content":""}`)
	})
	e, _, _ := testEnv(t, h)
	r := runWith(t, registry, e, "file", "o/r", "big.txt")
	if r.code != 0 || r.stdout != "big contents\n" || len(accepts) != 2 {
		t.Fatalf("exit %d, stderr %q, accepts %q, stdout %q", r.code, r.stderr, accepts, r.stdout)
	}
}

func TestFileSymlinkAndBinary(t *testing.T) {
	rt := newRoutes(t, map[string]string{
		"/repos/o/r/contents/link": `{"type":"symlink","target":"../etc","size":6,"name":"link","path":"link","sha":"s"}`,
		"/repos/o/r/contents/logo.png": `{"type":"file","encoding":"base64","size":4,"name":"logo.png","path":"logo.png","sha":"p","content":"` +
			base64.StdEncoding.EncodeToString([]byte{0x89, 'P', 'N', 0xff}) + `"}`,
	})
	e, _, _ := testEnv(t, rt)
	r := runWith(t, registry, e, "file", "o/r", "link")
	if r.code != 0 || r.stdout != "link is a symlink to ../etc\n" {
		t.Fatalf("exit %d, stdout %q, stderr %q", r.code, r.stdout, r.stderr)
	}
	r = runWith(t, registry, e, "file", "o/r", "logo.png")
	if r.code != 0 || r.stdout != "logo.png is a binary file of 4 bytes; --json gives it as content_base64\n" {
		t.Fatalf("exit %d, stdout %q, stderr %q", r.code, r.stdout, r.stderr)
	}
	r = runWith(t, registry, e, "file", "o/r", "logo.png", "--json")
	if !strings.Contains(r.stdout, `"content_base64": "iVBO/w=="`) || strings.Contains(r.stdout, `"content":`) {
		t.Fatalf("json:\n%s", r.stdout)
	}
}

func TestFileBadPaths(t *testing.T) {
	for _, p := range []string{"a/../b", "..", "a//b", "a/./b"} {
		t.Run(p, func(t *testing.T) {
			rt := newRoutes(t, nil)
			e, _, _ := testEnv(t, rt)
			r := runWith(t, registry, e, "file", "o/r", p)
			if r.code != 2 || len(rt.uris()) != 0 || !strings.Contains(r.stderr, "is not a path in the repository") {
				t.Fatalf("exit %d, asked %q, stderr %q", r.code, rt.uris(), r.stderr)
			}
		})
	}
}

func TestFilePathIsEscaped(t *testing.T) {
	rt := newRoutes(t, map[string]string{
		"/repos/o/r/contents/q%3F/h%23/b%5C/%C3%A9": `{"type":"file","encoding":"base64","size":1,"name":"x","path":"x","sha":"s","content":"eA=="}`,
	})
	e, _, _ := testEnv(t, rt)
	r := runWith(t, registry, e, "file", "o/r", "q?/h#/b\\/\u00e9")
	if r.code != 0 || r.stdout != "x" {
		t.Fatalf("exit %d, stderr %q, asked %q, stdout %q", r.code, r.stderr, rt.uris(), r.stdout)
	}
}
