package main

import (
	"errors"
	"slices"
	"strings"
	"testing"
)

var testCmds = []*command{
	{path: "repo view", args: []string{"R?"}, flags: []flag{repoFlag}},
	{path: "api", args: []string{"PATH"}, flags: []flag{{name: "paginate", kind: boolFlag}, limitFlag}},
	{path: "api graphql", flags: []flag{{name: "query", short: "q", kind: stringFlag}, {name: "field", short: "F", kind: multiFlag}}},
}

func TestParse(t *testing.T) {
	tests := []struct {
		args  []string
		path  string
		pos   []string
		check func(*invocation) bool
	}{
		{[]string{"repo", "view"}, "repo view", nil, nil},
		{[]string{"repo", "view", "o/r", "--json"}, "repo view", []string{"o/r"}, func(i *invocation) bool { return i.bool("json") }},
		{[]string{"repo", "view", "-R", "o/r"}, "repo view", nil, func(i *invocation) bool { return i.str("repo") == "o/r" }},
		{[]string{"repo", "view", "--repo=o/r"}, "repo view", nil, func(i *invocation) bool { return i.str("repo") == "o/r" }},
		{[]string{"api", "repos/o/r", "--paginate", "--limit", "5"}, "api", []string{"repos/o/r"}, func(i *invocation) bool { return i.bool("paginate") && i.int("limit") == 5 }},
		{[]string{"api", "--limit=7", "repos/o/r"}, "api", []string{"repos/o/r"}, func(i *invocation) bool { return i.int("limit") == 7 }},
		{[]string{"api", "repos/o/r"}, "api", []string{"repos/o/r"}, func(i *invocation) bool { return i.int("limit") == 1000 && !i.bool("paginate") }},
		{[]string{"api", "graphql", "-q", "@f", "-F", "a=1", "-F", "b=x"}, "api graphql", nil, func(i *invocation) bool {
			return i.str("query") == "@f" && slices.Equal(i.list("field"), []string{"a=1", "b=x"})
		}},
		{[]string{"api", "--", "-weird"}, "api", []string{"-weird"}, nil},
	}
	for _, tt := range tests {
		t.Run(strings.Join(tt.args, " "), func(t *testing.T) {
			inv, err := parse(testCmds, tt.args)
			if err != nil {
				t.Fatal(err)
			}
			if inv.cmd.path != tt.path || !slices.Equal(inv.args, tt.pos) {
				t.Fatalf("got %q %q", inv.cmd.path, inv.args)
			}
			if tt.check != nil && !tt.check(inv) {
				t.Fatalf("flags wrong: %+v", inv.values)
			}
		})
	}
}

func TestParseUsageErrors(t *testing.T) {
	tests := []struct {
		args []string
		msg  string
	}{
		{nil, "no command given"},
		{[]string{"nope"}, `unknown command "nope"`},
		{[]string{"repo"}, "ghv repo needs a subcommand: view"},
		{[]string{"repo", "edit"}, "ghv repo needs a subcommand: view"},
		{[]string{"--json"}, "no command given"},
		{[]string{"repo", "view", "--nope"}, `unknown flag "--nope" for ghv repo view`},
		{[]string{"repo", "view", "-x"}, `unknown flag "-x" for ghv repo view`},
		{[]string{"repo", "view", "-repo", "o/r"}, `unknown flag "-repo" for ghv repo view`},
		{[]string{"repo", "view", "-R"}, `flag -R needs a value`},
		{[]string{"repo", "view", "a/b", "c/d"}, "ghv repo view takes at most 1 argument"},
		{[]string{"repo", "view", "-R", "a/b", "-R", "c/d"}, "flag -R given twice"},
		{[]string{"repo", "view", "--json=true"}, "flag --json takes no value"},
		{[]string{"api"}, "ghv api needs PATH"},
		{[]string{"api", "x", "--limit", "0"}, "flag --limit needs a positive number"},
		{[]string{"api", "x", "--limit", "ten"}, "flag --limit needs a positive number"},
	}
	for _, tt := range tests {
		t.Run(strings.Join(tt.args, " "), func(t *testing.T) {
			_, err := parse(testCmds, tt.args)
			var ue usageError
			if !errors.As(err, &ue) || ue.Error() != tt.msg {
				t.Fatalf("err = %v, want usage error %q", err, tt.msg)
			}
		})
	}
}

func TestParseHelp(t *testing.T) {
	for _, args := range [][]string{{"--help"}, {"-h"}, {"help"}, {"repo", "view", "--help"}, {"api", "x", "-h"}} {
		_, err := parse(testCmds, args)
		var hr helpRequest
		if !errors.As(err, &hr) {
			t.Fatalf("%q: err = %v, want help", args, err)
		}
	}
}

func TestHelpText(t *testing.T) {
	top := helpText(testCmds, nil)
	for _, want := range []string{"Usage: ghv <command> [flags]", "  api PATH", "  api graphql", "  repo view [R]"} {
		if !strings.Contains(top, want) {
			t.Errorf("top help lacks %q:\n%s", want, top)
		}
	}
	sub := helpText(testCmds, testCmds[0])
	for _, want := range []string{"Usage: ghv repo view [R] [flags]", "-R, --repo OWNER/REPO", "--json"} {
		if !strings.Contains(sub, want) {
			t.Errorf("repo view help lacks %q:\n%s", want, sub)
		}
	}
}
