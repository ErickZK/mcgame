// Input sanitization and XSS defense helpers for player-supplied strings
// such as chat messages, character names, and mail bodies.
package security

import (
	"html"
	"strings"
	"unicode"
)

func SanitizeText(input string, maxRunes int) string {
	var b strings.Builder
	b.Grow(len(input))
	count := 0
	for _, r := range input {
		if r == '\r' || r == '\n' || r == '\t' {
			r = ' '
		} else if unicode.IsControl(r) || r == 0x202e || r == 0x202d || r == 0x200e || r == 0x200f {
			continue
		}
		if maxRunes > 0 && count >= maxRunes {
			break
		}
		b.WriteRune(r)
		count++
	}
	return strings.TrimSpace(html.EscapeString(b.String()))
}

func ValidateUsername(username string) bool {
	if len(username) < 3 || len(username) > 32 {
		return false
	}
	for _, r := range username {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') || r == '_' || r == '-' {
			continue
		}
		return false
	}
	return true
}
