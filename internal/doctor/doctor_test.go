package doctor

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func fixed(fs ...Finding) func(context.Context, *Env) []Finding {
	return func(context.Context, *Env) []Finding { return fs }
}

func TestRunOrdersAndSanitizes(t *testing.T) {
	e := &Env{User: "agent", UID: 1001, Human: &Human{Name: "sriram", UID: 1000}}
	r := run(context.Background(), e, []check{
		{"first", fixed(Finding{Severity: Info, Detail: "an info"}, Finding{Severity: High, Detail: "evil \x1b]52;c;x\x07", Fix: "fix \x1b[2J"})},
		{"second", fixed(Finding{Severity: Medium, Detail: "a medium"}, Finding{Severity: High, Detail: "second high"})},
	})
	var got []string
	for _, f := range r.Findings {
		got = append(got, f.Severity.String()+" "+f.Check+" "+f.Detail+" | "+f.Fix)
	}
	want := []string{
		"HIGH first evil <U+001B>]52;c;x<U+0007> | fix <U+001B>[2J",
		"HIGH second second high | ",
		"MEDIUM second a medium | ",
		"INFO first an info | ",
	}
	if strings.Join(got, "\n") != strings.Join(want, "\n") {
		t.Fatalf("findings:\n%s", strings.Join(got, "\n"))
	}
	if r.Verdict != "Uncontained" || r.High() != 2 || r.HumanUser != "sriram" {
		t.Fatalf("%+v", r)
	}
	var he HighError
	if err := r.Err(); !errors.As(err, &he) || he.N != 2 || err.Error() != "2 HIGH findings" {
		t.Fatalf("err %v", err)
	}
	b, _ := json.Marshal(r.Findings[2])
	if string(b) != `{"check":"second","severity":"MEDIUM","detail":"a medium"}` {
		t.Fatalf("json %s", b)
	}
}

func TestRunContained(t *testing.T) {
	r := run(context.Background(), &Env{User: "agent", UID: 1001}, []check{{"only", fixed(Finding{Severity: Medium, Detail: "m"})}})
	if r.Verdict != "Contained" || r.Err() != nil {
		t.Fatalf("%+v", r)
	}
	b, _ := json.Marshal(run(context.Background(), &Env{User: "agent"}, nil))
	if string(b) != `{"user":"agent","uid":0,"findings":[],"verdict":"Contained"}` {
		t.Fatalf("json %s", b)
	}
}

func TestWriteText(t *testing.T) {
	var b strings.Builder
	r := run(context.Background(), &Env{User: "agent", UID: 1001, Human: &Human{Name: "sriram"}}, []check{
		{"same-user", fixed(Finding{Severity: High, Detail: "runs as the human", Fix: "run it as its own user"})},
		{"proxy", fixed(Finding{Severity: Info, Detail: "HTTPS_PROXY is set"})},
	})
	r.WriteText(&b)
	want := "What this process can reach, running as agent (uid 1001); the human is sriram:\n\n" +
		"HIGH    same-user  runs as the human\n" +
		"                   fix: run it as its own user\n" +
		"INFO    proxy      HTTPS_PROXY is set\n" +
		"\nVerdict: Uncontained (1 HIGH finding). The agent can act as you on GitHub by these routes; fix them, or run it as its own user.\n"
	if b.String() != want {
		t.Fatalf("got:\n%s\nwant:\n%s", b.String(), want)
	}
	b.Reset()
	run(context.Background(), &Env{User: "agent", UID: 1001}, nil).WriteText(&b)
	if b.String() != "What this process can reach, running as agent (uid 1001):\n\nNothing found.\n\nVerdict: Contained (no HIGH findings).\n" {
		t.Fatalf("got:\n%s", b.String())
	}
}
