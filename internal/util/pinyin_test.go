package util

import "testing"

func TestNameToPinyin(t *testing.T) {
	cases := []struct {
		name string
		want string
	}{
		{"文国荣", "wen_guorong"},
		{"闫鹏", "yan_peng"},
		{"欧阳", "ou_yang"},
		{"Mac", "mac"},
		{"", ""},
		{"  文国荣  ", "wen_guorong"},
	}
	for _, tc := range cases {
		if got := NameToPinyin(tc.name); got != tc.want {
			t.Errorf("NameToPinyin(%q) = %q, want %q", tc.name, got, tc.want)
		}
	}
}

func TestNameToPinyinPolyphoneFallsBack(t *testing.T) {
	// Polyphone surnames use the library default reading; the guarantee is a
	// stable lowercase a-z (plus optional underscore) result.
	got := NameToPinyin("单")
	if got == "" {
		t.Fatalf("NameToPinyin(单) returned empty")
	}
	for _, r := range got {
		if (r < 'a' || r > 'z') && r != '_' {
			t.Fatalf("NameToPinyin(单) = %q contains rune %q outside lowercase a-z/_", got, r)
		}
	}
}
