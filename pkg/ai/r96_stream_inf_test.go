package ai

import (
	"context"
	"encoding/json"
	"math"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Ruling 4 (round 96): a provider number beyond the double range (1e400) in
// provider metadata reads as +Inf, as TS JSON.parse reads Infinity, and the
// SDK's UI message stream writes it as null, as JSON.stringify does.
func TestR96streamInfProviderMetadataReadsInfWritesNull(t *testing.T) {
	md := json.RawMessage(`{"p":{"x":1e400}}`)
	r, err := StreamText(context.Background(), StreamTextOptions{
		Model: r71streamModel(nil, []provider.StreamChunk{
			{Type: provider.ChunkTypeStreamStart},
			{Type: provider.ChunkTypeTextStart, ID: "t"},
			{Type: provider.ChunkTypeText, ID: "t", Text: "hi", ProviderMetadata: md},
			{Type: provider.ChunkTypeTextEnd, ID: "t"},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, RawFinishReason: "stop", Usage: r71streamUsage, ProviderMetadata: md},
		}),
		Prompt: "x",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := r.ConsumeStream(); err != nil {
		t.Fatal(err)
	}
	p, _ := r.FinalStep().ProviderMetadata["p"].(map[string]interface{})
	if x, _ := p["x"].(float64); !math.IsInf(x, 1) {
		t.Fatalf("step providerMetadata p.x = %#v, want +Inf", p["x"])
	}
	chunks, errs := r.ToUIMessageStream(context.Background())
	var delta string
	for c := range chunks {
		if c["type"] == "text-delta" {
			b, err := uiSSEEventBytes(c)
			if err != nil {
				t.Fatalf("encode text-delta: %v", err)
			}
			delta = string(b)
		}
	}
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	want := `data: {"type":"text-delta","id":"t","delta":"hi","providerMetadata":{"p":{"x":null}}}` + "\n\n"
	if delta != want {
		t.Fatalf("text-delta event\n got %q\nwant %q", delta, want)
	}
}

// ReadUIMessageStream reads a 1e400 in a chunk as +Inf (TS
// parseJsonEventStream JSON.parse), instead of failing the stream.
func TestR96streamReadUIMessageStreamInf(t *testing.T) {
	chunks, err := ReadUIMessageStream(strings.NewReader("data: {\"type\":\"data-x\",\"data\":{\"v\":1e400}}\n\n"))
	if err != nil {
		t.Fatal(err)
	}
	data, _ := chunks[0]["data"].(map[string]interface{})
	if v, _ := data["v"].(float64); !math.IsInf(v, 1) {
		t.Fatalf("data.v = %#v, want +Inf", data["v"])
	}
}
