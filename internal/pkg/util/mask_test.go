package util

import "testing"

func TestMaskEmail(t *testing.T) {
	tests := map[string]string{
		"nobody@mkp.test":   "n***@mkp.test",
		"a@mkp.test":        "a***@mkp.test", // one character still gets the fixed-width mask
		"élodie@mkp.test":   "é***@mkp.test", // multi-byte first character stays whole
		"not-an-email":      "***",
		"@mkp.test":         "***",
		"":                  "***",
		"x@y@mkp.test":      "x***@y@mkp.test",
		"customer@mkp.test": "c***@mkp.test",
	}
	for in, want := range tests {
		if got := MaskEmail(in); got != want {
			t.Errorf("MaskEmail(%q) = %q, want %q", in, got, want)
		}
	}
}
