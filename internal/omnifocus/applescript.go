package omnifocus

import "strings"

// applescriptQuoteString takes an arbitrary Go string and returns a valid
// AppleScript string literal. AppleScript string literals only require two
// characters to be escaped: backslash (\ → \\) and double quote (" → \").
// Newlines and Unicode pass through unchanged.
//
// Note the escaping order matters: backslashes must be doubled FIRST, then
// double quotes escaped. Doing it the other way around would re-escape the
// backslash that was just added in front of a quote.
func applescriptQuoteString(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}
