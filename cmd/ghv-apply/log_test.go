package main

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/Sriram-PR/ghv/internal/audit"
)

func TestLog(t *testing.T) {
	e := testEnv(t)
	if r := runWith(e, "log"); r.code != 0 || r.stdout != "The audit log is empty.\n" || r.stderr != "" {
		t.Fatalf("exit %d, stdout %q, stderr %q", r.code, r.stdout, r.stderr)
	}
	at := time.Date(2026, 10, 18, 12, 0, 0, 0, time.UTC)
	for _, en := range []audit.Entry{
		{Time: at, Draft: "d-0123456789ab", Kind: "comment", Repo: "o/r", RequestSHA256: "ab", Status: 201, URL: "https://github.com/o/r/issues/7#issuecomment-1"},
		{Time: at.Add(90 * time.Minute), Draft: "d-0123456789ac", Kind: "issue.create", Repo: "evil\x1b]52;c;x\x07/r", URL: "https://github.com/o/r/issues/8", Found: true},
	} {
		if err := audit.Append(audit.Path(e.home), en); err != nil {
			t.Fatal(err)
		}
	}
	r := runWith(e, "log")
	repo := "evil<U+001B>]52;c;x<U+0007>/r"
	want := "2026-10-18 12:00 UTC  d-0123456789ab  comment       o/r" + strings.Repeat(" ", len(repo)-len("o/r")+2) + "https://github.com/o/r/issues/7#issuecomment-1\n" +
		"2026-10-18 13:30 UTC  d-0123456789ac  issue.create  " + repo + "  https://github.com/o/r/issues/8 (found later)\n"
	if r.code != 0 || r.stdout != want {
		t.Fatalf("exit %d, stderr %q, stdout:\n%s", r.code, r.stderr, r.stdout)
	}
}

func TestCommandFlags(t *testing.T) {
	var got []string
	withCommand(t, command{name: "zz-flags", flags: []string{"--yes"}, run: func(_ context.Context, e env) error {
		got = e.args
		return nil
	}})
	e := testEnv(t)
	if r := runWith(e, "zz-flags", "--yes"); r.code != 0 || len(got) != 1 || got[0] != "--yes" {
		t.Fatalf("exit %d, args %q, stderr %q", r.code, got, r.stderr)
	}
	if r := runWith(e, "zz-flags", "--no"); r.code != 2 || r.stderr != "ghv-apply: zz-flags takes only --yes\n" {
		t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
	}
	if r := runWith(e, "log", "--yes"); r.code != 2 || !strings.Contains(r.stderr, "log takes no arguments") {
		t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
	}
}
