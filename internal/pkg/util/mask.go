package util

import (
	"strings"
	"unicode/utf8"
)

// MaskEmail keeps the first character and the domain, e.g. "nobody@mkp.test" -> "n***@mkp.test",
// so a log shows which account was targeted without storing the full address. The mask has a fixed
// width, so it does not reveal how long the address is.
func MaskEmail(email string) string {
	local, domain, ok := strings.Cut(email, "@")
	if !ok || local == "" {
		return "***"
	}
	first, _ := utf8.DecodeRuneInString(local)
	return string(first) + "***@" + domain
}
