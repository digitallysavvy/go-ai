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

// TestR96provbRawChunkForbiddenPrototype (cr from provg, retired known
// difference "JSON payloads with __proto__"): TS parseJsonEventStream's
// safeParseJSON fails on a __proto__ or constructor.prototype key at any
// depth, so the raw part's rawValue is undefined.
func TestR96provbRawChunkForbiddenPrototype(t *testing.T) {
	for _, data := range []string{
		`{"__proto__":{"x":1}}`,
		`{"a":{"constructor":{"prototype":{}}}}`,
		`[{"__proto__":1}]`,
	} {
		if v := RawChunkValue(data); v != nil {
			t.Errorf("RawChunkValue(%s) = %#v, want nil", data, v)
		}
	}
	if v := RawChunkValue(`{"constructor":"x","a":1}`); v == nil {
		t.Error("a plain constructor key must still parse")
	}
}
