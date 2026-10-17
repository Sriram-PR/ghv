package main

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const assetURL = "https://release-assets.githubusercontent.com/github-production-release-asset/1/"

// assetsHub serves release v1.0.0 with two assets. tool is served as
// toolBody, but its size and digest describe toolMeta.
func assetsHub(t *testing.T, toolName, toolMeta, toolBody string) *fakeHub {
	h := newHub(t)
	h.api["/repos/o/r/releases/tags/v1.0.0"] = fmt.Sprintf(`{"tag_name":"v1.0.0","assets":[
{"id":11,"name":%q,"size":%d,"digest":"sha256:%x"},
{"id":12,"name":"notes.txt","size":5,"digest":null}]}`, toolName, len(toolMeta), sha256.Sum256([]byte(toolMeta)))
	h.redirect["/repos/o/r/releases/assets/11"] = assetURL + "11?sig=s"
	h.files[assetURL+"11"] = []byte(toolBody)
	h.redirect["/repos/o/r/releases/assets/12"] = assetURL + "12?sig=s"
	h.files[assetURL+"12"] = []byte("notes")
	return h
}

func TestReleaseDownload(t *testing.T) {
	h := assetsHub(t, "tool.tar.gz", "TOOL", "TOOL")
	e, _, _ := testEnv(t, h)
	r := runWith(t, registry, e, "release", "download", "v1.0.0", "-R", "o/r")
	tool, _ := os.ReadFile(filepath.Join(e.dir, "tool.tar.gz"))
	notes, _ := os.ReadFile(filepath.Join(e.dir, "notes.txt"))
	if r.code != 0 || string(tool) != "TOOL" || string(notes) != "notes" {
		t.Fatalf("exit %d, stderr %q, tool %q, notes %q", r.code, r.stderr, tool, notes)
	}
	want := "saved tool.tar.gz (4 bytes, sha256 checked)\nsaved notes.txt (5 bytes, no digest from GitHub)\n"
	if r.stdout != want || h.accept["/repos/o/r/releases/assets/11"] != "application/octet-stream" {
		t.Fatalf("stdout %q, Accept %q", r.stdout, h.accept["/repos/o/r/releases/assets/11"])
	}
}

func TestReleaseDownloadOneAssetToDir(t *testing.T) {
	h := assetsHub(t, "tool.tar.gz", "TOOL", "TOOL")
	e, _, _ := testEnv(t, h)
	r := runWith(t, registry, e, "release", "download", "v1.0.0", "-R", "o/r", "--asset", "notes.txt", "--dir", "out/sub", "--json")
	notes, _ := os.ReadFile(filepath.Join(e.dir, "out", "sub", "notes.txt"))
	if r.code != 0 || string(notes) != "notes" || !strings.Contains(r.stdout, `"path": "out/sub/notes.txt"`) || !strings.Contains(r.stdout, `"verified": false`) {
		t.Fatalf("exit %d, stderr %q, stdout %s", r.code, r.stderr, r.stdout)
	}
	for _, u := range h.uris() {
		if strings.Contains(u, "assets/11") {
			t.Fatalf("fetched %s", u)
		}
	}
}

func TestReleaseDownloadNeverOverwrites(t *testing.T) {
	h := assetsHub(t, "tool.tar.gz", "TOOL", "TOOL")
	e, _, _ := testEnv(t, h)
	writeFile(t, filepath.Join(e.dir, "notes.txt"), "mine")
	r := runWith(t, registry, e, "release", "download", "v1.0.0", "-R", "o/r")
	b, _ := os.ReadFile(filepath.Join(e.dir, "notes.txt"))
	if r.code != 2 || !strings.Contains(r.stderr, "notes.txt already exists") || string(b) != "mine" || exists(filepath.Join(e.dir, "tool.tar.gz")) {
		t.Fatalf("exit %d, stderr %q, notes %q", r.code, r.stderr, b)
	}
}

func TestReleaseDownloadRemovesBadFiles(t *testing.T) {
	tests := []struct {
		name, meta, body, msg string
	}{
		{"digest mismatch", "TOOL", "EVIL", "does not match its digest"},
		{"more than its size", "TOOL", "TOOLS", "too large"},
		{"less than its size", "TOOL", "TOO", "3 of 4 bytes"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := assetsHub(t, "tool.tar.gz", tt.meta, tt.body)
			e, _, _ := testEnv(t, h)
			r := runWith(t, registry, e, "release", "download", "v1.0.0", "-R", "o/r", "--asset", "tool.tar.gz")
			if r.code != 1 || !strings.Contains(r.stderr, tt.msg) || exists(filepath.Join(e.dir, "tool.tar.gz")) {
				t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
			}
		})
	}
}

func TestReleaseDownloadRefusals(t *testing.T) {
	tests := []struct {
		name, tool string
		args       []string
		exit       int
		msg        string
	}{
		{"unsafe name", "../evil", nil, 1, "unsafe asset name"},
		{"unknown asset", "tool.tar.gz", []string{"--asset", "nope"}, 2, `no asset named "nope"`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := assetsHub(t, tt.tool, "TOOL", "TOOL")
			e, _, _ := testEnv(t, h)
			r := runWith(t, registry, e, append([]string{"release", "download", "v1.0.0", "-R", "o/r"}, tt.args...)...)
			entries, _ := os.ReadDir(e.dir)
			if r.code != tt.exit || !strings.Contains(r.stderr, tt.msg) || len(entries) != 0 || exists(filepath.Join(filepath.Dir(e.dir), "evil")) {
				t.Fatalf("exit %d, stderr %q, dir %v", r.code, r.stderr, entries)
			}
		})
	}
}

func TestReleaseDownloadRepeatedAsset(t *testing.T) {
	h := assetsHub(t, "tool.tar.gz", "TOOL", "TOOL")
	e, _, _ := testEnv(t, h)
	r := runWith(t, registry, e, "release", "download", "v1.0.0", "-R", "o/r", "--asset", "notes.txt", "--asset", "notes.txt")
	if r.code != 0 || r.stdout != "saved notes.txt (5 bytes, no digest from GitHub)\n" {
		t.Fatalf("exit %d, stderr %q, stdout %q", r.code, r.stderr, r.stdout)
	}
}
