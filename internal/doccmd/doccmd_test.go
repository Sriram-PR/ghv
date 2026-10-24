package doccmd

import (
	"reflect"
	"strings"
	"testing"
)

func TestParse(t *testing.T) {
	md := strings.Join([]string{
		"Read a pull request with `ghv pr view 12 -R cli/cli`, or review with `ghv-apply`.",
		"Not commands: `ghv`, `ghv-applyx`, `ghvx pr`, `go test`.",
		"```sh",
		"$ ghv pr diff 12 --repo cli/cli | less",
		"ghv search issues 'is:open label:\"good first issue\"' --limit 5 # comment",
		"ghv api repos/cli/cli \\",
		"  --jq .name > out.json",
		"sudo -H -u ghv-agent /usr/local/bin/ghv-apply doctor --human-user me",
		"GH_DEBUG=1 ghv rate",
		"echo ghv rate",
		"```",
		"~~~",
		"ghv: unknown flag \"--bogus\"",
		"ghv drafts --json 2>/dev/null",
		"~~~",
	}, "\n")
	got, err := Parse("doc.md", []byte(md))
	if err != nil {
		t.Fatal(err)
	}
	want := []Line{
		{"doc.md", 1, []string{"ghv", "pr", "view", "12", "-R", "cli/cli"}},
		{"doc.md", 1, []string{"ghv-apply"}},
		{"doc.md", 4, []string{"ghv", "pr", "diff", "12", "--repo", "cli/cli"}},
		{"doc.md", 5, []string{"ghv", "search", "issues", `is:open label:"good first issue"`, "--limit", "5"}},
		{"doc.md", 6, []string{"ghv", "api", "repos/cli/cli", "--jq", ".name"}},
		{"doc.md", 8, []string{"ghv-apply", "doctor", "--human-user", "me"}},
		{"doc.md", 9, []string{"ghv", "rate"}},
		{"doc.md", 14, []string{"ghv", "drafts", "--json"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got:\n%+v\nwant:\n%+v", got, want)
	}
}

func TestParseRefusesAnOpenQuote(t *testing.T) {
	_, err := Parse("doc.md", []byte("```\nghv search issues 'is:open\n```\n"))
	if err == nil || err.Error() != "doc.md:2: unclosed quote" {
		t.Fatalf("err %v", err)
	}
}
