package main

import (
	"errors"
	"regexp"
	"strconv"
	"time"
)

var (
	sinceFlag = flag{name: "since", kind: stringFlag, arg: "T", help: "only since T: an RFC 3339 time, YYYY-MM-DD (UTC), or an age such as 90m, 36h, or 7d"}
	ageRE     = regexp.MustCompile(`^([1-9][0-9]{0,5})([mhd])$`)
)

// parseSince reads T as an RFC 3339 time, a UTC date, or an age before now.
func parseSince(s string, now time.Time) (time.Time, error) {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UTC(), nil
	}
	if t, err := time.Parse(time.DateOnly, s); err == nil {
		return t, nil
	}
	m := ageRE.FindStringSubmatch(s)
	if m == nil {
		return time.Time{}, errors.New("not a time, date, or age")
	}
	n, _ := strconv.Atoi(m[1])
	switch m[2] {
	case "m":
		return now.Add(-time.Duration(n) * time.Minute), nil
	case "h":
		return now.Add(-time.Duration(n) * time.Hour), nil
	}
	return now.AddDate(0, 0, -n), nil
}

// since returns --since, or the zero time when it is not given.
func (a *app) since() (time.Time, error) {
	s := a.inv.str("since")
	if s == "" {
		return time.Time{}, nil
	}
	t, err := parseSince(s, a.now())
	if err != nil {
		return time.Time{}, usagef("--since %q: give an RFC 3339 time, YYYY-MM-DD, or an age such as 36h or 7d", s)
	}
	return t, nil
}
