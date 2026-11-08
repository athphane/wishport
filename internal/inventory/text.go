package inventory

import (
	"strings"
	"unicode"
)

// DisplayText prevents metadata from issuing terminal controls or bidi overrides.
func DisplayText(s string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || unicode.Is(unicode.Cf, r) || r == '\u2028' || r == '\u2029' {
			return -1
		}
		return r
	}, s)
}
