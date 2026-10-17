package read

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
)

// pages serves n pages of two numbered items each, linked with rel="next".
func pages(t *testing.T, n int, search bool, queries *[]string) *Client {
	t.Helper()
	return fakeClient("", func(w http.ResponseWriter, r *http.Request) {
		*queries = append(*queries, r.URL.RawQuery)
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		if page == 0 {
			page = 1
		}
		if page < n {
			next := fmt.Sprintf(`<https://api.github.com%s?page=%d&per_page=2>; rel="next", <https://api.github.com%s?page=%d>; rel="last"`, r.URL.Path, page+1, r.URL.Path, n)
			w.Header().Set("Link", next)
		}
		items := fmt.Sprintf("[%d,%d]", 2*page-1, 2*page)
		if search {
			fmt.Fprintf(w, `{"total_count":%d,"incomplete_results":false,"items":%s}`, 2*n, items)
			return
		}
		io.WriteString(w, items)
	})
}

func ints(t *testing.T, items []json.RawMessage) string {
	t.Helper()
	var s []string
	for _, it := range items {
		s = append(s, string(it))
	}
	return strings.Join(s, ",")
}

func TestPaginate(t *testing.T) {
	var q []string
	items, err := pages(t, 3, false, &q).Paginate(context.Background(), "repos/o/r/pulls?state=open", "", 5)
	if err != nil {
		t.Fatal(err)
	}
	if got := ints(t, items); got != "1,2,3,4,5" {
		t.Fatalf("items %s", got)
	}
	if len(q) != 3 || q[0] != "state=open&per_page=5" {
		t.Fatalf("queries %q", q)
	}
}

func TestPaginateAllPages(t *testing.T) {
	var q []string
	items, err := pages(t, 3, false, &q).Paginate(context.Background(), "repos/o/r/pulls", "", DefaultLimit)
	if err != nil || ints(t, items) != "1,2,3,4,5,6" {
		t.Fatalf("items %s, err %v", ints(t, items), err)
	}
	if q[0] != "per_page=100" {
		t.Fatalf("first query %q", q[0])
	}
}

func TestPaginateSearchItems(t *testing.T) {
	var q []string
	items, err := pages(t, 2, true, &q).Paginate(context.Background(), "search/issues?q=is:pr&per_page=2", "", 10)
	if err != nil || ints(t, items) != "1,2,3,4" {
		t.Fatalf("items %s, err %v", ints(t, items), err)
	}
	if q[0] != "q=is:pr&per_page=2" {
		t.Fatalf("per_page added twice: %q", q[0])
	}
}

func TestPaginateRefuses(t *testing.T) {
	tests := []struct {
		name, link, body string
	}{
		{"next link off api.github.com", `<https://evil.example/x?page=2>; rel="next"`, `[1]`},
		{"next link over http", `<http://api.github.com/x?page=2>; rel="next"`, `[1]`},
		{"not a list", ``, `{"id":1}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			c := fakeClient("", func(w http.ResponseWriter, r *http.Request) {
				if tt.link != "" {
					w.Header().Set("Link", tt.link)
				}
				io.WriteString(w, tt.body)
			})
			if _, err := c.Paginate(context.Background(), "x", "", 10); err == nil {
				t.Fatal("no error")
			}
		})
	}
}

func TestPaginateStopsOnEmptyPage(t *testing.T) {
	calls := 0
	c := fakeClient("", func(w http.ResponseWriter, r *http.Request) {
		calls++
		w.Header().Set("Link", `<https://api.github.com/x?page=2>; rel="next"`)
		io.WriteString(w, `[]`)
	})
	items, err := c.Paginate(context.Background(), "x", "", 10)
	if err != nil || len(items) != 0 || calls != 1 {
		t.Fatalf("items %d, err %v, calls %d", len(items), err, calls)
	}
}

func TestNextLink(t *testing.T) {
	tests := []struct{ header, want string }{
		{``, ``},
		{`<https://api.github.com/r?page=3>; rel="last"`, ``},
		{`<https://api.github.com/r?page=1>; rel="prev", <https://api.github.com/r?page=3>; rel="next"`, `/r?page=3`},
		{`<https://api.github.com/r?page=2>; rel="next last"`, `/r?page=2`},
	}
	for _, tt := range tests {
		got, err := nextLink(tt.header)
		if err != nil || got != tt.want {
			t.Errorf("nextLink(%q) = %q, %v; want %q", tt.header, got, err, tt.want)
		}
	}
}
