package prompt

import (
	"encoding/json"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS reads part.providerOptions.google.thoughtSignature from the replayed
// metadata object as is; a number beyond the double range elsewhere in it
// is Infinity there (ruling 4). encoding/json failed the whole decode and
// dropped the signature.
func TestR99provgGoogleReplayMetadataOutOfRangeKeepsSignature(t *testing.T) {
	out, err := ConvertToGoogleMessages([]types.Message{{Role: types.RoleAssistant, Content: []types.ContentPart{
		types.TextContent{Text: "hi", ProviderMetadata: json.RawMessage(`{"google":{"thoughtSignature":"sig","score":1e400}}`)},
	}}}, GoogleMessagesOptions{})
	if err != nil {
		t.Fatal(err)
	}
	parts, _ := out.Contents[0]["parts"].([]map[string]interface{})
	if len(parts) != 1 || parts[0]["thoughtSignature"] != "sig" {
		t.Fatalf("parts = %#v, want the replayed thoughtSignature", parts)
	}
}
