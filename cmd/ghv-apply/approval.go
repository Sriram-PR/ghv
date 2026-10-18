package main

import (
	"fmt"
	"strings"

	"github.com/Sriram-PR/ghv/internal/sanitize"
)

// verdict is what stands between a draft and sending it (Blocks) and what
// the reviewer should weigh first (Flags).
type verdict struct{ Blocks, Flags []string }

// judge combines the sanitiser's report and the live context. A finding of
// a rejected class blocks the draft; the rest are counted by class.
func judge(rep sanitize.Report, rc reviewContext) verdict {
	var v verdict
	var classes []sanitize.Class
	count := map[sanitize.Class]int{}
	for _, f := range rep.Findings {
		if f.Class.Rejects() {
			v.Blocks = append(v.Blocks, "the draft holds a character ghv-apply refuses: "+f.String())
			continue
		}
		if count[f.Class] == 0 {
			classes = append(classes, f.Class)
		}
		count[f.Class]++
	}
	v.Blocks = append(v.Blocks, rc.Blocks...)
	v.Flags = append(v.Flags, rc.Flags...)
	if len(classes) > 0 {
		parts := make([]string, len(classes))
		for i, c := range classes {
			parts[i] = fmt.Sprintf("%s %d", c, count[c])
		}
		v.Flags = append(v.Flags, "findings to read: "+strings.Join(parts, ", "))
	}
	return v
}

func renderVerdict(v verdict) []string {
	var out []string
	if len(v.Blocks) > 0 {
		out = append(out, "ghv-apply will not send this draft:")
		for _, b := range v.Blocks {
			out = append(out, "  - "+b)
		}
	}
	if len(v.Flags) > 0 {
		out = append(out, "Check before deciding:")
		for _, f := range v.Flags {
			out = append(out, "  - "+f)
		}
	}
	if out == nil {
		out = []string{"Nothing flagged."}
	}
	return out
}

// action is one answer at the review prompt, named by its first letter.
type action byte

const (
	actApprove action = 'a'
	actEdit    action = 'e'
	actReject  action = 'r'
	actSkip    action = 's'
	actDiff    action = 'd'
	actQuit    action = 'q'
)

var actionWords = map[action]string{actApprove: "approve", actEdit: "edit", actReject: "reject", actSkip: "skip", actDiff: "diff", actQuit: "quit"}

// offered lists the answers for one draft: [a]pprove only when nothing
// blocks sending it, [e]dit when it has text, [d]iff when there is a diff
// to page.
func offered(canSend, canEdit, hasDiff bool) []action {
	var as []action
	if canSend {
		as = append(as, actApprove)
	}
	if canEdit {
		as = append(as, actEdit)
	}
	as = append(as, actReject, actSkip)
	if hasDiff {
		as = append(as, actDiff)
	}
	return append(as, actQuit)
}

func prompt(as []action) string {
	parts := make([]string, len(as))
	for i, a := range as {
		w := actionWords[a]
		parts[i] = "[" + w[:1] + "]" + w[1:]
	}
	return strings.Join(parts[:len(parts)-1], ", ") + ", or " + parts[len(parts)-1] + "? "
}

// parseAction accepts a letter or the whole word. An empty answer is no
// answer: nothing happens by default.
func parseAction(answer string, as []action) (action, bool) {
	a := strings.ToLower(strings.TrimSpace(answer))
	for _, x := range as {
		if a == string(rune(x)) || a == actionWords[x] {
			return x, true
		}
	}
	return 0, false
}
