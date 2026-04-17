package normalize_test

import (
	"testing"

	"github.com/guerra/go-palace/pkg/normalize"
)

func TestNormalize(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"empty", "", ""},
		{"whitespace_only", "   \t\n  ", ""},
		{"trim_and_collapse", "  hello   world  ", "hello world"},
		{"paragraph_preserved", "a\n\n\nb", "a\n\nb"},
		{"crlf_to_space", "hello\r\nworld", "hello world"},
		{"nfc_roundtrip_cafe", "café", "café"},
		{"invalid_utf8_replaced", "bad\xff byte", "bad\uFFFD byte"},
		{"emoji_preserved", "hi 😀 there", "hi 😀 there"},
		{"tabs_collapsed", "a\t\tb", "a b"},
		{"mixed_whitespace", "a\t\t b", "a b"},
		{"multi_paragraph_kept", "first paragraph\n\nsecond paragraph", "first paragraph\n\nsecond paragraph"},
		{"paragraph_internal_newlines", "line1\nline2\n\nline3", "line1 line2\n\nline3"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := normalize.Normalize(tc.in)
			if got != tc.want {
				t.Errorf("Normalize(%q) = %q; want %q", tc.in, got, tc.want)
			}
		})
	}
}
