package read

import (
	"context"
	"errors"
	"io"
	"net/http"
	"testing"
)

func TestClientGraphQL(t *testing.T) {
	var bodies []string
	status := []int{502, 200}
	c := fakeClient("ghp_x", func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		bodies = append(bodies, r.Method+" "+r.URL.String()+" "+r.Header.Get("Content-Type")+" "+string(b))
		w.Header().Set("X-Oauth-Scopes", "")
		w.WriteHeader(status[len(bodies)-1])
		io.WriteString(w, `{"data":{"viewer":{"login":"me"}}}`)
	})
	resp, err := c.GraphQL(context.Background(), "query($n: Int) { viewer { login } }", map[string]any{"n": 1})
	if err != nil || string(resp.Body) != `{"data":{"viewer":{"login":"me"}}}` {
		t.Fatalf("resp %v, err %v", resp, err)
	}
	want := `POST https://api.github.com/graphql application/json {"query":"query($n: Int) { viewer { login } }","variables":{"n":1}}`
	if len(bodies) != 2 || bodies[0] != want || bodies[1] != want {
		t.Fatalf("requests:\n%q\nwant twice:\n%q", bodies, want)
	}
}

func TestClientGraphQLRefusesMutationUnsent(t *testing.T) {
	c := fakeClient("", func(w http.ResponseWriter, r *http.Request) { t.Fatal("request sent") })
	_, err := c.GraphQL(context.Background(), "mutation { addStar(input: {}) { clientMutationId } }", nil)
	if !errors.Is(err, ErrReadOnly) {
		t.Fatalf("err = %v", err)
	}
}

func TestClientGraphQLErrorsIn200(t *testing.T) {
	c := fakeClient("", func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, `{"data":null,"errors":[{"message":"Field 'x' doesn't exist"}]}`)
	})
	_, err := c.GraphQL(context.Background(), "{ x }", nil)
	var ae *APIError
	if !errors.As(err, &ae) || ae.Code != CodeGraphQL {
		t.Fatalf("err = %v", err)
	}
}
