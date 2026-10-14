package ai

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// Ported from packages/ai/src/generate-text/citations.test.ts (TS #21797).

func testCitations() []types.Citation {
	start, end := 4, 9
	return []types.Citation{
		{
			Source:     types.SourceContent{SourceType: "url", ID: "https://go.dev/", URL: "https://go.dev/", Title: "Go"},
			StartIndex: &start,
			EndIndex:   &end,
			CitedText:  "Go",
		},
		{Source: types.SourceContent{SourceType: "document", ID: "doc-1", MediaType: "application/pdf", Title: "Report", Filename: "r.pdf"}},
	}
}

func citationStreamModel() *testutil.MockLanguageModel {
	return &testutil.MockLanguageModel{
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeTextStart, ID: "t1"},
				{Type: provider.ChunkTypeText, ID: "t1", Text: "See Go docs."},
				{Type: provider.ChunkTypeTextEnd, ID: "t1", Citations: testCitations()},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		},
	}
}

func TestStreamTextAttachesCitationsToTextContent(t *testing.T) {
	result, err := StreamText(context.Background(), StreamTextOptions{Model: citationStreamModel(), Prompt: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := result.ReadAll(); err != nil {
		t.Fatal(err)
	}
	var text *types.TextContent
	for _, part := range result.Content() {
		if tc, ok := part.(types.TextContent); ok {
			text = &tc
		}
	}
	if text == nil || text.Text != "See Go docs." || len(text.Citations) != 2 || text.Citations[0].CitedText != "Go" {
		t.Fatalf("content = %#v", result.Content())
	}
}

func TestStreamTextUIMessageStreamCarriesCitations(t *testing.T) {
	result, err := StreamText(context.Background(), StreamTextOptions{Model: citationStreamModel(), Prompt: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	chunks, errs := result.ToUIMessageStream(context.Background())
	var textEnd UIMessageChunk
	for c := range chunks {
		if c["type"] == "text-end" {
			textEnd = c
		}
	}
	if err := <-errs; err != nil {
		t.Fatal(err)
	}
	cites, ok := textEnd["citations"].([]interface{})
	if !ok || len(cites) != 2 {
		t.Fatalf("text-end = %#v", textEnd)
	}
	first := cites[0].(map[string]interface{})
	src := first["source"].(map[string]interface{})
	if src["type"] != "source" || src["sourceType"] != "url" || first["startIndex"] != float64(4) || first["citedText"] != "Go" {
		t.Fatalf("citation = %#v", first)
	}
}

func TestTextUIPartCitationsRoundTripAndValidation(t *testing.T) {
	part := TextUIPart{Text: "x", State: "done", Citations: testCitations()}
	raw, err := json.Marshal(part)
	if err != nil {
		t.Fatal(err)
	}
	var back TextUIPart
	if err := json.Unmarshal(raw, &back); err != nil {
		t.Fatal(err)
	}
	if len(back.Citations) != 2 || back.Citations[1].Source.Filename != "r.pdf" || *back.Citations[0].EndIndex != 9 {
		t.Fatalf("round trip = %#v", back)
	}

	for _, bad := range []string{
		`{"type":"text","text":"x","citations":[{"source":{"type":"source","sourceType":"url","id":"a"}}]}`,
		`{"type":"text","text":"x","citations":[{"source":{"type":"source","sourceType":"document","id":"a","title":"t"}}]}`,
		`{"type":"text","text":"x","citations":[{"source":{"type":"source","sourceType":"url","id":"a","url":"u"},"startIndex":-1}]}`,
	} {
		var p TextUIPart
		if err := json.Unmarshal([]byte(bad), &p); err == nil {
			t.Errorf("expected validation error for %s", bad)
		}
	}
}

func TestUIMessageCallbackStateAppliesTextEndCitations(t *testing.T) {
	state := newUIMessageCallbackState(nil, "m1")
	state.apply(UIMessageChunk{"type": "text-start", "id": "1"}, nil)
	state.apply(UIMessageChunk{"type": "text-delta", "id": "1", "delta": "hi"}, nil)
	state.apply(UIMessageChunk{"type": "text-end", "id": "1", "citations": []interface{}{map[string]interface{}{"source": map[string]interface{}{"type": "source", "sourceType": "url", "id": "u", "url": "u"}}}}, nil)
	msg := state.responseMessage()
	parts, _ := msg["parts"].([]interface{})
	first, _ := parts[0].(UIMessageChunk)
	if len(parts) != 1 || first["citations"] == nil {
		t.Fatalf("message = %#v", msg)
	}
}
