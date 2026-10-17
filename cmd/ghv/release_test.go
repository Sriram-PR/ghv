package main

import (
	"fmt"
	"strings"
	"testing"
)

const releasesJSON = `[
{"id":3,"tag_name":"v2.0.0","name":"Two","draft":true,"prerelease":false,"published_at":null,"html_url":"https://github.com/o/r/releases/tag/untagged-1"},
{"id":2,"tag_name":"v1.1.0-rc1","name":"RC","draft":false,"prerelease":true,"published_at":"2026-10-01T00:00:00Z","html_url":"https://github.com/o/r/releases/tag/v1.1.0-rc1"},
{"id":1,"tag_name":"v1.0.0","name":"First","draft":false,"prerelease":false,"published_at":"2026-09-30T02:40:02Z","html_url":"https://github.com/o/r/releases/tag/v1.0.0"}]`

const releaseJSON = `{"id":1,"tag_name":"pkg@v1.0.0","name":"First \u001b[31mrelease","draft":false,"prerelease":false,
"author":{"login":"alice","type":"User"},"body":"Notes line\r\n","created_at":"2026-09-29T00:00:00Z","published_at":"2026-09-30T02:40:02Z",
"html_url":"https://github.com/o/r/releases/tag/pkg%40v1.0.0","assets":[
{"id":11,"name":"tool_linux.tar.gz","label":"","content_type":"application/gzip","size":1971,"download_count":5,
"digest":"sha256:afe49e9affa232faa8212aed035417166f6ade9b9470acb53d4dbd28c0504e8d","browser_download_url":"https://github.com/o/r/releases/download/pkg%40v1.0.0/tool_linux.tar.gz"},
{"id":12,"name":"notes.txt","label":"","content_type":"text/plain","size":10,"download_count":0,"digest":null,
"browser_download_url":"https://github.com/o/r/releases/download/pkg%40v1.0.0/notes.txt"}]}`

func TestReleaseList(t *testing.T) {
	rt := newRoutes(t, map[string]string{"/repos/o/r/releases?per_page=30": releasesJSON})
	e, _, _ := testEnv(t, rt)
	r := runWith(t, registry, e, "release", "list", "-R", "o/r")
	row := func(tag, state, published, name string) string {
		return fmt.Sprintf("%-10s  %-10s  %-20s  %s\n", tag, state, published, name)
	}
	want := row("v2.0.0", "draft", "-", "Two") + row("v1.1.0-rc1", "prerelease", "2026-10-01T00:00:00Z", "RC") +
		row("v1.0.0", "published", "2026-09-30T02:40:02Z", "First")
	if r.code != 0 || r.stdout != want {
		t.Fatalf("exit %d, stderr %q, stdout:\n%s\nwant:\n%s", r.code, r.stderr, r.stdout, want)
	}
	r = runWith(t, registry, e, "release", "list", "-R", "o/r", "--json")
	for _, s := range []string{`"tag": "v2.0.0"`, `"draft": true`, `"prerelease": true`, `"published_at": null`, `"url": "https://github.com/o/r/releases/tag/v1.0.0"`} {
		if !strings.Contains(r.stdout, s) {
			t.Fatalf("missing %s in:\n%s", s, r.stdout)
		}
	}
}

func TestReleaseView(t *testing.T) {
	rt := newRoutes(t, map[string]string{"/repos/o/r/releases/tags/pkg%40v1.0.0": releaseJSON})
	e, _, _ := testEnv(t, rt)
	r := runWith(t, registry, e, "release", "view", "pkg@v1.0.0", "-R", "o/r")
	want := "o/r pkg@v1.0.0 First <U+001B>[31mrelease\n\n" +
		"state        published\n" +
		"author       alice\n" +
		"published    2026-09-30T02:40:02Z\n" +
		"assets       2\n" +
		"url          https://github.com/o/r/releases/tag/pkg%40v1.0.0\n" +
		"\n" +
		"tool_linux.tar.gz  1971\n" +
		"notes.txt          10\n" +
		"\nNotes line\n"
	if r.code != 0 || r.stdout != want {
		t.Fatalf("exit %d, stderr %q, stdout:\n%s\nwant:\n%s", r.code, r.stderr, r.stdout, want)
	}
	r = runWith(t, registry, e, "release", "view", "pkg@v1.0.0", "-R", "o/r", "--json")
	for _, s := range []string{`"repo": "o/r"`, `"tag": "pkg@v1.0.0"`, `"author": "alice"`, `"body": "Notes line\r\n"`,
		`"id": 11`, `"content_type": "application/gzip"`, `"downloads": 5`, `"digest": "sha256:afe49e`, `"digest": ""`} {
		if !strings.Contains(r.stdout, s) {
			t.Fatalf("missing %s in:\n%s", s, r.stdout)
		}
	}
}

func TestReleaseTagPaths(t *testing.T) {
	for tag, uri := range map[string]string{
		"storage/v1.57.0": "/repos/o/r/releases/tags/storage%2Fv1.57.0",
		"a#b?c":           "/repos/o/r/releases/tags/a%23b%3Fc",
	} {
		rt := newRoutes(t, map[string]string{uri: `{"tag_name":"x","assets":[]}`})
		e, _, _ := testEnv(t, rt)
		if r := runWith(t, registry, e, "release", "view", tag, "-R", "o/r"); r.code != 0 {
			t.Errorf("%s: exit %d, stderr %q, asked %q", tag, r.code, r.stderr, rt.uris())
		}
	}
	rt := newRoutes(t, nil)
	e, _, _ := testEnv(t, rt)
	if r := runWith(t, registry, e, "release", "view", "", "-R", "o/r"); r.code != 2 || len(rt.uris()) != 0 {
		t.Fatalf("empty tag: exit %d, stderr %q", r.code, r.stderr)
	}
}
