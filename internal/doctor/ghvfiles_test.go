package doctor

import (
	"context"
	"path/filepath"
	"testing"
)

func TestGhvFiles(t *testing.T) {
	human := t.TempDir()
	plant(t, human, map[string]string{".config/ghv/vault": "sealed"})
	p := func(rel string) string { return filepath.Join(human, rel) }
	vaultInfo := "INFO sriram's vault at " + p(".config/ghv/vault") + " is readable by this user; it is sealed with the passphrase, but the agent should not have it\n"
	for _, c := range []struct {
		name      string
		writable  []string
		want, fix string
	}{
		{"nothing writable", nil, vaultInfo, ""},
		{"the config directory", []string{p(".config")},
			"HIGH sriram's ghv-apply files can be changed by this user: " + p(".config") + "; the agent could delete or replace the vault or the audit log\n" + vaultInfo,
			"chmod go-w " + p(".config")},
		{"the vault and the home, with no audit log yet", []string{human, p(".config/ghv/vault")},
			"HIGH sriram's ghv-apply files can be changed by this user: " + p(".config/ghv/vault") + ", " + human + "; the agent could delete or replace the vault or the audit log\n",
			"chmod go-w " + p(".config/ghv/vault") + " " + human},
	} {
		e := &Env{UID: 1001, Human: &Human{Name: "sriram", UID: 1000, Home: human}, Writable: writableOnly(c.writable...)}
		fs := ghvFiles(context.Background(), e)
		if got := details(fs); got != c.want {
			t.Errorf("%s:\n got %q\nwant %q", c.name, got, c.want)
		}
		if c.fix != "" && fs[0].Fix != c.fix {
			t.Errorf("%s: fix %q", c.name, fs[0].Fix)
		}
	}
	for _, e := range []*Env{
		{UID: 1001, Home: human},
		{UID: 1000, Human: &Human{Name: "sriram", UID: 1000, Home: human}},
	} {
		if fs := ghvFiles(context.Background(), e); fs != nil {
			t.Errorf("own files: %+v", fs)
		}
	}
}
