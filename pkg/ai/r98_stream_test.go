package ai

import (
	"context"
	"encoding/json"
	"reflect"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// r98streamProtoMetadata is provider metadata with an own `__proto__` key
// and a `constructor.prototype`: the keys secureJsonParse rejects in network
// bytes. TS passes a providerMetadata object along as is (it is never parsed
// again), so it reaches the step, the onEnd telemetry and the UI chunks with
// those keys. Go keeps its metadata as JSON and decodes it again internally;
// those decodes must not drop it.
const r98streamProtoMetadata = `{"p":{"__proto__":{"x":1},"constructor":{"prototype":{"y":2}},"sig":"keep"}}`

func r98streamProtoMetadataMap(t *testing.T) map[string]interface{} {
	t.Helper()
	var m map[string]interface{}
	if err := json.Unmarshal([]byte(r98streamProtoMetadata), &m); err != nil {
		t.Fatal(err)
	}
	return m
}

type r98streamEndRecorder struct {
	telemetry.NoopTelemetryIntegration
	mu  sync.Mutex
	end []map[string]interface{}
}

func (r *r98streamEndRecorder) OnEnd(_ context.Context, e telemetry.TelemetryFinishEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.end = append(r.end, e.ProviderMetadata)
}

// StreamText: the step's provider metadata, the onEnd telemetry metadata and
// the UI text-delta / tool-output metadata keep a `__proto__` key.
func TestR98streamProviderMetadataKeepsPrototypeKeys(t *testing.T) {
	want := r98streamProtoMetadataMap(t)
	meta := json.RawMessage(r98streamProtoMetadata)
	rec := &r98streamEndRecorder{}
	r, err := StreamText(context.Background(), StreamTextOptions{
		Model: r71streamModel(nil, []provider.StreamChunk{
			{Type: provider.ChunkTypeStreamStart},
			{Type: provider.ChunkTypeTextStart, ID: "t1"},
			{Type: provider.ChunkTypeText, ID: "t1", Text: "hi", ProviderMetadata: meta},
			{Type: provider.ChunkTypeTextEnd, ID: "t1"},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, RawFinishReason: "stop", Usage: r71streamUsage, ProviderMetadata: meta},
		}),
		Prompt:    "x",
		Telemetry: &telemetry.Settings{Integrations: []telemetry.TelemetryIntegration{rec}},
	})
	if err != nil {
		t.Fatal(err)
	}
	chunks, errCh := CreateUIMessageStream(context.Background(), r)
	var delta UIMessageChunk
	for c := range chunks {
		if c["type"] == "text-delta" {
			delta = c
		}
	}
	if err := <-errCh; err != nil {
		t.Fatal(err)
	}
	if got := delta["providerMetadata"]; !reflect.DeepEqual(got, want) {
		t.Errorf("UI text-delta providerMetadata = %#v, want %#v", got, want)
	}
	if got := r.FinalStep().ProviderMetadata; !reflect.DeepEqual(got, want) {
		t.Errorf("step providerMetadata = %#v, want %#v", got, want)
	}
	rec.mu.Lock()
	defer rec.mu.Unlock()
	if len(rec.end) != 1 || !reflect.DeepEqual(rec.end[0], want) {
		t.Errorf("onEnd telemetry providerMetadata = %#v, want [%#v]", rec.end, want)
	}
}

// A provider-executed tool result's metadata keeps a `__proto__` key on the
// step's ToolResults (TS keeps the part's providerMetadata object).
func TestR98streamProviderToolResultMetadataKeepsPrototypeKeys(t *testing.T) {
	want := r98streamProtoMetadataMap(t)
	got := providerToolResultsFromContent([]types.ContentPart{
		types.ToolResultContent{ToolCallID: "c1", ToolName: "web", Result: "ok", ProviderMetadata: json.RawMessage(r98streamProtoMetadata)},
		types.ToolErrorContent{ToolCallID: "c2", ToolName: "web", Error: "bad", ProviderMetadata: json.RawMessage(r98streamProtoMetadata)},
	}, nil)
	if len(got) != 2 {
		t.Fatalf("tool results = %d, want 2", len(got))
	}
	for i, tr := range got {
		if !reflect.DeepEqual(tr.ProviderMetadata, want) {
			t.Errorf("tool result %d providerMetadata = %#v, want %#v", i, tr.ProviderMetadata, want)
		}
	}
}

// NewStreamTextResultFromParts: the step's provider metadata from an
// external finish-step part, and the call-level metadata of the terminal
// finish part, keep a `__proto__` key.
func TestR98streamExternalFinishMetadataKeepsPrototypeKeys(t *testing.T) {
	want := r98streamProtoMetadataMap(t)
	meta := json.RawMessage(r98streamProtoMetadata)
	src := testutil.NewMockTextStream([]provider.StreamChunk{
		{Type: provider.ChunkTypeStreamStart},
		{Type: provider.ChunkTypeText, Text: "hi"},
		{Type: provider.ChunkTypeFinishStep, FinishReason: types.FinishReasonStop, ProviderMetadata: meta},
		{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, ProviderMetadata: meta},
	})
	result := NewStreamTextResultFromParts(context.Background(), src, ExternalStreamOptions{TerminalFinishClosesTurn: true})
	if err := result.ConsumeStream(); err != nil {
		t.Fatal(err)
	}
	steps := result.Steps()
	if len(steps) != 1 {
		t.Fatalf("steps = %d, want 1", len(steps))
	}
	if got := steps[0].ProviderMetadata; !reflect.DeepEqual(got, want) {
		t.Errorf("step providerMetadata = %#v, want %#v", got, want)
	}
	var got map[string]interface{}
	if raw := result.ProviderMetadata(); len(raw) == 0 || json.Unmarshal(raw, &got) != nil || !reflect.DeepEqual(got, want) {
		t.Errorf("call providerMetadata = %s, want %s", raw, r98streamProtoMetadata)
	}
}

// With an experimental transform the step is recorded from the transformed
// finish-step part: its provider metadata keeps a `__proto__` key too.
func TestR98streamTransformedStepMetadataKeepsPrototypeKeys(t *testing.T) {
	want := r98streamProtoMetadataMap(t)
	meta := json.RawMessage(r98streamProtoMetadata)
	r := r71streamRun(t, StreamTextOptions{
		Model: r71streamModel(nil, []provider.StreamChunk{
			{Type: provider.ChunkTypeStreamStart},
			{Type: provider.ChunkTypeText, Text: "hi"},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, RawFinishReason: "stop", Usage: r71streamUsage, ProviderMetadata: meta},
		}),
		Prompt: "x",
		ExperimentalTransform: []StreamTransformFunc{func(_ context.Context, c provider.StreamChunk) []provider.StreamChunk {
			return []provider.StreamChunk{c}
		}},
	})
	if got := r.FinalStep().ProviderMetadata; !reflect.DeepEqual(got, want) {
		t.Errorf("step providerMetadata = %#v, want %#v", got, want)
	}
}

// A transform that leaves a tool call's decoded input as the model sent it
// keeps the call's input text (TS keeps the part's input as it is), also when
// that text has a `__proto__` key: it is not rewritten in Go's key order.
func TestR98streamSyncRawArgumentsKeepsPrototypeKeyText(t *testing.T) {
	raw := `{"b":1,"__proto__":{"x":1}}`
	var args map[string]interface{}
	if err := json.Unmarshal([]byte(raw), &args); err != nil {
		t.Fatal(err)
	}
	call := types.ToolCall{ID: "c1", ToolName: "t", Arguments: args, RawArguments: raw}
	syncRawArguments(&call)
	if call.RawArguments != raw {
		t.Errorf("RawArguments = %s, want %s", call.RawArguments, raw)
	}
}
