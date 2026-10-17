package main

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/Sriram-PR/ghv/internal/read"
	"github.com/Sriram-PR/ghv/internal/sanitize"
)

const maxQueryFile = 1 << 20

var graphQLNameRE = regexp.MustCompile(`^[_A-Za-z][_0-9A-Za-z]*$`)

func init() {
	register(&command{
		path: "api",
		args: []string{"PATH"},
		flags: []flag{
			{name: "paginate", kind: boolFlag, help: `follow rel="next" links and print one JSON array`},
			limitFlag,
		},
		help: "GET a REST path on api.github.com, such as repos/OWNER/REPO, and print the JSON",
		run:  runAPI,
	})
	register(&command{
		path: "api graphql",
		flags: []flag{
			{name: "query", short: "q", kind: stringFlag, arg: "QUERY", help: "query document, or @FILE to read it from a file"},
			{name: "field", short: "F", kind: multiFlag, arg: "NAME=VALUE", help: "variable: true, false, null and integers are typed, @FILE reads a file, anything else is a string"},
		},
		help: "Run a GraphQL query; mutations and subscriptions are refused",
		run:  runGraphQL,
	})
}

func runAPI(ctx context.Context, a *app) error {
	p := a.inv.args[0]
	if _, err := read.APIURL(p); err != nil {
		return usageError{err.Error()}
	}
	c, err := a.read()
	if err != nil {
		return err
	}
	if a.inv.bool("paginate") {
		items, err := c.Paginate(ctx, p, "", a.inv.int("limit"))
		if err != nil {
			return err
		}
		if items == nil {
			items = []json.RawMessage{}
		}
		b, err := json.Marshal(items)
		if err != nil {
			return err
		}
		return writeRaw(a.stdout, b)
	}
	resp, err := c.Get(ctx, p, "")
	if err != nil {
		return err
	}
	return writeRaw(a.stdout, resp.Body)
}

// writeRaw prints a GitHub response body: indented and escaped if it is
// JSON, sanitised text otherwise.
func writeRaw(w io.Writer, body []byte) error {
	if !json.Valid(body) {
		_, err := io.WriteString(w, sanitize.Terminal(string(body)))
		return err
	}
	var buf bytes.Buffer
	if err := json.Indent(&buf, body, "", "  "); err != nil {
		return err
	}
	buf.WriteByte('\n')
	_, err := w.Write(escapeJSON(buf.Bytes()))
	return err
}

func runGraphQL(ctx context.Context, a *app) error {
	q := a.inv.str("query")
	if q == "" {
		return usagef("ghv api graphql needs -q QUERY or -q @FILE")
	}
	query, err := a.argValue(q)
	if err != nil {
		return err
	}
	vars := map[string]any{}
	for _, f := range a.inv.list("field") {
		name, raw, ok := cutField(f)
		if !ok {
			return usagef("-F %q is not NAME=VALUE", f)
		}
		if _, dup := vars[name]; dup {
			return usagef("-F %s given twice", name)
		}
		if vars[name], err = a.fieldValue(raw); err != nil {
			return err
		}
	}
	c, err := a.read()
	if err != nil {
		return err
	}
	resp, err := c.GraphQL(ctx, query, vars)
	if err != nil {
		return err
	}
	return writeRaw(a.stdout, resp.Body)
}

func cutField(f string) (string, string, bool) {
	name, val, ok := strings.Cut(f, "=")
	if !ok || !graphQLNameRE.MatchString(name) {
		return "", "", false
	}
	return name, val, true
}

func (a *app) fieldValue(raw string) (any, error) {
	switch raw {
	case "true":
		return true, nil
	case "false":
		return false, nil
	case "null":
		return nil, nil
	}
	if n, err := strconv.ParseInt(raw, 10, 64); err == nil {
		return n, nil
	}
	return a.argValue(raw)
}

// argValue reads @FILE, relative to the working directory, or returns s.
func (a *app) argValue(s string) (string, error) {
	name, ok := cutAt(s)
	if !ok {
		return s, nil
	}
	if !filepath.IsAbs(name) {
		name = filepath.Join(a.dir, name)
	}
	f, err := os.Open(name)
	if err != nil {
		return "", usageError{err.Error()}
	}
	defer f.Close()
	b, err := io.ReadAll(io.LimitReader(f, maxQueryFile+1))
	if err != nil {
		return "", err
	}
	if len(b) > maxQueryFile {
		return "", usagef("%s is over %d bytes", name, maxQueryFile)
	}
	return string(b), nil
}

func cutAt(s string) (string, bool) {
	if len(s) > 1 && s[0] == '@' {
		return s[1:], true
	}
	return "", false
}
