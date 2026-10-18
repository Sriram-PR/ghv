package main

import (
	"context"
	"fmt"
	"slices"
	"strings"

	"github.com/Sriram-PR/ghv/internal/audit"
	"github.com/Sriram-PR/ghv/internal/sanitize"
)

func init() {
	commands = append(commands, command{name: "log", flags: []string{"--verify"},
		help: "show the audit log of sent drafts; --verify checks it against your public events on GitHub", run: runLog})
}

func runLog(ctx context.Context, e env) error {
	es, err := audit.Read(audit.Path(e.home))
	if err != nil {
		return err
	}
	if slices.Contains(e.args, "--verify") {
		return verify(ctx, e, es)
	}
	if len(es) == 0 {
		fmt.Fprintln(e.stdout, "The audit log is empty.")
		return nil
	}
	rows := make([][]string, len(es))
	for i, en := range es {
		url := en.URL
		if en.Found {
			url += " (found later)"
		}
		rows[i] = []string{en.Time.UTC().Format("2006-01-02 15:04 UTC"), en.Draft, en.Kind, en.Repo, url}
	}
	writeTable(e, rows)
	return nil
}

// writeTable prints rows in columns two spaces apart, every cell through
// sanitize.Line: the agent's user can edit the log.
func writeTable(e env, rows [][]string) {
	var width []int
	for _, r := range rows {
		for i, c := range r {
			r[i] = sanitize.Line(c)
			if i >= len(width) {
				width = append(width, 0)
			}
			width[i] = max(width[i], len([]rune(r[i])))
		}
	}
	for _, r := range rows {
		var b strings.Builder
		for i, c := range r {
			b.WriteString(c)
			if i < len(r)-1 {
				b.WriteString(strings.Repeat(" ", width[i]-len([]rune(c))+2))
			}
		}
		fmt.Fprintln(e.stdout, b.String())
	}
}
