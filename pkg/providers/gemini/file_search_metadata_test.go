package gemini

import (
	"encoding/json"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS #22175: File Search customMetadata on retrievedContext grounding chunks
// is preserved verbatim in providerMetadata.google.groundingMetadata. Go
// passes the grounding metadata through as raw JSON, so nothing is stripped.
const fileSearchGrounding = `{"groundingChunks":[{"retrievedContext":{"title":"I, Claudius","text":"A historical novel.","fileSearchStore":"fileSearchStores/test-store","customMetadata":[{"key":"author","stringValue":"Robert Graves"},{"key":"year","numericValue":1934},{"key":"genres","stringListValue":{"values":["historical fiction","novel"]}},{"key":"page","numericValue":0},{"key":"description","stringValue":""},{"key":"tags","stringListValue":{"values":[]}}]}}]}`

func TestFileSearchCustomMetadataPreservedNonStreaming(t *testing.T) {
	m := makeTestModel("gemini-2.0-flash")
	var resp Response
	body := `{"candidates":[{"content":{"parts":[{"text":"test response"}],"role":"model"},"finishReason":"STOP","groundingMetadata":` + fileSearchGrounding + `}]}`
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatal(err)
	}
	result := m.convertResponse(resp, nil)
	googleMeta, ok := result.ProviderMetadata["google"].(map[string]json.RawMessage)
	if !ok {
		t.Fatalf("ProviderMetadata[google] type = %T", result.ProviderMetadata["google"])
	}
	assertJSONEqual(t, googleMeta["groundingMetadata"], fileSearchGrounding)
}

func TestFileSearchCustomMetadataPreservedStreaming(t *testing.T) {
	body := `{"candidates":[{"content":{"parts":[{"text":"test"}],"role":"model"},"finishReason":"STOP","groundingMetadata":` + fileSearchGrounding + `}]}`
	s := newTestStream(sseStream(body))
	defer s.Close() //nolint:errcheck
	var finish *provider.StreamChunk
	for {
		c, err := s.Next()
		if err != nil {
			break
		}
		if c.Type == provider.ChunkTypeFinish {
			finish = c
		}
	}
	if finish == nil {
		t.Fatal("no finish chunk")
	}
	var meta map[string]map[string]json.RawMessage
	if err := json.Unmarshal(finish.ProviderMetadata, &meta); err != nil {
		t.Fatal(err)
	}
	assertJSONEqual(t, meta["google"]["groundingMetadata"], fileSearchGrounding)
}

func assertJSONEqual(t *testing.T, got json.RawMessage, want string) {
	t.Helper()
	var a, b interface{}
	if err := json.Unmarshal(got, &a); err != nil {
		t.Fatalf("got is not JSON: %v (%s)", err, got)
	}
	_ = json.Unmarshal([]byte(want), &b)
	ga, _ := json.Marshal(a)
	gb, _ := json.Marshal(b)
	if string(ga) != string(gb) {
		t.Fatalf("groundingMetadata = %s, want %s", ga, gb)
	}
}
