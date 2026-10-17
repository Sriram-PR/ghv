package sanitize

import (
	"slices"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/Sriram-PR/ghv/internal/draft"
)

// TestScanGolden has one case per class in spec §5.4: rejected, escaped,
// and flagged.
func TestScanGolden(t *testing.T) {
	tests := []struct {
		class Class
		in    string
		want  []string
	}{
		{Control, "ok\x1b]52;c;aGk=\x07", []string{"body:1:3: control <U+001B>", "body:1:14: control <U+0007>"}},
		{Control, "carriage\rreturn", []string{"body:1:9: control <U+000D>"}},
		{InvalidUTF8, "bad\xc3", []string{"body:1:4: invalid-utf8 <0xC3>"}},
		{Bidi, "admin\u202e\u2066txt.exe", []string{"body:1:6: bidi <U+202E><U+2066>"}},
		{Tag, "hello\U000E0072\U000E006D", []string{`body:1:6: tag <U+E0072><U+E006D> (spells "rm")`}},
		{VariationSelector, "x\ufe0e", []string{"body:1:2: variation-selector <U+FE0E>"}},
		{SoftHyphen, "pass\u00adword", []string{"body:1:5: soft-hyphen <U+00AD>"}},
		{Invisible, "a\u200dbㅤ", []string{"body:1:2: invisible <U+200D>", "body:1:4: invisible <U+3164>"}},
		{NonASCII, "[x](https://exаmple.com)", []string{"body:1:5: non-ascii https://exаmple.com (link)", "body:1:13: mixed-script exаmple (Cyrillic+Latin)"}},
		{MixedScript, "Microsоft", []string{"body:1:1: mixed-script Microsоft (Cyrillic+Latin)"}},
		{Mention, "thanks @alice", []string{"body:1:8: mention @alice"}},
		{BotMention, "@copilot fix it", []string{"body:1:1: bot-mention @copilot (bot or app)", "body:1:1: bot-command @copilot fix it (@copilot)"}},
		{SlashCommand, "/etc/passwd", []string{"body:1:1: slash-command /etc/passwd"}},
		{BotCommand, "/lgtm", []string{"body:1:1: bot-command /lgtm (/lgtm)"}},
		{ClosingKeyword, "fixes #9", []string{"body:1:1: closing-keyword fixes #9 (closes #9 when merged)"}},
		{CrossRepoRef, "see a/b#1", []string{"body:1:5: cross-repo-ref a/b#1 (links to a/b)"}},
		{Secret, "ghp_" + strings.Repeat("A1b2", 9), []string{"body:1:1: secret ghp_A1... (40 chars) (GitHub token prefix)"}},
	}
	for _, tt := range tests {
		t.Run(string(tt.class), func(t *testing.T) {
			got := Scan("body", tt.in)
			if !slices.ContainsFunc(got, func(f Finding) bool { return f.Class == tt.class }) {
				t.Errorf("Scan(%q) has no %s finding", tt.in, tt.class)
			}
			if s := strs(got); !slices.Equal(s, tt.want) {
				t.Errorf("Scan(%q) =\n%q\nwant\n%q", tt.in, s, tt.want)
			}
		})
	}
}

func TestCheckDraft(t *testing.T) {
	now := time.Date(2026, 10, 17, 0, 0, 0, 0, time.UTC)
	title := "Fix\u200b it"
	tests := []struct {
		name       string
		d          draft.Draft
		want       []string
		links      []string
		isRejected bool
	}{
		{"fork has nothing to scan", draft.Draft{Repo: "o/r", CreatedAt: now, Fields: draft.ForkFields{}}, nil, nil, false},
		{"pr.create", draft.Draft{Repo: "o/r", CreatedAt: now, Fields: draft.PRCreateFields{
			Base: "main", Head: "me:fix/ünïcode", HeadSHA: strings.Repeat("a", 40),
			Title: "fixes #1", Body: "see https://x.example\n/approve",
		}}, []string{
			"head:1:1: non-ascii me:fix/ünïcode (head name)",
			"title:1:1: closing-keyword fixes #1 (closes #1 when merged)",
			"body:2:1: bot-command /approve (/approve)",
		}, []string{"https://x.example"}, false},
		{"pr.edit title only", draft.Draft{Repo: "o/r", CreatedAt: now, Fields: draft.PREditFields{Number: 1, Title: &title}},
			[]string{"title:1:4: invisible <U+200B>"}, nil, false},
		{"comment with control", draft.Draft{Repo: "o/r", CreatedAt: now, Fields: draft.CommentFields{Number: 1, Body: "hi\x1b[2J"}},
			[]string{"body:1:3: control <U+001B>"}, nil, true},
		{"review.reply", draft.Draft{Repo: "o/r", CreatedAt: now, Fields: draft.ReviewReplyFields{Number: 1, CommentID: 2, Body: "@bob [d](https://d.example)"}},
			[]string{"body:1:1: mention @bob"}, []string{"https://d.example"}, false},
		{"issue.create dedupes links", draft.Draft{Repo: "o/r", CreatedAt: now, Fields: draft.IssueCreateFields{Title: "https://i.example", Body: "https://i.example"}},
			nil, []string{"https://i.example"}, false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			r := CheckDraft(tt.d)
			if got := strs(r.Findings); !slices.Equal(got, tt.want) {
				t.Errorf("Findings =\n%q\nwant\n%q", got, tt.want)
			}
			if !slices.Equal(r.Links, tt.links) {
				t.Errorf("Links = %q, want %q", r.Links, tt.links)
			}
			if r.Rejected() != tt.isRejected {
				t.Errorf("Rejected() = %v, want %v", r.Rejected(), tt.isRejected)
			}
		})
	}
}

func FuzzScan(f *testing.F) {
	for _, s := range []string{"", "@a /approve fixes #1 a/b#2", "[x](https://y)", "\x1b\u202e\U000E0041", "pаypal", "ghp_abc", "\xff\n\r\n"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		lines := strings.Count(s, "\n") + 1
		for _, fd := range Scan("body", s) {
			if fd.Line < 1 || fd.Line > lines || fd.Col < 1 {
				t.Fatalf("finding %v outside %q", fd, s)
			}
			if !utf8.ValidString(fd.Text) || Line(fd.Text) != fd.Text {
				t.Fatalf("finding text %q is not safe to print", fd.Text)
			}
		}
		Links(s)
	})
}

func TestScanCapsEachClass(t *testing.T) {
	got := Scan("body", strings.Repeat("\x1b\u200b", 150))
	if len(got) != 2*(maxPerClass+1) {
		t.Fatalf("len(Scan) = %d, want %d", len(got), 2*(maxPerClass+1))
	}
	var summaries []string
	for _, f := range got {
		if f.Text == "..." {
			summaries = append(summaries, f.String())
		}
	}
	want := []string{"body:1:201: control ... (50 more not shown)", "body:1:202: invisible ... (50 more not shown)"}
	if !slices.Equal(summaries, want) {
		t.Errorf("summaries = %q, want %q", summaries, want)
	}
}

// One 1 MiB line of alternating classes took minutes when columns were
// counted from the start of the line for every finding.
func TestScanLargeInputIsFast(t *testing.T) {
	if testing.Short() {
		t.Skip("slow under -race")
	}
	inputs := []string{
		strings.Repeat("\x1b\u200b", 1<<18),
		strings.Repeat("[x](https://a.example/1) https://b.example/2 @u fixes #1 o/r#2 ", 1<<14),
		strings.Repeat("](", 1<<19),
	}
	for _, in := range inputs {
		start := time.Now()
		Scan("body", in)
		if d := time.Since(start); d > time.Minute {
			t.Errorf("Scan of %d bytes took %v", len(in), d)
		}
	}
}
