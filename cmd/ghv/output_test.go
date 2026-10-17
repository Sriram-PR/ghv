package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestWriteJSONEscapesWhatTerminalWould(t *testing.T) {
	v := map[string]string{"t": "a\x1b]52;c;x\x07 \u009b \u202eevil \u200b del\x7f \U000E0041 ok é 🙂 \u2764\ufe0f"}
	var b bytes.Buffer
	if err := writeJSON(&b, v); err != nil {
		t.Fatal(err)
	}
	out := b.String()
	for _, r := range out {
		if r == '\n' {
			continue
		}
		if r < 0x20 || r == 0x7f || (r >= 0x80 && r <= 0x9f) || r == 0x202e || r == 0x200b || r == 0xfe0f || r >= 0xE0000 {
			t.Fatalf("raw %U in output %q", r, out)
		}
	}
	for _, want := range []string{`\u001b`, `\u009b`, `\u202e`, `\u200b`, `\u007f`, `\udb40\udc41`, `\ufe0f`, "é 🙂 \u2764"} {
		if !strings.Contains(out, want) {
			t.Errorf("output lacks %q: %s", want, out)
		}
	}
	var back map[string]string
	if err := json.Unmarshal(b.Bytes(), &back); err != nil || back["t"] != v["t"] {
		t.Fatalf("round trip lost data: %q, %v", back["t"], err)
	}
}

func TestEscapeJSONDropsRawCR(t *testing.T) {
	in := []byte("{\r\n  \"a\": \"\u202e\"\r\n}")
	got := escapeJSON(in)
	if bytes.ContainsRune(got, '\r') || !json.Valid(got) || !bytes.Contains(got, []byte(`\u202e`)) {
		t.Fatalf("got %q", got)
	}
}
