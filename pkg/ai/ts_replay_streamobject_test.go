package ai

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// Replays packages/ai/src/generate-object/stream-object.test.ts: object
// deltas, name/description in the response format, error handling (doStream
// throws, error stream part, raw stream error, throwing onError), the
// NoObjectGeneratedError cases, arrays, enums, no-schema, repairText and
// warnings. The Go API is a blocking call with OnChunk partials instead of a
// promise/stream result, so each TS assertion maps to its Go counterpart.

func streamObjectModel(chunks ...provider.StreamChunk) *testutil.MockLanguageModel {
	return &testutil.MockLanguageModel{
		StructuredSupport: true,
		DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream(append([]provider.StreamChunk(nil), chunks...)), nil
		},
	}
}

func textChunks(deltas ...string) []provider.StreamChunk {
	out := []provider.StreamChunk{{Type: provider.ChunkTypeTextStart, ID: "1"}}
	for _, d := range deltas {
		out = append(out, provider.StreamChunk{Type: provider.ChunkTypeText, ID: "1", Text: d})
	}
	return append(out, provider.StreamChunk{Type: provider.ChunkTypeTextEnd, ID: "1"})
}

func finishChunk() provider.StreamChunk {
	return provider.StreamChunk{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, Usage: usage(3, 10)}
}

func TestTSReplay_StreamObject_ObjectDeltas(t *testing.T) {
	chunks := textChunks("{ ", `"content": `, `"Hello, `, `world`, `!"`, " }")
	chunks = append(chunks, finishChunk())
	model := streamObjectModel(chunks...)

	var partials []interface{}
	res, err := StreamObject(context.Background(), StreamObjectOptions{
		Model: model, Prompt: "prompt", Schema: contentSchema(),
		SchemaName: "test-name", SchemaDescription: "test description",
		OnChunk: func(p interface{}) { partials = append(partials, p) },
	})
	if err != nil {
		t.Fatal(err)
	}
	want := []interface{}{
		map[string]interface{}{},
		map[string]interface{}{"content": "Hello, "},
		map[string]interface{}{"content": "Hello, world"},
		map[string]interface{}{"content": "Hello, world!"},
	}
	if !reflect.DeepEqual(partials, want) {
		t.Fatalf("partials = %#v, want %#v", partials, want)
	}
	if !reflect.DeepEqual(res.Object, map[string]interface{}{"content": "Hello, world!"}) {
		t.Fatalf("object = %#v", res.Object)
	}
	rf := model.StreamCalls[0].ResponseFormat
	if rf == nil || rf.Type != "json" || rf.Name != "test-name" || rf.Description != "test description" {
		t.Fatalf("response format = %#v", rf)
	}
	if res.FinishReason != types.FinishReasonStop {
		t.Fatalf("finish reason = %v", res.FinishReason)
	}
}

func TestTSReplay_StreamObject_DoStreamThrows(t *testing.T) {
	boom := errors.New("test error")
	model := &testutil.MockLanguageModel{
		StructuredSupport: true,
		DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			return nil, boom
		},
	}
	var got []error
	_, err := StreamObject(context.Background(), StreamObjectOptions{
		Model: model, Prompt: "prompt", Schema: contentSchema(), MaxRetries: 0,
		OnError: func(_ context.Context, e error) { got = append(got, e) },
	})
	if !errors.Is(err, boom) {
		t.Fatalf("error = %v, want the doStream error", err)
	}
	if len(got) != 1 || !errors.Is(got[0], boom) {
		t.Fatalf("onError calls = %v", got)
	}
}

func TestTSReplay_StreamObject_ErrorStreamPart(t *testing.T) {
	boom := errors.New("test error")
	model := streamObjectModel(provider.StreamChunk{Type: provider.ChunkTypeError, Err: boom, Text: boom.Error()})

	var onErrors []error
	var stepEnd *ObjectOnStepFinishEvent
	var end *ObjectOnFinishEvent
	_, err := StreamObject(context.Background(), StreamObjectOptions{
		Model: model, Prompt: "prompt", Schema: contentSchema(),
		OnError:   func(_ context.Context, e error) { onErrors = append(onErrors, e) },
		OnStepEnd: func(_ context.Context, e ObjectOnStepFinishEvent) { stepEnd = &e },
		OnEnd:     func(_ context.Context, e ObjectOnFinishEvent) { end = &e },
	})
	if !errors.Is(err, boom) {
		t.Fatalf("error = %v, want the stream error (typed, errors.Is)", err)
	}
	if len(onErrors) != 1 || !errors.Is(onErrors[0], boom) {
		t.Fatalf("onError = %v", onErrors)
	}
	if stepEnd == nil || stepEnd.FinishReason != types.FinishReasonError {
		t.Fatalf("onStepEnd = %#v, want finishReason error", stepEnd)
	}
	if end == nil || end.Object != nil || end.FinishReason != types.FinishReasonError || !errors.Is(end.Error, boom) {
		t.Fatalf("onEnd = %#v, want object nil, error and finishReason error", end)
	}
}

func TestTSReplay_StreamObject_ThrowingOnErrorKeepsFinishCallbacks(t *testing.T) {
	boom := errors.New("provider error")
	model := streamObjectModel(provider.StreamChunk{Type: provider.ChunkTypeError, Err: boom, Text: boom.Error()})
	var mu sync.Mutex
	stepEnds, ends := 0, 0
	_, err := StreamObject(context.Background(), StreamObjectOptions{
		Model: model, Prompt: "prompt", Schema: contentSchema(),
		OnError:   func(context.Context, error) { panic("callback error") },
		OnStepEnd: func(context.Context, ObjectOnStepFinishEvent) { mu.Lock(); stepEnds++; mu.Unlock() },
		OnEnd:     func(context.Context, ObjectOnFinishEvent) { mu.Lock(); ends++; mu.Unlock() },
	})
	if !errors.Is(err, boom) {
		t.Fatalf("error = %v", err)
	}
	if stepEnds != 1 || ends != 1 {
		t.Fatalf("onStepEnd=%d onEnd=%d, want once each", stepEnds, ends)
	}
}

type failingStream struct{ err error }

func (s *failingStream) Next() (*provider.StreamChunk, error) { return nil, s.err }
func (s *failingStream) Err() error                           { return s.err }
func (s *failingStream) Close() error                         { return nil }

func TestTSReplay_StreamObject_RawStreamErrorPreserved(t *testing.T) {
	boom := errors.New("test error")
	model := &testutil.MockLanguageModel{
		StructuredSupport: true,
		DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			return &failingStream{err: boom}, nil
		},
	}
	var onErrors []error
	_, err := StreamObject(context.Background(), StreamObjectOptions{
		Model: model, Prompt: "prompt", Schema: contentSchema(),
		OnError: func(_ context.Context, e error) { onErrors = append(onErrors, e); panic("callback error") },
	})
	if !errors.Is(err, boom) {
		t.Fatalf("error = %v, want the raw stream error", err)
	}
	if len(onErrors) != 1 || !errors.Is(onErrors[0], boom) {
		t.Fatalf("onError = %v", onErrors)
	}
}

func TestTSReplay_StreamObject_NoObjectGeneratedErrors(t *testing.T) {
	meta := provider.StreamChunk{Type: provider.ChunkTypeResponseMetadata, ResponseMetadata: &provider.ResponseMetadata{ID: "id-1", ModelID: "model-1"}}
	cases := []struct {
		name    string
		chunks  []provider.StreamChunk
		message string
	}{
		{"schema validation fails", append(append(textChunks(`{ "content": 123 }`), meta), finishChunk()), "No object generated: response did not match schema."},
		{"parsing fails", append(append(textChunks(`{ broken json`), meta), finishChunk()), "No object generated: could not parse the response."},
		{"no text", []provider.StreamChunk{meta, finishChunk()}, "No object generated: could not parse the response."},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := StreamObject(context.Background(), StreamObjectOptions{Model: streamObjectModel(tc.chunks...), Prompt: "prompt", Schema: contentSchema()})
			var nog *NoObjectGeneratedError
			if !errors.As(err, &nog) {
				t.Fatalf("error = %v (%T), want NoObjectGeneratedError", err, err)
			}
			if nog.Error() != tc.message {
				t.Fatalf("message = %q, want %q", nog.Error(), tc.message)
			}
			if nog.Response == nil || nog.Response.ID != "id-1" || nog.Response.ModelID != "model-1" {
				t.Fatalf("response = %#v", nog.Response)
			}
			if nog.Usage == nil || nog.Usage.InputTokens == nil || *nog.Usage.InputTokens != 3 {
				t.Fatalf("usage = %#v", nog.Usage)
			}
			if nog.FinishReason != types.FinishReasonStop {
				t.Fatalf("finish reason = %v", nog.FinishReason)
			}
		})
	}
}

func TestTSReplay_StreamObject_OnFinishWithInvalidObject(t *testing.T) {
	chunks := append(textChunks(`{ "content": 123 }`), finishChunk())
	var end *ObjectOnFinishEvent
	_, _ = StreamObject(context.Background(), StreamObjectOptions{
		Model: streamObjectModel(chunks...), Prompt: "prompt", Schema: contentSchema(),
		OnEnd: func(_ context.Context, e ObjectOnFinishEvent) { end = &e },
	})
	if end == nil || end.Object != nil || end.Error == nil {
		t.Fatalf("onEnd = %#v, want object nil and a validation error", end)
	}
	var nog *NoObjectGeneratedError
	if !errors.As(end.Error, &nog) || !strings.Contains(nog.Error(), "did not match schema") {
		t.Fatalf("onEnd error = %v", end.Error)
	}
}

func TestTSReplay_StreamObject_Array(t *testing.T) {
	deltas := []string{`{"elements":[`, `{"content":"element 1"},`, `{"content":"element 2"},`, `{"content":"element 3"}`, `]}`}
	chunks := append(textChunks(deltas...), finishChunk())
	var partials []interface{}
	res, err := StreamObject(context.Background(), StreamObjectOptions{
		Model: streamObjectModel(chunks...), Prompt: "prompt", Schema: contentSchema(), OutputMode: ObjectModeArray,
		OnChunk: func(p interface{}) { partials = append(partials, p) },
	})
	if err != nil {
		t.Fatal(err)
	}
	el := func(s string) interface{} { return map[string]interface{}{"content": s} }
	want := []interface{}{
		[]interface{}{},
		[]interface{}{el("element 1")},
		[]interface{}{el("element 1"), el("element 2")},
		[]interface{}{el("element 1"), el("element 2"), el("element 3")},
	}
	if !reflect.DeepEqual(partials, want) {
		t.Fatalf("partials = %#v\nwant %#v", partials, want)
	}
	if !reflect.DeepEqual(res.Array, want[3]) {
		t.Fatalf("result array = %#v", res.Array)
	}
}

func TestTSReplay_StreamObject_Enum(t *testing.T) {
	enum := []string{"sunny", "rainy", "snowy"}
	run := func(deltas ...string) ([]interface{}, *GenerateObjectResult, error) {
		var partials []interface{}
		res, err := StreamObject(context.Background(), StreamObjectOptions{
			Model: streamObjectModel(append(textChunks(deltas...), finishChunk())...), Prompt: "prompt",
			OutputMode: ObjectModeEnum, EnumValues: enum,
			OnChunk: func(p interface{}) { partials = append(partials, p) },
		})
		return partials, res, err
	}
	// 'should stream an enum value'
	partials, res, err := run(`{ `, `"result": `, `"su`, `nny`, `" `, `}`)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(partials, []interface{}{"sunny"}) || res.EnumValue != "sunny" {
		t.Fatalf("enum value: partials=%#v result=%q", partials, res.EnumValue)
	}
	// 'should not stream incorrect values'
	partials, _, err = run(`{ `, `"result": `, `"foo`, `bar`, `" `, `}`)
	if err == nil {
		t.Fatal("an invalid enum value must fail")
	}
	if len(partials) != 0 {
		t.Fatalf("incorrect values were streamed: %#v", partials)
	}
}

func TestTSReplay_StreamObject_RepairText(t *testing.T) {
	append1 := func(suffix string) RepairTextFunc {
		return func(_ context.Context, text string, _ error) (*string, error) { s := text + suffix; return &s, nil }
	}
	// 'should be able to repair a JSONParseError'
	res, err := StreamObject(context.Background(), StreamObjectOptions{
		Model: streamObjectModel(append(textChunks(`{ "content": "provider metadata test" `), finishChunk())...), Prompt: "prompt",
		Schema: contentSchema(), RepairText: append1("}"),
	})
	if err != nil || !reflect.DeepEqual(res.Object, map[string]interface{}{"content": "provider metadata test"}) {
		t.Fatalf("repair parse error: %#v, %v", res.Object, err)
	}
	// 'should be able to handle repair that returns null'
	_, err = StreamObject(context.Background(), StreamObjectOptions{
		Model: streamObjectModel(append(textChunks(`{ "content": "provider metadata test" `), finishChunk())...), Prompt: "prompt",
		Schema: contentSchema(), RepairText: func(context.Context, string, error) (*string, error) { return nil, nil },
	})
	var nog *NoObjectGeneratedError
	if !errors.As(err, &nog) {
		t.Fatalf("null repair: error = %v, want NoObjectGeneratedError", err)
	}
	// 'should throw NoObjectGeneratedError when parsing fails with repairText'
	_, err = StreamObject(context.Background(), StreamObjectOptions{
		Model: streamObjectModel(append(textChunks(`{ broken json`), finishChunk())...), Prompt: "prompt",
		Schema: contentSchema(), RepairText: append1("{"),
	})
	if !errors.As(err, &nog) || nog.Error() != "No object generated: could not parse the response." {
		t.Fatalf("failed repair: error = %v", err)
	}
	// 'should be able to repair JSON wrapped with markdown code blocks'
	res, err = StreamObject(context.Background(), StreamObjectOptions{
		Model: streamObjectModel(append(textChunks("```json\n{ \"content\": \"test message\" }\n```"), finishChunk())...), Prompt: "prompt",
		Schema: contentSchema(),
		RepairText: func(_ context.Context, text string, _ error) (*string, error) {
			s := strings.TrimSuffix(strings.TrimPrefix(text, "```json\n"), "\n```")
			return &s, nil
		},
	})
	if err != nil || !reflect.DeepEqual(res.Object, map[string]interface{}{"content": "test message"}) {
		t.Fatalf("markdown repair: %#v, %v", res.Object, err)
	}
	// 'should prefer repairText over experimental_repairText'
	res, err = StreamObject(context.Background(), StreamObjectOptions{
		Model: streamObjectModel(append(textChunks(`{ "content": "x" `), finishChunk())...), Prompt: "prompt",
		Schema: contentSchema(), RepairText: append1("}"), ExperimentalRepairText: func(context.Context, string, error) (*string, error) { s := "{}"; return &s, nil },
	})
	if err != nil || !reflect.DeepEqual(res.Object, map[string]interface{}{"content": "x"}) {
		t.Fatalf("precedence: %#v, %v", res.Object, err)
	}
}

func TestTSReplay_StreamObject_Warnings(t *testing.T) {
	warn := types.Warning{Type: "other", Message: "Setting is not supported"}
	chunks := append([]provider.StreamChunk{{Type: provider.ChunkTypeStreamStart, Warnings: []types.Warning{warn}}}, textChunks(`{ "content": "x" }`)...)
	chunks = append(chunks, finishChunk())
	var end *ObjectOnFinishEvent
	res, err := StreamObject(context.Background(), StreamObjectOptions{
		Model: streamObjectModel(chunks...), Prompt: "prompt", Schema: contentSchema(),
		OnEnd: func(_ context.Context, e ObjectOnFinishEvent) { end = &e },
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Warnings) != 1 || res.Warnings[0].Message != warn.Message {
		t.Fatalf("result warnings = %#v", res.Warnings)
	}
	if end == nil || len(end.Warnings) != 1 {
		t.Fatalf("onEnd warnings = %#v", end)
	}
}

// TS always sends responseFormat {type: 'json', schema?, name?, description?}.
// Providers key their JSON mode off type "json" (Gemini sets responseMimeType
// and responseJsonSchema only for it), so "json_schema"/"json_object" from
// GenerateObject/StreamObject silently disabled structured output there.
func TestResponseFormatTypeIsJSONForObjectCalls(t *testing.T) {
	t.Parallel()
	modes := []struct {
		mode       ObjectOutputMode
		text       string
		wantSchema bool
	}{
		{ObjectModeObject, `{"content":"x"}`, true},
		{ObjectModeArray, `{"elements":[{"content":"x"}]}`, true},
		{ObjectModeEnum, `{"result":"a"}`, true},
		{ObjectModeNoSchema, `{"a":1}`, false},
	}
	for _, m := range modes {
		t.Run(string(m.mode), func(t *testing.T) {
			var got []*provider.ResponseFormat
			var mu sync.Mutex
			model := &testutil.MockLanguageModel{
				StructuredSupport: true,
				DoGenerateFunc: func(_ context.Context, o *provider.GenerateOptions) (*types.GenerateResult, error) {
					mu.Lock()
					got = append(got, o.ResponseFormat)
					mu.Unlock()
					return &types.GenerateResult{Text: m.text, FinishReason: types.FinishReasonStop}, nil
				},
				DoStreamFunc: func(_ context.Context, o *provider.GenerateOptions) (provider.TextStream, error) {
					mu.Lock()
					got = append(got, o.ResponseFormat)
					mu.Unlock()
					return testutil.NewMockTextStream(append(textChunks(m.text), finishChunk())), nil
				},
			}
			var sch schema.Schema
			var enum []string
			switch m.mode {
			case ObjectModeObject, ObjectModeArray:
				sch = contentSchema()
			case ObjectModeEnum:
				enum = []string{"a", "b"}
			}
			if _, err := GenerateObject(context.Background(), GenerateObjectOptions{Model: model, Prompt: "p", Schema: sch, OutputMode: m.mode, EnumValues: enum}); err != nil {
				t.Fatal(err)
			}
			if _, err := StreamObject(context.Background(), StreamObjectOptions{Model: model, Prompt: "p", Schema: sch, OutputMode: m.mode, EnumValues: enum}); err != nil {
				t.Fatal(err)
			}
			if len(got) != 2 {
				t.Fatalf("calls = %d", len(got))
			}
			for _, rf := range got {
				if rf == nil || rf.Type != "json" {
					t.Fatalf("response format = %#v, want type json", rf)
				}
				if (rf.Schema != nil) != m.wantSchema {
					t.Fatalf("schema present = %v, want %v", rf.Schema != nil, m.wantSchema)
				}
				if m.wantSchema {
					if _, ok := rf.Schema.(map[string]interface{}); !ok {
						if _, isSchema := rf.Schema.(interface{ JSONSchema() map[string]interface{} }); !isSchema {
							t.Fatalf("schema = %T", rf.Schema)
						}
					}
				}
			}
		})
	}
}
