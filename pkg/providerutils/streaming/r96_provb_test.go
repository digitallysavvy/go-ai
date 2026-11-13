package streaming

import "testing"

// TestR96provbFinishMetadataJSONStringify: the finish providerMetadata is
// written like JSON.stringify: <, > and & in the provider key stay literal
// (main ruling 4, write side).
func TestR96provbFinishMetadataJSONStringify(t *testing.T) {
	s := &OpenAICompatStream{MetadataKey: "<a&b>"}
	if raw := s.finishMetadata(nil); string(raw) != `{"<a&b>":{}}` {
		t.Fatalf("finishMetadata = %s", raw)
	}
}
