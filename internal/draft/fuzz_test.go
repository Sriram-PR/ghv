package draft

import (
	"os"
	"path/filepath"
	"testing"
)

func FuzzDecode(f *testing.F) {
	files, _ := filepath.Glob(filepath.Join("testdata", "golden", "*.json"))
	for _, p := range files {
		data, err := os.ReadFile(p)
		if err != nil {
			f.Fatal(err)
		}
		f.Add(data)
	}
	f.Add([]byte(`{"schema":1,"kind":"comment","kind":"x"}`))
	f.Fuzz(func(t *testing.T, data []byte) {
		d, err := Decode(data)
		if err != nil {
			return
		}
		if err := Validate(d); err != nil {
			t.Fatalf("Decode accepted a draft Validate rejects: %v", err)
		}
		if d.ID != "" {
			t.Fatalf("Decode set ID %q", d.ID)
		}
		enc, err := Encode(d)
		if err != nil {
			t.Fatalf("Encode of a decoded draft: %v", err)
		}
		again, err := Decode(enc)
		if err != nil {
			t.Fatalf("Decode(Encode(d)): %v\n%s", err, enc)
		}
		if !equalDraft(d, again) {
			t.Fatalf("round trip changed the draft:\n%+v\n%+v", d, again)
		}
	})
}
