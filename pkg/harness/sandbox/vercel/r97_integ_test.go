package vercel

import (
	"encoding/base64"
	"strings"
	"testing"
)

// TestR97integOIDCPayloadKeepsPrototypeKey: @vercel/sandbox 3.9.0 decodes
// the OIDC token payload with plain JSON.parse (decode-base64-url.js:9, then
// schema.parse; api-client.js:75 decodeUnverifiedToken), so a __proto__ key
// is an ordinary property, not a parse failure.
func TestR97integOIDCPayloadKeepsPrototypeKey(t *testing.T) {
	payload := `{"__proto__":{"a":1},"constructor":{"prototype":{}},"owner_id":"team_1","project_id":"prj_1"}`
	token := "h." + base64.RawURLEncoding.EncodeToString([]byte(payload)) + ".s"
	owner, project, err := decodeOIDCClaims(token)
	if err != nil || owner != "team_1" || project != "prj_1" {
		t.Fatalf("owner = %q, project = %q, err = %v", owner, project, err)
	}
	gotOwner, gotProject, ok := unverifiedTokenOwner(token)
	if !ok || gotOwner != "team_1" || gotProject != "prj_1" {
		t.Fatalf("unverified owner = %v, project = %q, ok = %v", gotOwner, gotProject, ok)
	}
	// The schema still runs: a payload without owner_id fails with zod's text.
	bad := "h." + base64.RawURLEncoding.EncodeToString([]byte(`{"__proto__":{},"project_id":"prj_1"}`)) + ".s"
	if _, _, err := decodeOIDCClaims(bad); err == nil || !strings.Contains(err.Error(), `"owner_id"`) {
		t.Fatalf("err = %v, want the owner_id issue", err)
	}
}
