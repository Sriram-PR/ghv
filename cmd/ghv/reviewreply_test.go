package main

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/Sriram-PR/ghv/internal/draft"
)

func replyRoutes(t *testing.T) *routes {
	return newRoutes(t, map[string]string{
		"/repos/o/r/pulls/comments/101": `{"id":101,"pull_request_url":"https://api.github.com/repos/o/r/pulls/7"}`,
		"/repos/o/r/pulls/comments/102": `{"id":102,"in_reply_to_id":101,"pull_request_url":"https://api.github.com/repos/o/r/pulls/7"}`,
		"/repos/o/r/pulls/comments/103": `{"id":103,"pull_request_url":"https://api.github.com/repos/o/r/pulls/8"}`,
	})
}

func TestReviewReply(t *testing.T) {
	for id, note := range map[string]string{
		"101": "",
		"102": "ghv: note: comment 102 is a reply; the draft replies to the first comment of its thread, 101\n",
	} {
		e, _, _ := testEnv(t, replyRoutes(t))
		writeFile(t, filepath.Join(e.dir, "b.md"), "Done.\n")
		r := runWith(t, registry, e, "review", "reply", "7", id, "-R", "o/r", "--body-file", "b.md")
		if r.code != 0 || !draftedRE.MatchString(r.stdout) || r.stderr != note {
			t.Fatalf("%s: exit %d, stdout %q, stderr %q", id, r.code, r.stdout, r.stderr)
		}
		if f := queued(t, e).Fields; f != (draft.ReviewReplyFields{Number: 7, CommentID: 101, Body: "Done.\n"}) {
			t.Fatalf("%s: fields %+v", id, f)
		}
	}
}

func TestReviewReplyRefusals(t *testing.T) {
	tests := []struct {
		name string
		args []string
		exit int
		msg  string
	}{
		{"other pull request", []string{"7", "103"}, 2, "review comment 103 is on pull request 8, not 7"},
		{"missing comment", []string{"7", "999"}, 1, "no review comment 999 in o/r; for a conversation comment, use ghv comment"},
		{"bad comment ID", []string{"7", "0101"}, 2, `"0101" is not a comment ID`},
		{"empty body", []string{"7", "101", "--body-file", "empty.md"}, 2, "the body is empty"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rt := replyRoutes(t)
			e, _, _ := testEnv(t, rt)
			writeFile(t, filepath.Join(e.dir, "b.md"), "Done.\n")
			writeFile(t, filepath.Join(e.dir, "empty.md"), "\n")
			args := append([]string{"review", "reply", "-R", "o/r"}, tt.args...)
			if !strings.Contains(strings.Join(tt.args, " "), "--body-file") {
				args = append(args, "--body-file", "b.md")
			}
			r := runWith(t, registry, e, args...)
			if r.code != tt.exit || !strings.Contains(r.stderr, tt.msg) {
				t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
			}
			if tt.exit == 2 && tt.name != "other pull request" && len(rt.uris()) != 0 {
				t.Fatalf("asked %q", rt.uris())
			}
			emptyQueue(t, e)
		})
	}
}

func TestRunIDsStillParse(t *testing.T) {
	e, _, _ := testEnv(t, noNetwork(t))
	r := runWith(t, registry, e, "run", "logs", "007", "-R", "o/r")
	if r.code != 2 || !strings.Contains(r.stderr, `"007" is not a run ID`) {
		t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
	}
}
