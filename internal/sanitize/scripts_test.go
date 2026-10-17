package sanitize

import (
	"slices"
	"testing"
)

func TestMixedScripts(t *testing.T) {
	tests := []struct {
		name, in string
		want     []string
	}{
		{"latin", "plain English words", nil},
		{"one other script", "привет мир Καλημέρα", nil},
		{"cyrillic a in latin word", "pаypal login", []string{"body:1:1: mixed-script pаypal (Cyrillic+Latin)"}},
		{"greek omicron", "gοogle", []string{"body:1:1: mixed-script gοogle (Greek+Latin)"}},
		{"words split on punctuation", "pаy-pal ok", []string{"body:1:1: mixed-script pаy (Cyrillic+Latin)"}},
		{"digits and marks are common", "v2 café naïve", nil},
		{"japanese", "iPhoneを使う カタカナと漢字", nil},
		{"chinese with bopomofo", "注音ㄅㄆㄇ", nil},
		{"korean", "한국어漢字 Seoul서울", nil},
		{"japanese and hangul", "かな한글", []string{"body:1:1: mixed-script かな한글 (Hangul+Hiragana)"}},
		{"second line", "ok\nxyzа", []string{"body:2:1: mixed-script xyzа (Cyrillic+Latin)"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := strs(mixedScripts("body", tt.in)); !slices.Equal(got, tt.want) {
				t.Errorf("mixedScripts(%q) =\n%q\nwant\n%q", tt.in, got, tt.want)
			}
		})
	}
}
