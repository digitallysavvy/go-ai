package harness

import "testing"

func TestDecodeStreamPartRejectsNullMetadataEntry(t *testing.T) {
	bad := []string{
		`{"type":"text-start","id":"a","harnessMetadata":{"claude":null}}`,
		`{"type":"finish-step","finishReason":{"unified":"stop"},"usage":{"inputTokens":{},"outputTokens":{}},"harnessMetadata":{"h":null}}`,
		`{"type":"tool-input-start","id":"a","toolName":"t","providerMetadata":{"p":null}}`,
		`{"type":"text-start","id":"a","harnessMetadata":{"claude":"x"}}`,
	}
	for _, in := range bad {
		if _, err := DecodeStreamPart([]byte(in)); err == nil {
			t.Errorf("expected error for %s", in)
		}
	}
	// nested null values inside an entry are valid JSON values
	p, err := DecodeStreamPart([]byte(`{"type":"text-start","id":"a","harnessMetadata":{"claude":{"k":null}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := p.(*TextStartPart).HarnessMetadata["claude"]["k"]; !ok {
		t.Fatal("inner null value lost")
	}
}
