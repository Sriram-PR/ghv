package sanitize

import (
	"slices"
	"testing"
)

func TestMentions(t *testing.T) {
	tests := []struct {
		name, in string
		want     []string
	}{
		{"none", "no mentions here", nil},
		{"user", "thanks @octocat!", []string{"body:1:8: mention @octocat"}},
		{"start of text", "@octocat hi", []string{"body:1:1: mention @octocat"}},
		{"team", "cc @github/security.", []string{"body:1:4: mention @github/security"}},
		{"after paren", "(@a-b)", []string{"body:1:2: mention @a-b"}},
		{"trailing hyphen dropped", "@abc- x", []string{"body:1:1: mention @abc"}},
		{"email is not a mention", "mail a@b.com or x.@y", nil},
		{"path is not a mention", "see /@scope/pkg", nil},
		{"bot by name", "@Claude please", []string{"body:1:1: bot-mention @Claude (bot or app)"}},
		{"bot suffix", "ping @some-app[bot]", []string{"body:1:6: bot-mention @some-app[bot] (bot or app)"}},
		{"dependabot", "@dependabot rebase", []string{"body:1:1: bot-mention @dependabot (bot or app)"}},
		{"non-ascii", "hi @оctocat", []string{"body:1:4: mention @оctocat", "body:1:4: non-ascii @оctocat (mention)"}},
		{"line two", "a\n  @x", []string{"body:2:3: mention @x"}},
		{"two", "@a @b", []string{"body:1:1: mention @a", "body:1:4: mention @b"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := strs(mentions("body", tt.in)); !slices.Equal(got, tt.want) {
				t.Errorf("mentions(%q) =\n%q\nwant\n%q", tt.in, got, tt.want)
			}
		})
	}
}

func TestCommands(t *testing.T) {
	tests := []struct {
		name, in string
		want     []string
	}{
		{"none", "LGTM, thanks\nnot /approve mid-line", nil},
		{"approve", "/approve", []string{"body:1:1: bot-command /approve (/approve)"}},
		{"case and indent", "ok\n  /LGTM cancel", []string{"body:2:3: bot-command /LGTM cancel (/lgtm)"}},
		{"crlf", "/merge\r\nnext", []string{"body:1:1: bot-command /merge (/merge)"}},
		{"prefix of a longer word", "/testing notes", []string{"body:1:1: slash-command /testing notes"}},
		{"other slash line", "/usr/bin/env", []string{"body:1:1: slash-command /usr/bin/env"}},
		{"bors", "bors r+", []string{"body:1:1: bot-command bors r+ (bors r+)"}},
		{"bors delegate", "bors r=alice", []string{"body:1:1: bot-command bors r=alice (bors r=)"}},
		{"mergify", "@Mergifyio queue default", []string{"body:1:1: bot-command @Mergifyio queue default (@mergifyio queue)"}},
		{"dependabot", "@dependabot squash and merge", []string{"body:1:1: bot-command @dependabot squash and merge (@dependabot squash and merge)"}},
		{"tab indent", "\t/retest", []string{"body:1:2: bot-command /retest (/retest)"}},
		{"escape in command", "/approve\x1b[2K", []string{"body:1:1: bot-command /approve<U+001B>[2K (/approve)"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := strs(commands("body", tt.in)); !slices.Equal(got, tt.want) {
				t.Errorf("commands(%q) =\n%q\nwant\n%q", tt.in, got, tt.want)
			}
		})
	}
}
