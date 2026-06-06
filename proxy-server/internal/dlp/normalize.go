package dlp

import (
	"strings"

	"golang.org/x/text/unicode/norm"
)

// Normalize canonicalizes text before classification and fingerprinting so that
// the same content always maps to the same cache key, and so that simple
// Unicode obfuscation (homoglyph/compatibility forms, zero-width and bidi
// controls) cannot slip content past the detectors. See plan §"Normalizer".
//
// It applies NFKC compatibility normalization and strips zero-width, bidi, and
// non-printing control characters (keeping tab and newline).
func Normalize(s string) string {
	s = norm.NFKC.String(s)
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if isStrippable(r) {
			continue
		}
		b.WriteRune(r)
	}
	return b.String()
}

func isStrippable(r rune) bool {
	switch {
	case r == '\t' || r == '\n' || r == '\r':
		return false
	case r == 0xFEFF: // BOM / zero-width no-break space
		return true
	case r >= 0x200B && r <= 0x200F: // zero-width + LRM/RLM marks
		return true
	case r >= 0x202A && r <= 0x202E: // bidi embedding/override
		return true
	case r >= 0x2060 && r <= 0x2064: // word joiner + invisible operators
		return true
	case r >= 0x2066 && r <= 0x2069: // bidi isolates
		return true
	case r < 0x20: // other C0 control characters
		return true
	case r == 0x7F: // DEL
		return true
	}
	return false
}
