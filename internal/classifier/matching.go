package classifier

import (
	"strings"
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

// punctuationRunes lists characters treated as punctuation/whitespace separators
// by NormalizeAccountName. It covers the common symbols found in financial
// institution names (.,()[]-_'"*&/) plus a few extras.
var punctuationRunes = map[rune]struct{}{
	'.': {}, ',': {}, '(': {}, ')': {}, '[': {}, ']': {}, '-': {}, '_': {},
	'\'': {}, '"': {}, '*': {}, '&': {}, '/': {}, '\\': {}, ':': {}, ';': {},
	'!': {}, '?': {}, '+': {}, '=': {}, '#': {}, '@': {}, '$': {}, '%': {},
	'^': {}, '|': {}, '~': {}, '`': {}, '{': {}, '}': {}, '<': {}, '>': {},
}

// NormalizeAccountName returns a cleaned, lowercased, diacritic-stripped,
// punctuation-collapsed form of an account name suitable for fuzzy comparison.
func NormalizeAccountName(s string) string {
	s = strings.ToLower(s)
	// Decompose Unicode characters and remove marks (diacritics/accents).
	t := transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)
	s, _, _ = transform.String(t, s)

	// Replace punctuation with spaces.
	var b strings.Builder
	for _, r := range s {
		if _, ok := punctuationRunes[r]; ok {
			b.WriteRune(' ')
		} else {
			b.WriteRune(r)
		}
	}
	// Collapse whitespace and trim.
	return strings.Join(strings.Fields(b.String()), " ")
}

// NormalizeAccountNameAlphaNum returns a lowercased, diacritic-stripped,
// non-alphanumeric-stripped form of an account name. Only letters and digits
// are kept, so names like "S.A.R.L." and "SARL" become identical.
func NormalizeAccountNameAlphaNum(s string) string {
	s = strings.ToLower(s)
	t := transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)
	s, _, _ = transform.String(t, s)

	var b strings.Builder
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsNumber(r) {
			b.WriteRune(r)
		}
	}
	return b.String()
}

// MatchAccountCandidate finds the best matching account candidate for name
// using the priority order required by destination matching:
//   1. Exact case-insensitive match (strings.EqualFold).
//   2. Clean normalized match (lowercase, diacritics removed, punctuation
//      stripped and whitespace collapsed).
//   3. Alphanumeric-only normalized match (lowercase, non-alphanumeric
//      characters stripped).
//
// It returns the matched candidate unchanged so callers can use its canonical
// Name and ID.
func MatchAccountCandidate(name string, candidates []AccountCandidate) (AccountCandidate, bool) {
	// Step 1: exact case-insensitive.
	for _, c := range candidates {
		if strings.EqualFold(c.Name, name) {
			return c, true
		}
	}

	// Step 2: clean normalized.
	cleanName := NormalizeAccountName(name)
	for _, c := range candidates {
		if NormalizeAccountName(c.Name) == cleanName {
			return c, true
		}
	}

	// Step 3: alphanumeric normalized.
	alphaName := NormalizeAccountNameAlphaNum(name)
	if alphaName == "" {
		return AccountCandidate{}, false
	}
	for _, c := range candidates {
		if NormalizeAccountNameAlphaNum(c.Name) == alphaName {
			return c, true
		}
	}

	return AccountCandidate{}, false
}
