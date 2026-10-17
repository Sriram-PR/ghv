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

func takesFlag(c *command, name string) bool {
	return slices.ContainsFunc(c.flags, func(f flag) bool { return f.name == name })
}

// pathSegment escapes s as one URL path segment. url.PathEscape leaves "@",
// which read.APIURL refuses, so it is escaped too.
func pathSegment(s string) string {
	return strings.ReplaceAll(url.PathEscape(s), "@", "%40")
}
