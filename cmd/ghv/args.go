package main

import (
	"net/url"
	"slices"
	"strconv"
	"strings"
)

// number returns positional argument i as an issue or pull request number.
func (a *app) number(i int) (int, error) {
	s := a.inv.args[i]
	n, err := strconv.Atoi(s)
	if err != nil || n <= 0 || n > 1<<31-1 || strconv.Itoa(n) != s {
		return 0, usagef("%q is not a number", s)
	}
	return n, nil
}

// idArg returns positional argument i as a GitHub ID: a decimal int64 with
// no sign or leading zeros. what names it in the error.
func (a *app) idArg(i int, what string) (int64, error) {
	s := a.inv.args[i]
	n, err := strconv.ParseInt(s, 10, 64)
	if err != nil || n <= 0 || strconv.FormatInt(n, 10) != s {
		return 0, usagef("%q is not a %s", s, what)
	}
	return n, nil
}

func takesFlag(c *command, name string) bool {
	return slices.ContainsFunc(c.flags, func(f flag) bool { return f.name == name })
}

// pathSegment escapes s as one URL path segment. url.PathEscape leaves "@",
// which read.APIURL refuses, so it is escaped too.
func pathSegment(s string) string {
	return strings.ReplaceAll(url.PathEscape(s), "@", "%40")
}
