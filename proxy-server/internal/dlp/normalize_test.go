package dlp

import "testing"

func TestNormalizeStripsInvisibles(t *testing.T) {
	// Build the input from explicit runes so the source file stays pure ASCII:
	// zero-width space (U+200B), BOM (U+FEFF), and a bidi override (U+202E)
	// embedded in a secret-ish string.
	in := "AK" + string(rune(0x200B)) + "IA" + string(rune(0xFEFF)) + "EX" + string(rune(0x202E)) + "AMPLE"
	got := Normalize(in)
	want := "AKIAEXAMPLE"
	if got != want {
		t.Fatalf("Normalize = %q, want %q", got, want)
	}
}

func TestNormalizeNFKC(t *testing.T) {
	// Fullwidth letters should fold to ASCII under NFKC.
	in := string([]rune{0xFF33, 0xFF45, 0xFF43, 0xFF52, 0xFF45, 0xFF54}) // "Secret" fullwidth
	got := Normalize(in)
	if got != "Secret" {
		t.Fatalf("Normalize NFKC = %q, want %q", got, "Secret")
	}
}

func TestNormalizeKeepsNewlinesAndTabs(t *testing.T) {
	in := "line1\n\tline2"
	if got := Normalize(in); got != in {
		t.Fatalf("Normalize = %q, want %q", got, in)
	}
}
