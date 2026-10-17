package read

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"testing"
)

func TestPaginateEnvelopes(t *testing.T) {
	for _, key := range []string{"items", "check_runs", "workflow_runs", "jobs", "artifacts"} {
		t.Run(key, func(t *testing.T) {
			c := fakeClient("", func(w http.ResponseWriter, r *http.Request) {
				fmt.Fprintf(w, `{"total_count":2,%q:[1,2]}`, key)
			})
			items, err := c.Paginate(context.Background(), "repos/o/r/x", "", 10)
			if err != nil || ints(t, items) != "1,2" {
				t.Fatalf("items %s, err %v", ints(t, items), err)
			}
		})
	}
}

func TestPaginateRefusesUnknownEnvelope(t *testing.T) {
	c := fakeClient("", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"total_count":2,"things":[1,2]}`)
	})
	if _, err := c.Paginate(context.Background(), "repos/o/r/x", "", 10); err == nil {
		t.Fatal("an unknown envelope paginated")
	}
}

func TestPaginateFuncCountsKeptItems(t *testing.T) {
	var q []string
	odd := func(raw json.RawMessage) bool {
		var n int
		json.Unmarshal(raw, &n)
		return n%2 == 1
	}
	items, err := pages(t, 3, false, &q).PaginateFunc(context.Background(), "repos/o/r/issues", "", 2, odd)
	if err != nil || ints(t, items) != "1,3" {
		t.Fatalf("items %s, err %v", ints(t, items), err)
	}
	if len(q) != 2 || q[0] != "per_page=100" {
		t.Fatalf("queries %q", q)
	}
}
