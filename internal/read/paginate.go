package read

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

const (
	DefaultLimit = 1000
	maxPerPage   = 100
	maxPages     = 1000
)

// Paginate GETs path and follows rel="next" links until it has limit
// items. A page is a JSON array, or an object holding one under a key in
// listKeys, as the search, checks, commit status, and Actions APIs return.
func (c *Client) Paginate(ctx context.Context, path, accept string, limit int) ([]json.RawMessage, error) {
	return c.PaginateFunc(ctx, path, accept, limit, nil)
}

// PaginateFunc is Paginate keeping only the items keep accepts; limit
// counts kept items. A nil keep keeps every item.
func (c *Client) PaginateFunc(ctx context.Context, path, accept string, limit int, keep func(json.RawMessage) bool) ([]json.RawMessage, error) {
	if limit <= 0 {
		return nil, errors.New("limit must be positive")
	}
	perPage := min(limit, maxPerPage)
	if keep != nil {
		perPage = maxPerPage
	}
	p := withPerPage(path, perPage)
	var items []json.RawMessage
	for range maxPages {
		resp, err := c.Get(ctx, p, accept)
		if err != nil {
			return nil, err
		}
		page, err := pageItems(resp.Body)
		if err != nil {
			return nil, err
		}
		for _, it := range page {
			if keep == nil || keep(it) {
				items = append(items, it)
			}
		}
		if len(items) >= limit {
			return items[:limit], nil
		}
		if p, err = nextLink(resp.Header.Get("Link")); err != nil {
			return nil, err
		}
		if p == "" || len(page) == 0 {
			return items, nil
		}
	}
	return items, nil
}

func withPerPage(path string, n int) string {
	_, query, hasQuery := strings.Cut(path, "?")
	for kv := range strings.SplitSeq(query, "&") {
		if strings.HasPrefix(kv, "per_page=") {
			return path
		}
	}
	sep := "?"
	if hasQuery {
		sep = "&"
	}
	return fmt.Sprintf("%s%sper_page=%d", path, sep, n)
}

var listKeys = []string{"items", "check_runs", "statuses", "workflow_runs", "jobs", "artifacts"}

func pageItems(body []byte) ([]json.RawMessage, error) {
	var items []json.RawMessage
	if json.Unmarshal(body, &items) == nil {
		return items, nil
	}
	var env map[string]json.RawMessage
	if json.Unmarshal(body, &env) == nil {
		for _, k := range listKeys {
			if raw, ok := env[k]; ok && json.Unmarshal(raw, &items) == nil && items != nil {
				return items, nil
			}
		}
	}
	return nil, errors.New("GitHub's response is not a list, so it cannot be paginated")
}

// nextLink returns the path of the rel="next" target in a Link header, or
// "" on the last page. A target off https://api.github.com is an error.
func nextLink(header string) (string, error) {
	for part := range strings.SplitSeq(header, ",") {
		target, params, ok := strings.Cut(strings.TrimSpace(part), ";")
		if !ok || !strings.HasPrefix(target, "<") || !strings.HasSuffix(target, ">") {
			continue
		}
		if !hasRelNext(params) {
			continue
		}
		u, err := url.Parse(target[1 : len(target)-1])
		if err != nil || u.Scheme != "https" || u.Host != apiHost || u.User != nil {
			return "", fmt.Errorf("pagination link %q leaves https://%s", target, apiHost)
		}
		return u.RequestURI(), nil
	}
	return "", nil
}

func hasRelNext(params string) bool {
	for p := range strings.SplitSeq(params, ";") {
		k, v, ok := strings.Cut(strings.TrimSpace(p), "=")
		if !ok || !strings.EqualFold(k, "rel") {
			continue
		}
		for rel := range strings.FieldsSeq(strings.Trim(v, `"`)) {
			if rel == "next" {
				return true
			}
		}
	}
	return false
}
