package subscription

import (
	"encoding/base64"
	"testing"
)

// safeParseJSON rejects a __proto__ key, so the payload is undefined.
func TestParseJWTPayloadRejectsForbiddenPrototypeKey(t *testing.T) {
	enc := func(s string) string { return base64.RawURLEncoding.EncodeToString([]byte(s)) }
	if p := ParseJWTPayload("h." + enc(`{"exp":1,"__proto__":{}}`) + ".s"); p != nil {
		t.Fatalf("payload = %v", p)
	}
	if p := ParseJWTPayload("h." + enc(`{"exp":1}`) + ".s"); p["exp"] != float64(1) {
		t.Fatalf("control payload = %v", p)
	}
}
