package netgsm

import (
	"strings"
	"unicode"
)

// normalizeTRMobile converts Identity-canonical or common TR mobile forms to
// the 10-digit national mobile Netgsm REST v2 expects (5XXXXXXXXX).
// It does not invent a user-visible format and does not accept non-TR numbers.
func normalizeTRMobile(raw string) (string, bool) {
	var b strings.Builder
	b.Grow(len(raw))
	for _, r := range strings.TrimSpace(raw) {
		if unicode.IsSpace(r) || r == '-' || r == '(' || r == ')' {
			continue
		}
		if r == '+' {
			if b.Len() != 0 {
				return "", false
			}
			continue
		}
		if r < '0' || r > '9' {
			return "", false
		}
		b.WriteByte(byte(r))
	}
	digits := b.String()
	switch {
	case len(digits) == 12 && strings.HasPrefix(digits, "90"):
		digits = digits[2:]
	case len(digits) == 11 && strings.HasPrefix(digits, "0"):
		digits = digits[1:]
	}
	if len(digits) != 10 || digits[0] != '5' {
		return "", false
	}
	if digits[1] < '0' || digits[1] > '9' {
		return "", false
	}
	return digits, true
}
