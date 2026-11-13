package zodlite

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestR97integValidateWithinUsesRootOrder: ValidateWithin validates a member
// of a parsed envelope with the envelope's raw JSON key order (Tencent Cloud
// `Response`), so a record issue names the first bad key on the wire.
func TestR97integValidateWithinUsesRootOrder(t *testing.T) {
	data := []byte(`{"Response":{"z":"x","a":"y"}}`)
	var root map[string]interface{}
	if err := json.Unmarshal(data, &root); err != nil {
		t.Fatal(err)
	}
	bad := ValidateWithin(RecordOf(Number), data, root, root["Response"])
	if bad == nil || len(bad.Path) == 0 || bad.Path[0] != "z" {
		t.Fatalf("issue = %+v, want the z key first", bad)
	}
	if direct := RecordOf(Number)(root["Response"], true, nil); direct == nil || direct.Path[0] != "a" {
		t.Fatalf("direct issue = %+v, want the sorted fallback", direct)
	}
}

// TestR97integValidationMessagePlain: ValidationMessagePlain is
// ValidationMessage for a value TS reads with plain JSON.parse (@vercel/sandbox
// OIDC payload): a __proto__ key is no failure, and the schema still runs.
func TestR97integValidationMessagePlain(t *testing.T) {
	schema := Object(F("owner_id", String))
	if msg := ValidationMessagePlain(schema, `{"__proto__":{},"owner_id":"o"}`); msg != "" {
		t.Fatalf("plain = %q", msg)
	}
	if msg := ValidationMessage(schema, `{"__proto__":{},"owner_id":"o"}`); msg == "" {
		t.Fatal("secure ValidationMessage accepted a __proto__ key")
	}
	if msg := ValidationMessagePlain(schema, `{"__proto__":{}}`); !strings.Contains(msg, `"owner_id"`) {
		t.Fatalf("plain missing owner_id = %q", msg)
	}
}
