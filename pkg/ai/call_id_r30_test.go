package ai

import (
	"regexp"
	"testing"
)

// Every operation's callId is "call-" + 24 alphanumerics (TS
// createIdGenerator({prefix: 'call', size: 24})).
func TestNewCallIDShape(t *testing.T) {
	re := regexp.MustCompile(`^call-[0-9A-Za-z]{24}$`)
	seen := map[string]bool{}
	for i := 0; i < 50; i++ {
		id := newCallID()
		if !re.MatchString(id) {
			t.Fatalf("newCallID() = %q", id)
		}
		seen[id] = true
	}
	if len(seen) != 50 {
		t.Fatalf("call IDs collided: %d unique of 50", len(seen))
	}
}
