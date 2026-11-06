package vercel

import "testing"

// TS utils.ts isSandboxNameConflictError: /sandbox.*name.*already exists/i
// without the u or s flag, so `.` stops at \r, U+2028 and U+2029 (not
// U+0085) and s never matches U+017F. Expected values from node v24 running
// the TS regex.
func TestR86btoolsSandboxNameConflictPatternMatchesJS(t *testing.T) {
	for msg, want := range map[string]bool{
		"Sandbox name already exists":      true,
		"ſandbox name already exists":      false,
		"sandbox name\ralready exists":     false,
		"sandbox name already exists":      false,
		"SANDBOX NAME ALREADY EXISTſ":      false,
		"sandbox name\u0085already exists": true,
	} {
		if got := sandboxNameConflictPattern.MatchString(msg); got != want {
			t.Errorf("%q: match = %v, want %v", msg, got, want)
		}
	}
}
