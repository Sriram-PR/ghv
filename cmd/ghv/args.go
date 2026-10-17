package main

import (
	"slices"
	"strconv"
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
