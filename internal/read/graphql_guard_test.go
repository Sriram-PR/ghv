package read

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestGuardAdmitsBuiltQuery(t *testing.T) {
	req, err := NewGraphQLRequest(context.Background(),
		"query($o:String!){ repositoryOwner(login:$o){ login } }", map[string]any{"o": "octo"})
	if err != nil {
		t.Fatal(err)
	}
	rec := &recorder{}
	if _, err := (Guard{Next: rec}).RoundTrip(req); err != nil {
		t.Fatal(err)
	}
	if len(rec.reqs) != 1 {
		t.Fatalf("sent %d requests, want 1", len(rec.reqs))
	}
	got, _ := io.ReadAll(rec.reqs[0].Body)
	want := `{"query":"query($o:String!){ repositoryOwner(login:$o){ login } }","variables":{"o":"octo"}}`
	if string(got) != want {
		t.Fatalf("sent body %s, want %s", got, want)
	}
}

func TestNewGraphQLRequestRefusesMutation(t *testing.T) {
	_, err := NewGraphQLRequest(context.Background(), "mutation { addStar(input:{starrableId:\"x\"}) { clientMutationId } }", nil)
	if !errors.Is(err, ErrReadOnly) {
		t.Fatalf("err = %v, want ErrReadOnly", err)
	}
}

func TestGuardRefusesGraphQLBodies(t *testing.T) {
	for _, body := range []string{
		`{"query":"mutation { addStar(input:{starrableId:\"x\"}) { clientMutationId } }"}`,
		`{"query":"{ viewer { login } }","query":"mutation { x }"}`,
		`{"query":"mutation { x }","query":"{ viewer { login } }"}`,
		`{"query":"{ viewer { login } }","operationName":"x"}`,
		`{ "query":"{ viewer { login } }"}`,
		`{"QUERY":"{ viewer { login } }"}`,
		`{"query":"{ viewer { login } }"}{}`,
		`{"query":"{ viewer { login } }"} `,
		`[{"query":"{ viewer { login } }"}]`,
		`{"query":"{ viewer { login } }","variables":{ "x":1}}`,
		``,
	} {
		t.Run(body, func(t *testing.T) {
			req, _ := http.NewRequest("POST", "https://api.github.com/graphql", strings.NewReader(body))
			refused(t, req)
		})
	}
}

func TestGuardRefusesOtherPOSTs(t *testing.T) {
	const body = `{"query":"{ viewer { login } }"}`
	for _, u := range []string{
		"https://api.github.com/repos/o/r/issues",
		"https://api.github.com/graphql?x=1",
		"https://api.github.com/graphql/",
		"https://github.com/graphql",
		"https://uploads.github.com/graphql",
	} {
		t.Run(u, func(t *testing.T) {
			req, _ := http.NewRequest("POST", u, strings.NewReader(body))
			refused(t, req)
		})
	}
}

func TestGuardRefusesEncodedGraphQLBody(t *testing.T) {
	req, err := NewGraphQLRequest(context.Background(), "{ viewer { login } }", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("Content-Encoding", "gzip")
	refused(t, req)
}

func TestGuardRefusesOversizeGraphQLBody(t *testing.T) {
	req, err := NewGraphQLRequest(context.Background(), "{ a } #"+strings.Repeat("x", 1<<20), nil)
	if err != nil {
		t.Fatal(err)
	}
	refused(t, req)
}
