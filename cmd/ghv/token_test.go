package main

import (
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
)

const rateBody = `{"resources":{"core":{"limit":5000,"used":43,"remaining":4957,"reset":1792328400},"search":{"limit":30,"used":0,"remaining":30,"reset":1792324860},"graphql":{"limit":5000,"used":0,"remaining":5000,"reset":1792328400}},"rate":{"limit":5000,"used":43,"remaining":4957,"reset":1792328400}}`

func rateServer(t *testing.T, header http.Header, calls *int) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*calls++
		if r.URL.Path != "/rate_limit" {
			t.Errorf("unexpected %s", r.URL)
		}
		for k, v := range header {
			w.Header()[k] = v
		}
		io.WriteString(w, rateBody)
	})
}

func TestRate(t *testing.T) {
	calls := 0
	e, _, _ := testEnv(t, rateServer(t, nil, &calls))
	r := runWith(t, registry, e, "rate")
	want := "RESOURCE  USED   LIMIT  REMAINING  RESETS\n" +
		"core        43    5000       4957  in 1h0m (2026-10-18T13:00:00Z)\n" +
		"graphql      0    5000       5000  in 1h0m (2026-10-18T13:00:00Z)\n" +
		"search       0      30         30  in 1m (2026-10-18T12:01:00Z)\n"
	if r.code != 0 || r.stdout != want {
		t.Fatalf("exit %d, stdout:\n%s\nwant:\n%s\nstderr %q", r.code, r.stdout, want, r.stderr)
	}
	e, _, _ = testEnv(t, rateServer(t, nil, &calls))
	r = runWith(t, registry, e, "rate", "--json")
	var got struct {
		Authenticated bool `json:"authenticated"`
		Resources     []struct {
			Name                   string
			Limit, Used, Remaining int
			Reset                  string
		} `json:"resources"`
	}
	if err := json.Unmarshal([]byte(r.stdout), &got); err != nil || got.Authenticated || len(got.Resources) != 3 ||
		got.Resources[0].Name != "core" || got.Resources[2].Reset != "2026-10-18T12:01:00Z" {
		t.Fatalf("json %q: %v", r.stdout, err)
	}
}

func TestToken(t *testing.T) {
	tests := []struct {
		name, token string
		header      http.Header
		exit        int
		lines       []string
		verdict     string
	}{
		{"classic no scopes", "ghp_x", http.Header{"X-Oauth-Scopes": {""}}, 0,
			[]string{"type     classic", "scopes   none", "expires  never", "verdict  read-only: a classic token with no scopes can only read public data"}, "read-only"},
		{"fine-grained", "github_pat_x", http.Header{"Github-Authentication-Token-Expiration": {"2026-11-05 14:03:13 UTC"}}, 0,
			[]string{"type     fine-grained", "scopes   not shown for fine-grained tokens", "expires  2026-11-05T14:03:13Z (in 18 days)", "verdict  unverified: GitHub does not show"}, "unverified"},
		{"classic with scopes", "ghp_x", http.Header{"X-Oauth-Scopes": {"repo, gist"}}, 3,
			[]string{"scopes   repo, gist", "verdict  refused: the token has scopes"}, "refused"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			calls := 0
			e, _, _ := testEnv(t, rateServer(t, tt.header, &calls))
			setToken(t, e, tt.token)
			r := runWith(t, registry, e, "token")
			if r.code != tt.exit || calls != 1 || !strings.HasPrefix(r.stdout, "token    "+e.tokenPath()+"\n") {
				t.Fatalf("exit %d, calls %d, stdout:\n%s\nstderr %q", r.code, calls, r.stdout, r.stderr)
			}
			for _, l := range tt.lines {
				if !strings.Contains(r.stdout, "\n"+l) {
					t.Errorf("stdout lacks %q:\n%s", l, r.stdout)
				}
			}
			e, _, _ = testEnv(t, rateServer(t, tt.header, &calls))
			setToken(t, e, tt.token)
			r = runWith(t, registry, e, "token", "--json")
			var got map[string]any
			if err := json.Unmarshal([]byte(r.stdout), &got); err != nil || got["verdict"] != tt.verdict || got["path"] != e.tokenPath() {
				t.Fatalf("json %q: %v", r.stdout, err)
			}
		})
	}
}

func TestTokenAnonymousSendsNothing(t *testing.T) {
	calls := 0
	e, _, _ := testEnv(t, rateServer(t, nil, &calls))
	r := runWith(t, registry, e, "token")
	if r.code != 0 || calls != 0 || !strings.Contains(r.stdout, "verdict  anonymous: no read token") || strings.Contains(r.stdout, "scopes") {
		t.Fatalf("exit %d, calls %d, stdout %q", r.code, calls, r.stdout)
	}
	e, _, _ = testEnv(t, rateServer(t, nil, &calls))
	r = runWith(t, registry, e, "token", "--json")
	if !strings.Contains(r.stdout, `"type": "none"`) || !strings.Contains(r.stdout, `"scopes": null`) {
		t.Fatalf("json %q", r.stdout)
	}
}

func TestTokenPathIsSanitised(t *testing.T) {
	calls := 0
	e, _, _ := testEnv(t, rateServer(t, nil, &calls))
	e.getenv = func(k string) string {
		if k == "XDG_CONFIG_HOME" {
			return "/x\x1b]8;;https://evil\x07y"
		}
		return ""
	}
	r := runWith(t, registry, e, "token")
	if r.code != 0 || strings.ContainsAny(r.stdout, "\x1b\x07") {
		t.Fatalf("exit %d, stdout %q", r.code, r.stdout)
	}
}
