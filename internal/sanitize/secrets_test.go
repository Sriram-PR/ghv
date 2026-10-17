package sanitize

import (
	"slices"
	"strings"
	"testing"
)

func TestSecrets(t *testing.T) {
	tests := []struct {
		name, in string
		want     []string
	}{
		{"none", "ordinary text with a sha f6459ee889aa7778a14b7224677b7610a2cbe83c", nil},
		{"classic token", "token: ghp_a1B2c3D4e5F6g7H8i9J0k1L2m3N4o5P6q7R8", []string{"body:1:8: secret ghp_a1... (40 chars) (GitHub token prefix)"}},
		{"every prefix", "gho_x ghu_x ghs_x ghr_x github_pat_11AB", []string{
			"body:1:1: secret gho_x (GitHub token prefix)",
			"body:1:7: secret ghu_x (GitHub token prefix)",
			"body:1:13: secret ghs_x (GitHub token prefix)",
			"body:1:19: secret ghr_x (GitHub token prefix)",
			"body:1:25: secret github... (15 chars) (GitHub token prefix)",
		}},
		{"prefix inside a word", "xghp_abc", nil},
		{"pem", "key:\n-----BEGIN OPENSSH PRIVATE KEY-----\nb3BlbnNzaC1rZXktdjEAAAAABG5vbmUAAAAEbm9uZQAAAAAAAAABAAAAMwAAAAtzc2gtZW\n-----END OPENSSH PRIVATE KEY-----\n", []string{
			"body:2:1: secret -----BEGIN OPENSSH PRIVATE KEY----- (PEM block)",
		}},
		{"high entropy", "aws: wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY12", []string{"body:1:6: secret wJalrX... (42 chars) (high-entropy string)"}},
		{"base64 blob", "data " + "SGVsbG8sIHRoaXMgaXMgYSBsb25nIGJhc2U2NCBibG9iIDEyMw==", []string{"body:1:6: secret SGVsbG... (52 chars) (high-entropy string)"}},
		{"identifier", "TestQueueWriteRefusesWhatReadFileRejects2", nil},
		{"path", "user/repo/blob/0123456789abcdef/internal/x", nil},
		{"hex digest", "sha256:" + strings.Repeat("ab12", 16), nil},
		{"short", "abc123XYZ", nil},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := strs(secrets("body", tt.in)); !slices.Equal(got, tt.want) {
				t.Errorf("secrets(%q) =\n%q\nwant\n%q", tt.in, got, tt.want)
			}
		})
	}
}
