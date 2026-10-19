package doctor

import (
	"context"
	"testing"
)

func TestEnvTokens(t *testing.T) {
	tok := "ghp_" + "planted"
	proc := fakeProc(t, map[string]string{
		"100/environ": "PATH=/bin\x00GH_TOKEN=" + tok + "\x00",
		"100/comm":    "bash\n",
		"101/environ": "GITHUB_TOKEN=\x00",
		"102/environ": "GH_TOKEN=" + tok + "\x00",
		"200/environ": "GH_TOKEN=" + tok + "\x00",
		"300/environ": "GH_TOKEN=" + tok + "\x00",
		"self/x":      "",
	})
	env := map[string]string{"GITHUB_TOKEN": tok}
	e := &Env{PID: 200, EnvPID: 300, Proc: proc, Getenv: func(k string) string { return env[k] }, EnvOf: "this process's environment"}
	want := "HIGH GITHUB_TOKEN is set in this process's environment\n" +
		"HIGH GH_TOKEN is set in 2 other processes: pid 100 (bash), pid 102\n"
	if got := details(envTokens(context.Background(), e)); got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
	e = &Env{GOOS: "darwin"}
	if got := details(envTokens(context.Background(), e)); got != "INFO other processes' environments not checked: no /proc on darwin\n" {
		t.Fatalf("got %q", got)
	}
}

func TestList(t *testing.T) {
	if got := list([]string{"a", "b", "c", "d", "e", "f", "g"}); got != "a, b, c, d, e and 2 more" {
		t.Fatal(got)
	}
}

func TestProxyVars(t *testing.T) {
	env := map[string]string{"HTTPS_PROXY": "http://user:secret@proxy", "SSL_CERT_FILE": "/x.pem"}
	e := &Env{Getenv: func(k string) string { return env[k] }, EnvOf: "the shell that started ghv-apply (pid 7)"}
	want := "INFO HTTPS_PROXY, SSL_CERT_FILE set in the shell that started ghv-apply (pid 7): ghv-apply ignores it; other tools send their traffic through it\n"
	if got := details(proxyVars(context.Background(), e)); got != want {
		t.Fatalf("got %q", got)
	}
	if got := proxyVars(context.Background(), &Env{}); got != nil {
		t.Fatal(got)
	}
}
