package util

import (
	"strings"
	"unicode/utf8"
)

// MaskEmail keeps the first character and domain ("n***@mkp.test"); the fixed mask hides the length.
func MaskEmail(email string) string {
	local, domain, ok := strings.Cut(email, "@")
	if !ok || local == "" {
		return "***"
	}
	first, _ := utf8.DecodeRuneInString(local)
	return string(first) + "***@" + domain
}
