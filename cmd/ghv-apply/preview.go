package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"github.com/Sriram-PR/ghv/internal/draft"
	"github.com/Sriram-PR/ghv/internal/harden"
	"github.com/Sriram-PR/ghv/internal/sanitize"
	"github.com/Sriram-PR/ghv/internal/write"
)

// renderDraft is the draft as the reviewer reads it: raw markdown escaped
// by sanitize, the findings, every link target, the request when there is
// one, and an end marker with counts, so nothing can hide past the end of
// the screen.
func renderDraft(l draft.Loaded, rep sanitize.Report, req *write.Request) []string {
	d := l.Draft
	var out, counts []string
	add := func(format string, a ...any) { out = append(out, fmt.Sprintf(format, a...)) }
	add("Draft %s: %s to %s", d.ID, d.Kind(), sanitize.Line(d.Repo))
	add("Created %s; file SHA-256 %x", d.CreatedAt.UTC().Format("2006-01-02 15:04 UTC"), l.SHA256)
	add("")
	name := func(key, v string) { add("%s: %s", key, sanitize.Line(v)) }
	title := func(v string) {
		n := plural(utf8.RuneCountInString(v), "character", "characters")
		add("title (%s): %s", n, sanitize.Line(v))
		counts = append(counts, "title "+n)
	}
	body := func(v string) {
		if v == "" {
			add("body: (empty)")
			counts = append(counts, "body empty")
			return
		}
		add("body (%s):", textCounts(v))
		for s := range strings.SplitSeq(sanitize.Terminal(v), "\n") {
			out = append(out, "    "+s)
		}
		counts = append(counts, "body "+textCounts(v))
	}
	switch f := d.Fields.(type) {
	case draft.PRCreateFields:
		name("base", f.Base)
		name("head", f.Head)
		name("head_sha", f.HeadSHA)
		add("draft: %t", f.Draft)
		add("maintainer_can_modify: %t", f.MaintainerCanModify)
		title(f.Title)
		body(f.Body)
	case draft.PREditFields:
		add("number: %d", f.Number)
		if f.Title != nil {
			title(*f.Title)
		} else {
			add("title: (not changed)")
		}
		if f.Body != nil {
			body(*f.Body)
		} else {
			add("body: (not changed)")
		}
	case draft.CommentFields:
		add("number: %d", f.Number)
		body(f.Body)
	case draft.ReviewReplyFields:
		add("number: %d", f.Number)
		add("comment_id: %d", f.CommentID)
		body(f.Body)
	case draft.IssueCreateFields:
		title(f.Title)
		body(f.Body)
	}
	if len(rep.Findings) > 0 {
		add("")
		add("Findings (%d):", len(rep.Findings))
		for _, f := range rep.Findings {
			add("  %s", f)
		}
	}
	if len(rep.Links) > 0 {
		add("Links (%d):", len(rep.Links))
		for _, u := range rep.Links {
			add("  %s", sanitize.Line(u))
		}
	}
	if req != nil {
		add("")
		out = append(out, renderRequest(*req)...)
	}
	if len(counts) == 0 {
		add("-- end of %s --", d.ID)
	} else {
		add("-- end of %s: %s --", d.ID, strings.Join(counts, "; "))
	}
	return out
}

func textCounts(v string) string {
	return plural(strings.Count(v, "\n")+1, "line", "lines") + ", " + plural(utf8.RuneCountInString(v), "character", "characters")
}

// renderRequest shows the request as it will be sent: the method, the URL,
// and every JSON field in order. The title and body point at the text
// shown above, which is the same value.
func renderRequest(r write.Request) []string {
	out := []string{fmt.Sprintf("Request: %s https://%s%s", r.Method, harden.APIHost, sanitize.Line(r.Path))}
	dec := json.NewDecoder(bytes.NewReader(r.Body))
	if t, err := dec.Token(); err != nil || t != json.Delim('{') {
		return append(out, "  (the body is not a JSON object)")
	}
	n := 0
	for dec.More() {
		t, err := dec.Token()
		key, ok := t.(string)
		var raw json.RawMessage
		if err != nil || !ok || dec.Decode(&raw) != nil {
			return append(out, "  (the body is not a JSON object)")
		}
		n++
		var text string
		switch {
		case (key == "title" || key == "body") && json.Unmarshal(raw, &text) == nil && text == "":
			out = append(out, fmt.Sprintf("  %s: (empty)", key))
		case key == "title" && json.Unmarshal(raw, &text) == nil:
			out = append(out, fmt.Sprintf("  title: (shown above, %s)", plural(utf8.RuneCountInString(text), "character", "characters")))
		case key == "body" && json.Unmarshal(raw, &text) == nil:
			out = append(out, fmt.Sprintf("  body: (shown above, %s)", textCounts(text)))
		default:
			out = append(out, fmt.Sprintf("  %s: %s", sanitize.Line(key), sanitize.Line(string(raw))))
		}
	}
	if n == 0 {
		out = append(out, "  (no fields)")
	}
	return append(out, "  request SHA-256 "+r.SHA256())
}

// page shows lines a screen at a time. Enter shows the next screen; q skips
// to the last line, which is always shown.
func page(tty terminal, lines []string) error {
	h := tty.Height() - 1
	if h < 1 || len(lines) <= h+1 {
		for _, l := range lines {
			tty.Printf("%s\n", l)
		}
		return nil
	}
	for i := 0; i < len(lines); {
		end := min(i+h, len(lines))
		for _, l := range lines[i:end] {
			tty.Printf("%s\n", l)
		}
		i = end
		if i == len(lines) {
			return nil
		}
		ans, err := tty.ReadLine(fmt.Sprintf("-- line %d of %d: Enter for more, q to skip to the end -- ", i, len(lines)))
		if err != nil {
			return err
		}
		if strings.EqualFold(strings.TrimSpace(ans), "q") {
			if skipped := len(lines) - 1 - i; skipped > 0 {
				tty.Printf("(%s skipped)\n", plural(skipped, "line", "lines"))
			}
			tty.Printf("%s\n", lines[len(lines)-1])
			return nil
		}
	}
	return nil
}
