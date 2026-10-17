package main

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

const artifactURL = "https://productionresultssa2.blob.core.windows.net/actions-results/x/artifacts/"

// artifactsHub serves run 7's artifacts: dist and cov with the given zips,
// and old, which has expired. A digest of "" sends the zip's real one.
func artifactsHub(t *testing.T, dist, cov []byte, covDigest string) *fakeHub {
	h := newHub(t)
	if covDigest == "" {
		covDigest = fmt.Sprintf("sha256:%x", sha256.Sum256(cov))
	}
	h.api["/repos/o/r/actions/runs/7/artifacts?per_page=100"] = fmt.Sprintf(`{"total_count":3,"artifacts":[
{"id":1,"name":"dist","size_in_bytes":%d,"expired":false,"expires_at":"2026-12-01T00:00:00Z","digest":"sha256:%x"},
{"id":2,"name":"old","size_in_bytes":10,"expired":true,"expires_at":"2026-09-01T00:00:00Z","digest":null},
{"id":3,"name":"cov","size_in_bytes":%d,"expired":false,"expires_at":"2026-12-01T00:00:00Z","digest":"%s"}]}`,
		len(dist), sha256.Sum256(dist), len(cov), covDigest)
	for id, b := range map[string][]byte{"1": dist, "3": cov} {
		h.redirect["/repos/o/r/actions/artifacts/"+id+"/zip"] = artifactURL + id + ".zip?sig=s"
		h.files[artifactURL+id+".zip"] = b
	}
	return h
}

func TestRunArtifacts(t *testing.T) {
	h := artifactsHub(t, zipBytes(t, "a", "A"), zipBytes(t, "c", "C"), "")
	e, _, _ := testEnv(t, h)
	r := runWith(t, registry, e, "run", "artifacts", "7", "-R", "o/r")
	if r.code != 0 || !strings.HasPrefix(r.stdout, "dist  ") || !strings.Contains(r.stdout, "old   10   expired\n") ||
		!strings.Contains(r.stdout, "expires 2026-12-01T00:00:00Z") {
		t.Fatalf("exit %d, stderr %q, stdout:\n%s", r.code, r.stderr, r.stdout)
	}
	r = runWith(t, registry, e, "run", "artifacts", "7", "-R", "o/r", "--json")
	for _, s := range []string{`"id": 1`, `"name": "dist"`, `"expired": true`, `"expires_at": "2026-09-01T00:00:00Z"`, `"digest": ""`} {
		if !strings.Contains(r.stdout, s) {
			t.Fatalf("missing %s in:\n%s", s, r.stdout)
		}
	}
}

func TestRunArtifactsDownload(t *testing.T) {
	h := artifactsHub(t, zipBytes(t, "bin/tool", "TOOL", "README", "read me"), zipBytes(t, "c.out", "C"), "")
	e, _, _ := testEnv(t, h)
	setToken(t, e, "ghp_x")
	r := runWith(t, registry, e, "run", "artifacts", "7", "-R", "o/r", "--download", "out")
	out := filepath.Join(e.dir, "out")
	b, _ := os.ReadFile(filepath.Join(out, "dist", "bin", "tool"))
	entries, _ := os.ReadDir(out)
	var names []string
	for _, en := range entries {
		names = append(names, en.Name())
	}
	if r.code != 0 || string(b) != "TOOL" || !slices.Equal(names, []string{"cov", "dist"}) {
		t.Fatalf("exit %d, stderr %q, tool %q, out %q", r.code, r.stderr, b, names)
	}
	if r.stdout != "saved dist: 2 files in out/dist\nsaved cov: 1 file in out/cov\n" || !strings.Contains(r.stderr, "skipped old: it has expired") {
		t.Fatalf("stdout %q, stderr %q", r.stdout, r.stderr)
	}
}

func TestRunArtifactsDownloadByName(t *testing.T) {
	h := artifactsHub(t, zipBytes(t, "a", "A"), zipBytes(t, "c", "C"), "")
	e, _, _ := testEnv(t, h)
	setToken(t, e, "ghp_x")
	r := runWith(t, registry, e, "run", "artifacts", "7", "-R", "o/r", "--download", filepath.Join(e.dir, "out"), "--name", "cov", "--json")
	if r.code != 0 || !strings.Contains(r.stdout, `"name": "cov"`) || strings.Contains(r.stdout, "dist") {
		t.Fatalf("exit %d, stderr %q, stdout %s", r.code, r.stderr, r.stdout)
	}
	for _, u := range h.uris() {
		if strings.Contains(u, "artifacts/1/") {
			t.Fatalf("fetched %s", u)
		}
	}
}

func TestRunArtifactsDownloadRefusals(t *testing.T) {
	tests := []struct {
		name string
		args []string
		exit int
		msg  string
	}{
		{"name without download", []string{"--name", "dist"}, 2, "--name needs --download"},
		{"unknown name", []string{"--download", "out", "--name", "nope"}, 2, `no artifact named "nope"`},
		{"expired name", []string{"--download", "out", "--name", "old"}, 1, "old has expired"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := artifactsHub(t, zipBytes(t, "a", "A"), zipBytes(t, "c", "C"), "")
			e, _, _ := testEnv(t, h)
			setToken(t, e, "ghp_x")
			r := runWith(t, registry, e, append([]string{"run", "artifacts", "7", "-R", "o/r"}, tt.args...)...)
			if r.code != tt.exit || !strings.Contains(r.stderr, tt.msg) || exists(filepath.Join(e.dir, "out")) {
				t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
			}
		})
	}
}

func TestRunArtifactsDownloadNeedsANewDirectory(t *testing.T) {
	h := artifactsHub(t, zipBytes(t, "a", "A"), zipBytes(t, "c", "C"), "")
	e, _, _ := testEnv(t, h)
	setToken(t, e, "ghp_x")
	os.Mkdir(filepath.Join(e.dir, "out"), 0o755)
	r := runWith(t, registry, e, "run", "artifacts", "7", "-R", "o/r", "--download", "out")
	if r.code != 2 || !strings.Contains(r.stderr, "already exists") || len(h.uris()) != 0 {
		t.Fatalf("exit %d, stderr %q, asked %q", r.code, r.stderr, h.uris())
	}
}

func TestRunArtifactsDownloadNeedsAToken(t *testing.T) {
	h := artifactsHub(t, zipBytes(t, "a", "A"), zipBytes(t, "c", "C"), "")
	e, _, _ := testEnv(t, h)
	r := runWith(t, registry, e, "run", "artifacts", "7", "-R", "o/r", "--download", "out")
	if r.code != 3 || len(h.uris()) != 0 || exists(filepath.Join(e.dir, "out")) {
		t.Fatalf("exit %d, stderr %q, asked %q", r.code, r.stderr, h.uris())
	}
}

// A failure after the first artifact removes the whole directory.
func TestRunArtifactsDownloadFailuresLeaveNothing(t *testing.T) {
	tests := []struct {
		name   string
		cov    []byte
		digest string
		msg    string
	}{
		{"digest mismatch", zipBytes(t, "c", "C"), "sha256:" + strings.Repeat("0", 64), "does not match its digest"},
		{"zip slip", zipBytes(t, "../evil", "x"), "", "unsafe zip entry"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := artifactsHub(t, zipBytes(t, "a", "A"), tt.cov, tt.digest)
			e, _, _ := testEnv(t, h)
			setToken(t, e, "ghp_x")
			r := runWith(t, registry, e, "run", "artifacts", "7", "-R", "o/r", "--download", "out")
			if r.code != 1 || !strings.Contains(r.stderr, tt.msg) || exists(filepath.Join(e.dir, "out")) || exists(filepath.Join(e.dir, "evil")) {
				t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
			}
		})
	}
}

func TestRunArtifactsRefusesUnsafeNames(t *testing.T) {
	h := newHub(t)
	h.api["/repos/o/r/actions/runs/7/artifacts?per_page=100"] = `{"total_count":1,"artifacts":[{"id":1,"name":"../x","size_in_bytes":1,"expired":false}]}`
	e, _, _ := testEnv(t, h)
	setToken(t, e, "ghp_x")
	r := runWith(t, registry, e, "run", "artifacts", "7", "-R", "o/r", "--download", "out")
	if r.code != 1 || !strings.Contains(r.stderr, "unsafe artifact name") || exists(filepath.Join(e.dir, "out")) {
		t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
	}
}

func exists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}
