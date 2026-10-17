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
// items. A page is a JSON array, or an object whose "items" is one, as
// the search API returns.
func (c *Client) Paginate(ctx context.Context, path, accept string, limit int) ([]json.RawMessage, error) {
	if limit <= 0 {
		return nil, errors.New("limit must be positive")
	}
	p := withPerPage(path, min(limit, maxPerPage))
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
		items = append(items, page...)
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

func pageItems(body []byte) ([]json.RawMessage, error) {
	var items []json.RawMessage
	if json.Unmarshal(body, &items) == nil {
		return items, nil
	}
	var env struct {
		Items *[]json.RawMessage `json:"items"`
	}
	if json.Unmarshal(body, &env) == nil && env.Items != nil {
		return *env.Items, nil
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
