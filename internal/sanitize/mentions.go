package sanitize

import (
	"regexp"
	"slices"
	"strings"
)

// The leading group stands in for a lookbehind: an @ after a letter, digit,
// or one of _ / . @ - is part of an email or path, not a mention.
var mentionRE = regexp.MustCompile(`(?:^|[^\p{L}\p{N}_/.@-])(@([\p{L}\p{N}](?:[\p{L}\p{N}-]*[\p{L}\p{N}])?(?:/[\p{L}\p{N}_-]+)?)(\[bot\])?)`)

var bots = []string{
	"claude", "copilot", "dependabot", "mergifyio", "mergify", "renovate", "renovatebot",
	"github-actions", "codecov", "coderabbitai", "gemini-code-assist", "sourcery-ai",
	"greptileai", "cursor", "devin-ai-integration", "sweep-ai", "bors", "rust-bors",
	"k8s-ci-robot", "openai", "chatgpt-codex-connector", "kodiakhq", "allcontributors",
}

func mentions(field, text string) []Finding {
	l := newLocator(text)
	var out []Finding
	for _, m := range mentionRE.FindAllStringSubmatchIndex(text, -1) {
		full, name := text[m[2]:m[3]], text[m[4]:m[5]]
		c, detail := Mention, ""
		if m[6] >= 0 || slices.Contains(bots, strings.ToLower(name)) {
			c, detail = BotMention, "bot or app"
		}
		out = append(out, l.finding(c, field, m[2], Line(full), detail))
		if !isASCII(name) {
			out = append(out, l.finding(NonASCII, field, m[2], Line(full), "mention"))
		}
	}
	return out
}

// botCommands start a line and are matched case-insensitively. One
// approved line with any of them can merge, deploy, or rerun CI.
var botCommands = []string{
	"/approve", "/lgtm", "/merge", "/retest", "/test", "/ok-to-test", "/hold", "/unhold",
	"/lift", "/deploy", "/ship", "/rebase", "/close", "/reopen", "/assign", "/unassign",
	"/label", "/remove-label", "/override", "/skip", "/backport", "/cherry-pick", "/benchmark",
	"/run", "/rerun", "/release", "/publish", "/fast-forward", "/automerge", "/squash",
	"bors r+", "bors r=", "bors merge", "bors try", "bors delegate", "bors retry",
	"@bors r+", "@bors r=", "@bors try", "@bors delegate", "@bors retry", "@rust-bors r+",
	"@dependabot merge", "@dependabot squash and merge", "@dependabot rebase", "@dependabot recreate",
	"@mergifyio queue", "@mergifyio rebase", "@mergifyio refresh", "@mergifyio backport", "@mergifyio update",
	"@renovatebot rebase", "@kodiakhq merge", "@copilot", "@claude",
}

func commands(field, text string) []Finding {
	var out []Finding
	lineNo := 0
	for line := range strings.Lines(text) {
		lineNo++
		trimmed := strings.TrimLeft(line, " \t")
		lead := len(line) - len(trimmed)
		trimmed = strings.TrimRight(trimmed, "\r\n")
		if trimmed == "" {
			continue
		}
		col := lead + 1
		if cmd, ok := botCommand(trimmed); ok {
			out = append(out, Finding{Class: BotCommand, Field: field, Line: lineNo, Col: col, Text: Line(trimmed), Detail: cmd})
		} else if trimmed[0] == '/' {
			out = append(out, Finding{Class: SlashCommand, Field: field, Line: lineNo, Col: col, Text: Line(trimmed)})
		}
	}
	return out
}

func botCommand(line string) (string, bool) {
	lower := strings.ToLower(line)
	for _, c := range botCommands {
		rest, ok := strings.CutPrefix(lower, c)
		if !ok {
			continue
		}
		if rest == "" || strings.HasSuffix(c, "=") || !isWordByte(rest[0]) {
			return c, true
		}
	}
	return "", false
}

func isWordByte(b byte) bool {
	return b == '_' || b == '-' || b >= '0' && b <= '9' || b >= 'a' && b <= 'z' || b >= 'A' && b <= 'Z'
}
