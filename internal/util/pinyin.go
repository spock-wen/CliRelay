package util

import (
	"strings"
	"unicode"

	"github.com/mozillazg/go-pinyin"
)

// NameToPinyin converts a Chinese full name to lowercase pinyin with the
// surname separated from the given name by an underscore:
//
//	文国荣 -> wen_guorong
//	闫鹏   -> yan_peng
//
// The first character is treated as the surname and the remaining characters
// as the given name, whose syllables are concatenated without a separator.
// Names containing non-Chinese characters are returned lowercased as-is
// (e.g. "Mac" -> "mac"). Polyphone readings fall back to the library default.
func NameToPinyin(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	// Check non-CJK names before calling the pinyin library: LazyPinyin
	// returns no syllables at all for input without Chinese characters, so
	// the empty-slice guard below must not pre-empt the as-is lowercasing.
	if !allCJK(name) {
		return strings.ToLower(name)
	}
	syllables := pinyin.LazyPinyin(name, pinyin.NewArgs())
	if len(syllables) == 0 {
		return ""
	}
	if len(syllables) == 1 {
		return syllables[0]
	}
	return syllables[0] + "_" + strings.Join(syllables[1:], "")
}

// allCJK reports whether every rune in s is a CJK unified ideograph.
func allCJK(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if !unicode.Is(unicode.Han, r) {
			return false
		}
	}
	return true
}
