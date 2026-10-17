package read

import (
	"net/url"
	"testing"
)

func TestAPIURLAccepts(t *testing.T) {
	tests := []struct{ in, want string }{
		{"repos/o/r", "https://api.github.com/repos/o/r"},
		{"/repos/o/r/pulls?state=open&per_page=100", "https://api.github.com/repos/o/r/pulls?state=open&per_page=100"},
		{"rate_limit", "https://api.github.com/rate_limit"},
		{"search/issues?q=is%3Apr+author%3Ame", "https://api.github.com/search/issues?q=is%3Apr+author%3Ame"},
		{"/repos/o/r/contents/a%20b.txt", "https://api.github.com/repos/o/r/contents/a%20b.txt"},
		{"api.github.com.evil.example/x", "https://api.github.com/api.github.com.evil.example/x"},
	}
	for _, tt := range tests {
		u, err := APIURL(tt.in)
		if err != nil {
			t.Errorf("APIURL(%q): %v", tt.in, err)
			continue
		}
		if u.String() != tt.want {
			t.Errorf("APIURL(%q) = %s, want %s", tt.in, u, tt.want)
		}
	}
}

func TestAPIURLRejects(t *testing.T) {
	for _, in := range []string{
		"",
		"https://evil.example/x",
		"http://api.github.com/x",
		"HTTPS://api.github.com/x",
		"//evil.example/x",
		"///evil.example/x",
		"user@evil.example/x",
		"/repos/o/r@evil",
		`\\evil.example\x`,
		"/x#frag",
		"/x y",
		"/x\n",
		"/x\x00",
		"/é",
		"repos/o/r\t",
	} {
		if u, err := APIURL(in); err == nil {
			t.Errorf("APIURL(%q) = %s, want an error", in, u)
		}
	}
}

func FuzzAPIURL(f *testing.F) {
	for _, s := range []string{"repos/o/r", "//evil", "a@b", "/x?y=1", "%2F%2Fevil.example", "/../.."} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, in string) {
		u, err := APIURL(in)
		if err != nil {
			return
		}
		again, err := url.Parse(u.String())
		if err != nil {
			t.Fatalf("APIURL(%q) = %s, which does not re-parse: %v", in, u, err)
		}
		for _, v := range []*url.URL{u, again} {
			if v.Scheme != "https" || v.Host != apiHost || v.User != nil || v.Opaque != "" {
				t.Fatalf("APIURL(%q) left api.github.com: %s", in, v)
			}
		}
	})
}
