package main

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"strings"
)

type flagKind int

const (
	boolFlag flagKind = iota
	stringFlag
	intFlag
	multiFlag
)

type flag struct {
	name  string
	short string
	kind  flagKind
	arg   string
	help  string
	def   string
}

var (
	jsonFlag  = flag{name: "json", kind: boolFlag, help: "print JSON"}
	repoFlag  = flag{name: "repo", short: "R", kind: stringFlag, arg: "OWNER/REPO", help: "repository; default: the origin remote of this checkout"}
	limitFlag = flag{name: "limit", kind: intFlag, arg: "N", help: "stop after N items", def: "1000"}
)

// command is one entry in the CLI table. args names the positional
// arguments; a trailing "?" marks an optional one.
type command struct {
	path  string
	args  []string
	flags []flag
	help  string
	run   func(ctx context.Context, a *app) error
}

func (c *command) allFlags() []flag { return append(slices.Clone(c.flags), jsonFlag) }

func (c *command) usage() string {
	parts := []string{"ghv", c.path}
	for _, a := range c.args {
		if name, ok := strings.CutSuffix(a, "?"); ok {
			parts = append(parts, "["+name+"]")
		} else {
			parts = append(parts, a)
		}
	}
	return strings.Join(parts, " ")
}

type invocation struct {
	cmd    *command
	args   []string
	values map[string][]string
}

func (i *invocation) str(name string) string {
	if v := i.values[name]; len(v) > 0 {
		return v[0]
	}
	return ""
}

func (i *invocation) bool(name string) bool { return i.str(name) == "true" }

func (i *invocation) int(name string) int {
	n, _ := strconv.Atoi(i.str(name))
	return n
}

func (i *invocation) list(name string) []string { return i.values[name] }

type usageError struct{ msg string }

func (e usageError) Error() string { return e.msg }

func usagef(format string, a ...any) error { return usageError{fmt.Sprintf(format, a...)} }

// helpRequest asks for the help of cmd, or of ghv itself when cmd is nil.
type helpRequest struct{ cmd *command }

func (helpRequest) Error() string { return "help requested" }

// parse is pure, so the skill-drift test can check documented command
// lines without running them. Flags may come before, between, or after
// positional arguments; "--" ends the flags.
func parse(cmds []*command, args []string) (*invocation, error) {
	if len(args) == 0 || strings.HasPrefix(args[0], "-") {
		if len(args) > 0 && (args[0] == "-h" || args[0] == "--help") {
			return nil, helpRequest{}
		}
		return nil, usagef("no command given")
	}
	if args[0] == "help" {
		return nil, helpRequest{}
	}
	cmd, rest := lookup(cmds, args)
	if cmd == nil {
		var subs []string
		for _, c := range cmds {
			if sub, ok := strings.CutPrefix(c.path, args[0]+" "); ok {
				subs = append(subs, sub)
			}
		}
		if len(subs) > 0 {
			slices.Sort(subs)
			return nil, usagef("ghv %s needs a subcommand: %s", args[0], strings.Join(subs, ", "))
		}
		return nil, usagef("unknown command %q", args[0])
	}
	inv := &invocation{cmd: cmd, values: map[string][]string{}}
	flags := cmd.allFlags()
	onlyArgs := false
	for i := 0; i < len(rest); i++ {
		tok := rest[i]
		if onlyArgs || tok == "-" || !strings.HasPrefix(tok, "-") {
			inv.args = append(inv.args, tok)
			continue
		}
		if tok == "--" {
			onlyArgs = true
			continue
		}
		if tok == "-h" || tok == "--help" {
			return nil, helpRequest{cmd}
		}
		name, val, hasVal := strings.Cut(tok, "=")
		f, ok := findFlag(flags, name)
		if !ok {
			return nil, usagef("unknown flag %q for ghv %s", name, cmd.path)
		}
		switch {
		case f.kind == boolFlag && hasVal:
			return nil, usagef("flag %s takes no value", name)
		case f.kind == boolFlag:
			val = "true"
		case !hasVal:
			if i+1 >= len(rest) {
				return nil, usagef("flag %s needs a value", name)
			}
			i++
			val = rest[i]
		}
		if f.kind == intFlag {
			if n, err := strconv.Atoi(val); err != nil || n <= 0 {
				return nil, usagef("flag %s needs a positive number", name)
			}
		}
		if _, dup := inv.values[f.name]; dup && f.kind != multiFlag {
			return nil, usagef("flag %s given twice", name)
		}
		inv.values[f.name] = append(inv.values[f.name], val)
	}
	for _, f := range flags {
		if _, set := inv.values[f.name]; !set && f.def != "" {
			inv.values[f.name] = []string{f.def}
		}
	}
	required := 0
	for _, a := range cmd.args {
		if !strings.HasSuffix(a, "?") {
			required++
		}
	}
	switch {
	case len(inv.args) < required:
		return nil, usagef("ghv %s needs %s", cmd.path, strings.Join(cmd.args[len(inv.args):required], " "))
	case len(inv.args) > len(cmd.args):
		return nil, usagef("ghv %s takes at most %d argument%s", cmd.path, len(cmd.args), plural(len(cmd.args)))
	}
	return inv, nil
}

// lookup matches the longest command path at the start of args.
func lookup(cmds []*command, args []string) (*command, []string) {
	for n := min(2, len(args)); n >= 1; n-- {
		key := strings.Join(args[:n], " ")
		for _, c := range cmds {
			if c.path == key {
				return c, args[n:]
			}
		}
	}
	return nil, nil
}

// findFlag accepts only --name and the one-letter short form, so a
// documented flag has no other spelling.
func findFlag(flags []flag, tok string) (flag, bool) {
	for _, f := range flags {
		if tok == "--"+f.name || (f.short != "" && tok == "-"+f.short) {
			return f, true
		}
	}
	return flag{}, false
}

func plural(n int) string {
	if n == 1 {
		return ""
	}
	return "s"
}

func helpText(cmds []*command, cmd *command) string {
	var b strings.Builder
	if cmd == nil {
		b.WriteString("Usage: ghv <command> [flags]\n\nghv reads GitHub. It cannot write to it.\n\nCommands:\n")
		sorted := slices.Clone(cmds)
		slices.SortFunc(sorted, func(x, y *command) int { return strings.Compare(x.path, y.path) })
		for _, c := range sorted {
			fmt.Fprintf(&b, "  %-26s %s\n", strings.TrimPrefix(c.usage(), "ghv "), c.help)
		}
		b.WriteString("\nEvery command takes --json. Run 'ghv <command> --help' for its flags.\n")
		return b.String()
	}
	fmt.Fprintf(&b, "Usage: %s [flags]\n\n", cmd.usage())
	if cmd.help != "" {
		b.WriteString(cmd.help + "\n\n")
	}
	b.WriteString("Flags:\n")
	for _, f := range cmd.allFlags() {
		name := "--" + f.name
		if f.short != "" {
			name = "-" + f.short + ", " + name
		}
		if f.arg != "" {
			name += " " + f.arg
		}
		help := f.help
		if f.def != "" {
			help += " (default " + f.def + ")"
		}
		fmt.Fprintf(&b, "  %-28s %s\n", name, help)
	}
	return b.String()
}
