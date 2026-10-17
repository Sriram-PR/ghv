package draft

import (
	"testing"
	"time"
)

func TestEncodeGolden(t *testing.T) {
	for name, d := range golden {
		t.Run(name, func(t *testing.T) {
			got, err := Encode(d)
			if err != nil {
				t.Fatal(err)
			}
			if want := readGolden(t, name); string(got) != string(want) {
				t.Fatalf("Encode =\n%s\nwant\n%s", got, want)
			}
		})
	}
}

func TestEncodeRefusesInvalid(t *testing.T) {
	bad := Draft{Repo: "not a repo", CreatedAt: time.Now(), Fields: ForkFields{}}
	if _, err := Encode(bad); err == nil {
		t.Fatal("Encode accepted an invalid draft")
	}
}
