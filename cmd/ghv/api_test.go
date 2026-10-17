package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func TestAPIGet(t *testing.T) {
	var auth string
	e, _, _ := testEnv(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		w.Header().Set("X-Oauth-Scopes", "")
		if r.URL.Path != "/repos/o/r" {
			w.WriteHeader(404)
			return
		}
		io.WriteString(w, `{"full_name":"o/r","description":"\u001b]52;c;x\u0007 \u202eevil"}`)
	}))
	setToken(t, e, "ghp_x")
	r := runWith(t, registry, e, "api", "repos/o/r")
	want := "{\n  \"full_name\": \"o/r\",\n  \"description\": \"\\u001b]52;c;x\\u0007 \\u202eevil\"\n}\n"
	if r.code != 0 || r.stdout != want {
		t.Fatalf("exit %d, stdout %q, stderr %q", r.code, r.stdout, r.stderr)
	}
	if auth != "Bearer ghp_x" {
		t.Fatalf("Authorization %q", auth)
	}
}

func TestAPIEscapesRawUnicodeInGitHubJSON(t *testing.T) {
	e, _, _ := testEnv(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "{\"t\":\"\u202eevil\u200b\"}")
	}))
	r := runWith(t, registry, e, "api", "repos/o/r")
	if r.code != 0 || strings.ContainsAny(r.stdout, "\u202e\u200b") || !strings.Contains(r.stdout, `\u202eevil\u200b`) {
		t.Fatalf("stdout %q", r.stdout)
	}
}

func TestAPINonJSONIsSanitised(t *testing.T) {
	e, _, _ := testEnv(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		io.WriteString(w, "plain \x1b[31mred")
	}))
	r := runWith(t, registry, e, "api", "repos/o/r")
	if r.code != 0 || r.stdout != "plain <U+001B>[31mred" {
		t.Fatalf("stdout %q", r.stdout)
	}
}

func TestAPIRefusesURLs(t *testing.T) {
	for _, p := range []string{"//evil.example/x", "https://evil.example/", "@evil.example", "x#y"} {
		e, _, _ := testEnv(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Fatal("request sent") }))
		r := runWith(t, registry, e, "api", p)
		if r.code != 2 || !strings.Contains(r.stderr, "give a path") && !strings.Contains(r.stderr, "does not stay") {
			t.Fatalf("%q: exit %d, stderr %q", p, r.code, r.stderr)
		}
	}
	e, _, _ := testEnv(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Fatal("request sent") }))
	if r := runWith(t, registry, e, "api", "graphql/"); r.code != 1 || !strings.Contains(r.stderr, "read-only") {
		t.Fatalf("GET /graphql: exit %d, stderr %q", r.code, r.stderr)
	}
}

func TestAPIPaginate(t *testing.T) {
	e, _, _ := testEnv(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page, _ := strconv.Atoi(r.URL.Query().Get("page"))
		if page == 0 {
			page = 1
		}
		if page < 3 {
			w.Header().Set("Link", fmt.Sprintf(`<https://api.github.com/repos/o/r/pulls?page=%d>; rel="next"`, page+1))
		}
		fmt.Fprintf(w, `[{"n":%d},{"n":%d}]`, 2*page-1, 2*page)
	}))
	r := runWith(t, registry, e, "api", "repos/o/r/pulls", "--paginate", "--limit", "5")
	var got []struct{ N int }
	if err := json.Unmarshal([]byte(r.stdout), &got); err != nil || len(got) != 5 || got[4].N != 5 {
		t.Fatalf("exit %d, stdout %q", r.code, r.stdout)
	}
}

func TestAPIGraphQL(t *testing.T) {
	var body string
	e, _, _ := testEnv(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		body = string(b)
		w.Header().Set("X-Oauth-Scopes", "")
		io.WriteString(w, `{"data":{"repository":{"name":"r"}}}`)
	}))
	setToken(t, e, "ghp_x")
	q := filepath.Join(e.dir, "q.graphql")
	os.WriteFile(q, []byte("query($o: String!, $n: Int, $b: Boolean, $z: String) { repository(owner: $o, name: \"r\") { name } }"), 0o600)
	os.WriteFile(filepath.Join(e.dir, "v.txt"), []byte("from file"), 0o600)
	r := runWith(t, registry, e, "api", "graphql", "-q", "@q.graphql", "-F", "o=me", "-F", "n=5", "-F", "b=true", "-F", "z=null", "-F", "f=@v.txt")
	if r.code != 0 || r.stdout != "{\n  \"data\": {\n    \"repository\": {\n      \"name\": \"r\"\n    }\n  }\n}\n" {
		t.Fatalf("exit %d, stdout %q, stderr %q", r.code, r.stdout, r.stderr)
	}
	var sent struct{ Variables map[string]any }
	json.Unmarshal([]byte(body), &sent)
	v := sent.Variables
	if v["o"] != "me" || v["n"] != float64(5) || v["b"] != true || v["z"] != nil || v["f"] != "from file" || len(v) != 5 {
		t.Fatalf("variables %v", v)
	}
}

func TestAPIGraphQLRefusals(t *testing.T) {
	tests := []struct {
		args []string
		exit int
		msg  string
	}{
		{[]string{"api", "graphql"}, 2, "needs -q"},
		{[]string{"api", "graphql", "-q", "mutation { x }"}, 1, "ghv is read-only"},
		{[]string{"api", "graphql", "-q", "{ x }", "-F", "novalue"}, 2, "NAME=VALUE"},
		{[]string{"api", "graphql", "-q", "{ x }", "-F", "1a=x"}, 2, "NAME=VALUE"},
		{[]string{"api", "graphql", "-q", "{ x }", "-F", "a=1", "-F", "a=2"}, 2, "given twice"},
		{[]string{"api", "graphql", "-q", "@missing.graphql"}, 2, "missing.graphql"},
	}
	for _, tt := range tests {
		t.Run(strings.Join(tt.args, " "), func(t *testing.T) {
			e, _, _ := testEnv(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { t.Fatal("request sent") }))
			r := runWith(t, registry, e, tt.args...)
			if r.code != tt.exit || !strings.Contains(r.stderr, tt.msg) {
				t.Fatalf("exit %d, stderr %q", r.code, r.stderr)
			}
		})
	}
}
