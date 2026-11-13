// Package atob holds the error Node's atob throws, so every Go twin of TS
// convertBase64ToUint8Array can report the same text.
package atob

// InvalidCharacterError is the DOMException atob throws (name
// InvalidCharacterError) for a string TS convertBase64ToUint8Array cannot
// decode; Error is its message, as Node writes it.
type InvalidCharacterError struct{ Message string }

func (e *InvalidCharacterError) Error() string { return e.Message }

// ErrorName is the DOMException name.
func (e *InvalidCharacterError) ErrorName() string { return "InvalidCharacterError" }

// Error returns the InvalidCharacterError Node's atob throws for s (see
// ErrorMessage). Call it once a decode of s has failed.
func Error(s string) *InvalidCharacterError {
	return &InvalidCharacterError{Message: ErrorMessage(s)}
}

// ErrorMessage is the message Node's atob throws for s after TS
// convertBase64ToUint8Array maps "-" and "_" to "+" and "/": "Invalid
// character" for a character outside the base64 alphabet (ASCII whitespace
// is skipped), an "=" that is not trailing, more than two "=", or padding
// that does not complete a 4-character group; "The string to be decoded is
// not correctly encoded." when the data characters leave one extra
// character. Matched against Node 24 atob on 300,000 generated strings.
// Call it only for a string whose decode failed.
func ErrorMessage(s string) string {
	const invalid = "Invalid character"
	alpha, eq := 0, 0
	for _, r := range s {
		switch {
		case r == ' ' || r == '\t' || r == '\n' || r == '\f' || r == '\r':
			continue
		case r == '=':
			eq++
			if eq > 2 {
				return invalid
			}
			continue
		case r == '-' || r == '_' || r == '+' || r == '/' ||
			(r >= 'A' && r <= 'Z') || (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
		default:
			return invalid
		}
		if eq > 0 {
			return invalid
		}
		alpha++
	}
	if alpha%4 == 1 {
		return "The string to be decoded is not correctly encoded."
	}
	return invalid
}
