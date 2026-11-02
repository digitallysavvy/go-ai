package gateway

import (
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/internal/tsfixture"
	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS parseJsonEventStream drops `data: [DONE]` and keeps reading the body.
func TestR78prova_GatewayReadsPastDone(t *testing.T) {
	stream := &gatewayTextStream{
		parser: streamingParser(strings.Join([]string{
			`data: {"type":"text-delta","id":"t","delta":"a"}`, "",
			`data: [DONE]`, "",
			`data: {"type":"text-delta","id":"t","delta":"b"}`, "",
			`data: {"type":"finish","finishReason":"stop"}`, "", "",
		}, "\n")),
		body: io.NopCloser(strings.NewReader("")),
	}
	text, finishes := "", 0
	for {
		c, err := stream.Next()
		if err != nil {
			break
		}
		if c.Type == provider.ChunkTypeText {
			text += c.Text
		}
		if c.Type == provider.ChunkTypeFinish {
			finishes++
		}
	}
	if text != "ab" || finishes != 1 {
		t.Fatalf("text %q, finishes %d; want \"ab\", 1", text, finishes)
	}
}

// TestR78prova_TSCasesLanguage replays the gateway-language-model.test.ts
// cases whose canned response the extractor can read (inline stream
// chunks); the others build it with local helpers (prepareJsonResponse,
// prepareStreamResponse) and are covered by the hand-written tests here.
func TestR78prova_TSCasesLanguage(t *testing.T) {
	tsfixture.DropResponseTimestamp = true
	defer func() { tsfixture.DropResponseTimestamp = false }()
	cases := tsfixture.LoadCases(t, "testdata/tscases/language.json.gz")
	// The TS prepareJsonResponse / prepareStreamResponse helpers' default
	// responses, for the cases that call them (their assertions read the
	// request or the call's error).
	for i := range cases {
		c := &cases[i]
		if c.Response != nil || c.Call == nil {
			continue
		}
		if c.CallKind == "stream" {
			c.Response = &tsfixture.Response{Kind: "chunks", Chunks: []interface{}{
				`data: {"type":"text-delta","textDelta":""}` + "\n\n",
				`data: {"type":"finish","finishReason":"stop","usage":{"prompt_tokens":10,"completion_tokens":20}}` + "\n\n",
			}}
			continue
		}
		c.Response = &tsfixture.Response{Kind: "json", Body: json.RawMessage(`{"id":"test-id","created":1711115037,"model":"test-model","content":{"type":"text","text":""},"finish_reason":"stop","usage":{"prompt_tokens":4,"completion_tokens":30}}`)}
	}
	tsfixture.Run(t, cases, func(base, id string) provider.LanguageModel {
		if id == "" {
			id = "test-model"
		}
		p, err := New(Config{APIKey: "test-token", BaseURL: base})
		if err != nil {
			t.Fatal(err)
		}
		m, err := p.LanguageModel(id)
		if err != nil {
			t.Fatal(err)
		}
		return m
	}, map[string]string{
		"should stream text deltas":                                    r78provaV3Parts,
		"should work without provider options":                         "the TS test's prepareJsonResponse content is one V3-shaped object, which TS returns verbatim as result.content",
		"should filter raw chunks based on includeRawChunks option":    r78provaV3Parts,
		"should include raw chunks when includeRawChunks is true":      r78provaV3Parts,
		"should not modify response-metadata chunks without timestamp": r78provaHeaderMetadata,
		"should handle null timestamp values gracefully":               r78provaHeaderMetadata,
	})
}

const (
	r78provaV3Parts        = "the TS test streams V3-shaped parts (textDelta, a string finishReason, wire usage), which TS forwards verbatim; Go decodes them to V4 parts"
	r78provaHeaderMetadata = "Go streams carry the HTTP response headers in a leading response-metadata part (providerutils.WithResponseMetadata); TS returns them on the stream result, so TS counts one part less"
)
