package gemini

import (
	"encoding/json"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS responseSchema / chunkSchema parse promptFeedback, groundingMetadata,
// urlContextMetadata and safetyRatings with plain z.object schemas, so the
// provider metadata holds only the declared keys (Vertex sends
// groundingChunks[].web.domain and searchEntryPoint.sdkBlob, which TS drops).
// retrievalMetadata is z.union([{webDynamicRetrievalScore: number}, {}]).
const (
	r79provaWireGrounding = `{"webSearchQueries":["q"],"searchEntryPoint":{"renderedContent":"<b>","sdkBlob":"x"},"groundingChunks":[{"web":{"uri":"https://a.example","title":"A","domain":"a.example"},"retrievedContext":{"title":"T","customMetadata":[{"key":"k","numericValue":1,"extra":true}],"ragChunk":{"text":"r"}}}],"groundingSupports":[{"segment":{"startIndex":0,"endIndex":1,"text":"t","partIndex":0},"groundingChunkIndices":[0],"confidenceScores":[0.9]}],"retrievalMetadata":{"googleSearchDynamicRetrievalScore":0.5},"googleMapsWidgetContextToken":"tok"}`
	r79provaTSGrounding   = `{"webSearchQueries":["q"],"searchEntryPoint":{"renderedContent":"<b>"},"groundingChunks":[{"web":{"uri":"https://a.example","title":"A"},"retrievedContext":{"title":"T","customMetadata":[{"key":"k","numericValue":1}]}}],"groundingSupports":[{"segment":{"startIndex":0,"endIndex":1,"text":"t"},"groundingChunkIndices":[0],"confidenceScores":[0.9]}],"retrievalMetadata":{}}`
	r79provaWireSafety    = `[{"category":"HARM_CATEGORY_HATE_SPEECH","probability":"NEGLIGIBLE","blocked":false,"x":1}]`
	r79provaTSSafety      = `[{"category":"HARM_CATEGORY_HATE_SPEECH","probability":"NEGLIGIBLE","blocked":false}]`
	r79provaWireURL       = `{"urlMetadata":[{"retrievedUrl":"https://b.example","urlRetrievalStatus":"URL_RETRIEVAL_STATUS_SUCCESS","x":1}],"y":2}`
	r79provaTSURL         = `{"urlMetadata":[{"retrievedUrl":"https://b.example","urlRetrievalStatus":"URL_RETRIEVAL_STATUS_SUCCESS"}]}`
	r79provaWireFeedback  = `{"blockReasonMessage":"m","safetyRatings":` + r79provaWireSafety + `}`
	r79provaTSFeedback    = `{"safetyRatings":` + r79provaTSSafety + `}`
	r79provaWireBody      = `{"candidates":[{"content":{"parts":[{"text":"hi"}],"role":"model"},"finishReason":"STOP","groundingMetadata":` + r79provaWireGrounding + `,"urlContextMetadata":` + r79provaWireURL + `,"safetyRatings":` + r79provaWireSafety + `}],"promptFeedback":` + r79provaWireFeedback + `}`
)

func r79provaCheckMeta(t *testing.T, meta map[string]json.RawMessage) {
	t.Helper()
	for key, want := range map[string]string{
		"groundingMetadata":  r79provaTSGrounding,
		"safetyRatings":      r79provaTSSafety,
		"urlContextMetadata": r79provaTSURL,
		"promptFeedback":     r79provaTSFeedback,
	} {
		t.Run(key, func(t *testing.T) { assertJSONEqual(t, meta[key], want) })
	}
}

func TestR79prova_MetadataStripsUndeclaredKeysGenerate(t *testing.T) {
	m := makeTestModel("gemini-2.0-flash")
	var resp Response
	if err := json.Unmarshal([]byte(r79provaWireBody), &resp); err != nil {
		t.Fatal(err)
	}
	result := m.convertResponse(resp, nil)
	meta, ok := result.ProviderMetadata["google"].(map[string]json.RawMessage)
	if !ok {
		t.Fatalf("ProviderMetadata[google] type = %T", result.ProviderMetadata["google"])
	}
	r79provaCheckMeta(t, meta)
}

func TestR79prova_MetadataStripsUndeclaredKeysStream(t *testing.T) {
	s := newTestStream(sseStream(r79provaWireBody))
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
	r79provaCheckMeta(t, meta["google"])
}

// retrievalMetadata with a numeric webDynamicRetrievalScore keeps only it;
// a null score falls through to the {} member.
func TestR79prova_RetrievalMetadataUnion(t *testing.T) {
	for in, want := range map[string]string{
		`{"retrievalMetadata":{"webDynamicRetrievalScore":0.96,"x":1}}`: `{"retrievalMetadata":{"webDynamicRetrievalScore":0.96}}`,
		`{"retrievalMetadata":{"webDynamicRetrievalScore":null,"x":1}}`: `{"retrievalMetadata":{}}`,
		`{"retrievalMetadata":null}`:                                    `{"retrievalMetadata":null}`,
		`null`:                                                          `null`,
	} {
		if got := string(stripMetadata(json.RawMessage(in), metaGrounding)); got != want {
			t.Errorf("%s: got %s, want %s", in, got, want)
		}
	}
}
