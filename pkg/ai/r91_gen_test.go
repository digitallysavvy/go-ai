package ai

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	promptutils "github.com/digitallysavvy/go-ai/pkg/providerutils/prompt"
	"github.com/digitallysavvy/go-ai/pkg/schema"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// Round 91 gen tests (TS 2ebc78b654).

// r91genMCPDynamicResult is a provider response holding a provider-executed
// MCP call of a tool that is not in the tool set (OpenAI mcp.*), its
// provider result, and the final text.
func r91genMCPDynamicResult() *types.GenerateResult {
	return &types.GenerateResult{
		Content: []types.ContentPart{
			types.ToolCallContent{ToolCallID: "mcp_1", ToolName: "mcp.web_search_exa", Input: `{"query":"x"}`, ProviderExecuted: true, Dynamic: true},
			types.ToolResultContent{ToolCallID: "mcp_1", ToolName: "mcp.web_search_exa", Result: "found", ProviderExecuted: true, Dynamic: true},
			types.TextContent{Text: "done"},
		},
		ToolCalls:    []types.ToolCall{{ID: "mcp_1", ToolName: "mcp.web_search_exa", RawArguments: `{"query":"x"}`, Arguments: map[string]interface{}{"query": "x"}, ProviderExecuted: true, ProviderExecutedSet: true, Dynamic: true}},
		FinishReason: types.FinishReasonStop,
	}
}

func r91genNoToolError(t *testing.T, content []types.ContentPart, msgs []types.Message) {
	t.Helper()
	for _, m := range msgs {
		if m.Role == types.RoleTool {
			t.Errorf("response messages hold a role:tool message %+v; TS sends none for a provider-executed call", m)
		}
	}
	for _, part := range content {
		if r, ok := part.(types.ToolResultContent); ok && r.Error != "" {
			t.Errorf("content holds a client tool error %q", r.Error)
		}
	}
}

// core1-C01: generate-text.ts:1526 runs only the client tool calls
// (!providerExecuted), and execute-tool-call.ts:89 gives a non-executable
// tool no output, so a provider-executed dynamic call (OpenAI MCP) gets no
// client "tool not found" error and no role:tool message; otherwise the next
// turn sends a function_call_output that OpenAI rejects.
func TestR91genProviderDynamicCallNotToolNotFound(t *testing.T) {
	local := types.Tool{Name: "local", Parameters: map[string]interface{}{"type": "object"}, Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
		return "ok", nil
	}}
	t.Run("generateText", func(t *testing.T) {
		model := &testutil.MockLanguageModel{DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
			return r91genMCPDynamicResult(), nil
		}}
		res, err := GenerateText(context.Background(), GenerateTextOptions{Model: model, Prompt: "hi", Tools: []types.Tool{local}})
		if err != nil {
			t.Fatal(err)
		}
		if len(res.Steps) != 1 {
			t.Fatalf("steps = %d, want 1", len(res.Steps))
		}
		r91genNoToolError(t, res.Content, res.ResponseMessages)
		for _, tr := range res.ToolResults {
			if tr.Error != nil {
				t.Errorf("tool result error %v", tr.Error)
			}
		}
	})
	t.Run("streamText", func(t *testing.T) {
		model := &testutil.MockLanguageModel{DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
			return testutil.NewMockTextStream([]provider.StreamChunk{
				{Type: provider.ChunkTypeStreamStart},
				{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "mcp_1", ToolName: "mcp.web_search_exa", RawArguments: `{"query":"x"}`, Arguments: map[string]interface{}{"query": "x"}, ProviderExecuted: true, ProviderExecutedSet: true, Dynamic: true}},
				{Type: provider.ChunkTypeToolResult, ToolResult: &types.ToolResult{ToolCallID: "mcp_1", ToolName: "mcp.web_search_exa", Result: "found", ProviderExecuted: true, ProviderExecutedSet: true, Dynamic: true}},
				{Type: provider.ChunkTypeText, Text: "done"},
				{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
			}), nil
		}}
		res, err := StreamText(context.Background(), StreamTextOptions{Model: model, Prompt: "hi", Tools: []types.Tool{local}})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := res.ReadAll(); err != nil {
			t.Fatal(err)
		}
		steps := res.Steps()
		if len(steps) != 1 {
			t.Fatalf("steps = %d, want 1", len(steps))
		}
		r91genNoToolError(t, steps[0].Content, res.ResponseMessages())
	})
}

// core1-C02: generate-text.ts:1718 loops while shouldStartNextStep(), with
// no step ceiling, so StopWhen decides past 1000 steps.
func TestR91genGenerateNoStepCeiling(t *testing.T) {
	model := &testutil.MockLanguageModel{DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
		return &types.GenerateResult{
			Content:      []types.ContentPart{types.ToolCallContent{ToolCallID: "c", ToolName: "t", Input: `{}`}},
			ToolCalls:    []types.ToolCall{{ID: "c", ToolName: "t", Arguments: map[string]interface{}{}, RawArguments: `{}`}},
			FinishReason: types.FinishReasonToolCalls,
		}, nil
	}}
	tool := types.Tool{Name: "t", Parameters: map[string]interface{}{"type": "object"}, Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
		return "ok", nil
	}}
	res, err := GenerateText(context.Background(), GenerateTextOptions{Model: model, Prompt: "hi", Tools: []types.Tool{tool}, StopWhen: []StopCondition{IsStepCount(1005)}})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Steps) != 1005 {
		t.Fatalf("steps = %d, want 1005", len(res.Steps))
	}
}

// core1-D01 (gen half): generate-text.ts:1808 passes the final step's
// response to parseCompleteOutput, so NoObjectGeneratedError.Response is the
// model response (id, model id, headers).
func TestR91genOutputParseErrorResponse(t *testing.T) {
	model := &testutil.MockLanguageModel{DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
		return &types.GenerateResult{
			Content:          []types.ContentPart{types.TextContent{Text: "not json"}},
			FinishReason:     types.FinishReasonStop,
			Usage:            types.Usage{InputTokens: r91genInt64(3)},
			ResponseMetadata: &types.ResponseMetadata{ID: "resp-1", ModelID: "m-1"},
		}, nil
	}}
	type obj struct {
		Name string `json:"name"`
	}
	_, err := GenerateText(context.Background(), GenerateTextOptions{Model: model, Prompt: "hi", Output: ObjectOutput[obj](ObjectOutputOptions{Schema: SchemaFor[obj]()})})
	var noObj *NoObjectGeneratedError
	if !errors.As(err, &noObj) {
		t.Fatalf("err = %v, want NoObjectGeneratedError", err)
	}
	if noObj.Response == nil || noObj.Response.ID != "resp-1" || noObj.Response.ModelID != "m-1" {
		t.Fatalf("Response = %+v, want id resp-1 / model m-1", noObj.Response)
	}
	if noObj.Usage == nil || noObj.Usage.InputTokens == nil || *noObj.Usage.InputTokens != 3 {
		t.Fatalf("Usage = %+v", noObj.Usage)
	}
}

func r91genInt64(v int64) *int64 { return &v }

// r91genTimeoutTool blocks until its context ends and fails with the cause.
func r91genTimeoutTool() types.Tool {
	return types.Tool{Name: "slow", Parameters: map[string]interface{}{"type": "object"}, Execute: func(ctx context.Context, _ map[string]interface{}, _ types.ToolExecutionOptions) (interface{}, error) {
		<-ctx.Done()
		return nil, context.Cause(ctx)
	}}
}

// core1-A01 (gen tool sites): a configured tool timeout is the TS
// mergeAbortSignals(abortSignal, toolMs) reason even when it is zero
// (AbortSignal.timeout(0)), so the tool error is built with
// NewToolTimeoutError, which marks the duration as set.
func TestR91genZeroToolTimeoutMarkedSet(t *testing.T) {
	model := &testutil.MockLanguageModel{DoGenerateFunc: func(_ context.Context, o *provider.GenerateOptions) (*types.GenerateResult, error) {
		return &types.GenerateResult{
			Content:      []types.ContentPart{types.ToolCallContent{ToolCallID: "c", ToolName: "slow", Input: `{}`}},
			ToolCalls:    []types.ToolCall{{ID: "c", ToolName: "slow", Arguments: map[string]interface{}{}, RawArguments: `{}`}},
			FinishReason: types.FinishReasonToolCalls,
		}, nil
	}}
	zero := time.Duration(0)
	res, err := GenerateText(context.Background(), GenerateTextOptions{Model: model, Prompt: "hi", Tools: []types.Tool{r91genTimeoutTool()}, Timeout: &TimeoutConfig{ToolMs: &zero}})
	if err != nil {
		t.Fatal(err)
	}
	var toolErr error
	for _, part := range res.Steps[0].Content {
		if e, ok := part.(types.ToolErrorContent); ok {
			toolErr = e.Err
		}
	}
	var te *TimeoutError
	if !errors.As(toolErr, &te) {
		t.Fatalf("tool error = %v, want *TimeoutError", toolErr)
	}
	if te.Reason != TimeoutReasonTool || !te.timeoutSet {
		t.Fatalf("tool timeout error = %+v, want a tool timeout with its duration set", te)
	}
}

// r91genTelRecorder records the root and step start events.
type r91genTelRecorder struct {
	telemetry.NoopTelemetryIntegration
	mu     sync.Mutex
	starts []telemetry.TelemetryStartEvent
	steps  []telemetry.TelemetryStepStartEvent
}

func (r *r91genTelRecorder) OnStart(ctx context.Context, e telemetry.TelemetryStartEvent) context.Context {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.starts = append(r.starts, e)
	return ctx
}

func (r *r91genTelRecorder) OnStepStart(ctx context.Context, e telemetry.TelemetryStepStartEvent) context.Context {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.steps = append(r.steps, e)
	return ctx
}

func r91genTextModel() *testutil.MockLanguageModel {
	return &testutil.MockLanguageModel{DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
		return &types.GenerateResult{Content: []types.ContentPart{types.TextContent{Text: `{"a":1}`}}, Text: `{"a":1}`, FinishReason: types.FinishReasonStop}, nil
	}}
}

// core2-05 / core1-R02 (gen call sites, B3 fields): the step start event
// carries the step's instructions (TS promptMessages starts with the system
// message, convertToLanguageModelPrompt), and the root start event carries
// the caller's string prompt (TS standardizePrompt keeps it, so the legacy
// root ai.prompt is {"role":"user","content":"hi"}); both are input-gated.
func TestR91genTelemetryPromptFields(t *testing.T) {
	f := false
	for _, tc := range []struct {
		name         string
		opts         GenerateTextOptions
		recordInputs *bool
		wantSystem   string
		wantInstr    int
		wantPrompt   *string
	}{
		{name: "system and prompt string", opts: GenerateTextOptions{System: "sys", Prompt: "hi"}, wantSystem: "sys", wantPrompt: func() *string { s := "hi"; return &s }()},
		{name: "messages keep no prompt string", opts: GenerateTextOptions{System: "sys", Messages: []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}}}}, wantSystem: "sys"},
		{name: "instruction messages", opts: GenerateTextOptions{InstructionMessages: []types.Message{{Role: types.RoleSystem, Content: []types.ContentPart{types.TextContent{Text: "a"}}}}, Prompt: "hi"}, wantInstr: 1, wantPrompt: func() *string { s := "hi"; return &s }()},
		{name: "inputs not recorded", opts: GenerateTextOptions{System: "sys", Prompt: "hi"}, recordInputs: &f},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rec := &r91genTelRecorder{}
			opts := tc.opts
			opts.Model = r91genTextModel()
			opts.Telemetry = &TelemetrySettings{Integrations: []telemetry.TelemetryIntegration{rec}, RecordInputs: tc.recordInputs}
			if _, err := GenerateText(context.Background(), opts); err != nil {
				t.Fatal(err)
			}
			rec.mu.Lock()
			defer rec.mu.Unlock()
			if len(rec.starts) != 1 || len(rec.steps) != 1 {
				t.Fatalf("starts=%d steps=%d", len(rec.starts), len(rec.steps))
			}
			step := rec.steps[0]
			if step.System != tc.wantSystem || len(step.InstructionMessages) != tc.wantInstr {
				t.Errorf("step System=%q InstructionMessages=%d, want %q / %d", step.System, len(step.InstructionMessages), tc.wantSystem, tc.wantInstr)
			}
			got := rec.starts[0].PromptString
			if (got == nil) != (tc.wantPrompt == nil) || (got != nil && *got != *tc.wantPrompt) {
				t.Errorf("PromptString = %v, want %v", got, tc.wantPrompt)
			}
		})
	}
	t.Run("generateObject step", func(t *testing.T) {
		rec := &r91genTelRecorder{}
		_, err := GenerateObject(context.Background(), GenerateObjectOptions{
			Model: r91genTextModel(), System: "sys", Prompt: "hi",
			Schema:    schema.NewSimpleJSONSchema(map[string]interface{}{"type": "object"}),
			Telemetry: &TelemetrySettings{Integrations: []telemetry.TelemetryIntegration{rec}},
		})
		if err != nil {
			t.Fatal(err)
		}
		rec.mu.Lock()
		defer rec.mu.Unlock()
		if len(rec.steps) != 1 || rec.steps[0].System != "sys" {
			t.Fatalf("object step events = %+v, want System sys", rec.steps)
		}
	})
}

// core1-O01: a StreamObject prompt preparation failure (here a failed
// download) reaches OnError once: TS streamObject's catch enqueues an error
// part that the event processor reports to onError (stream-object.ts:532-541,
// 948-960).
func TestR91genStreamObjectPrepErrorOnError(t *testing.T) {
	model := &testutil.MockLanguageModel{DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
		t.Error("model called")
		return nil, errors.New("unreachable")
	}}
	downloadErr := errors.New("download failed")
	var mu sync.Mutex
	var got []error
	_, err := StreamObject(context.Background(), StreamObjectOptions{
		Model:    model,
		Messages: []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.ImageContent{URL: "https://example.com/a.png"}}}},
		Schema:   schema.NewSimpleJSONSchema(map[string]interface{}{"type": "object"}),
		ExperimentalDownload: func(context.Context, []promptutils.DownloadRequest) ([]*promptutils.DownloadResult, error) {
			return nil, downloadErr
		},
		OnError: func(_ context.Context, err error) {
			mu.Lock()
			defer mu.Unlock()
			got = append(got, err)
		},
	})
	if err == nil {
		t.Fatal("want an error")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(got) != 1 || !errors.Is(got[0], downloadErr) {
		t.Fatalf("OnError calls = %v, want the download error once", got)
	}
}

// core1-O02: generateObject / streamObject call IDs come from their
// generateId, createIdGenerator({prefix: 'aiobj', size: 24})
// (generate-object.ts:46,351; stream-object.ts:70,570).
func TestR91genObjectCallIDPrefix(t *testing.T) {
	sch := schema.NewSimpleJSONSchema(map[string]interface{}{"type": "object"})
	var mu sync.Mutex
	var ids []string
	onStart := func(_ context.Context, e ObjectOnStartEvent) {
		mu.Lock()
		defer mu.Unlock()
		ids = append(ids, e.CallID)
	}
	if _, err := GenerateObject(context.Background(), GenerateObjectOptions{Model: r91genTextModel(), Prompt: "hi", Schema: sch, OnStart: onStart}); err != nil {
		t.Fatal(err)
	}
	streamModel := &testutil.MockLanguageModel{DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
		return testutil.NewMockTextStream([]provider.StreamChunk{{Type: provider.ChunkTypeText, Text: `{"a":1}`}, {Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop}}), nil
	}}
	res, err := StreamObject(context.Background(), StreamObjectOptions{Model: streamModel, Prompt: "hi", Schema: sch, OnStart: onStart})
	if err != nil {
		t.Fatal(err)
	}
	_ = res
	mu.Lock()
	defer mu.Unlock()
	if len(ids) != 2 {
		t.Fatalf("call ids = %v", ids)
	}
	for _, id := range ids {
		if !strings.HasPrefix(id, "aiobj-") || len(id) != len("aiobj-")+24 {
			t.Errorf("call id %q, want aiobj-<24>", id)
		}
	}
}

// r91genCountingSchema counts element validations.
type r91genCountingSchema struct {
	inner schema.Schema
	calls *int
}

func (s r91genCountingSchema) Validator() schema.Validator { return r91genCountingValidator(s) }

type r91genCountingValidator r91genCountingSchema

func (v r91genCountingValidator) Validate(data interface{}) error {
	*v.calls++
	return v.inner.Validator().Validate(data)
}

func (v r91genCountingValidator) JSONSchema() map[string]interface{} {
	return v.inner.Validator().JSONSchema()
}

// r91genReparsePartials is the partial sequence of the reparsing path
// (parseStreamPartial over the accumulated text, published when it changes).
func r91genReparsePartials(s schema.Schema, deltas []string) []string {
	var out []string
	acc := ""
	var last interface{}
	have := false
	for _, d := range deltas {
		acc += d
		if partial, ok := parseStreamPartial(ObjectModeArray, s, nil, parsePartialJSON(acc)); ok && (!have || !deepEqual(partial, last)) {
			last, have = partial, true
			b, _ := json.Marshal(partial)
			out = append(out, string(b))
		}
	}
	return out
}

func r91genStreamObjectPartials(t *testing.T, s schema.Schema, deltas []string) []string {
	t.Helper()
	chunks := []provider.StreamChunk{{Type: provider.ChunkTypeStreamStart}}
	for _, d := range deltas {
		chunks = append(chunks, provider.StreamChunk{Type: provider.ChunkTypeText, Text: d})
	}
	chunks = append(chunks, provider.StreamChunk{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop})
	model := &testutil.MockLanguageModel{StructuredSupport: true, DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
		return testutil.NewMockTextStream(chunks), nil
	}}
	var out []string
	_, _ = StreamObject(context.Background(), StreamObjectOptions{
		Model: model, Prompt: "p", OutputMode: ObjectModeArray, Schema: s,
		OnChunk: func(partial interface{}) {
			b, _ := json.Marshal(partial)
			out = append(out, string(b))
		},
	})
	return out
}

// TS 91a67c2f56 (stream-object side): array output scans each new delta
// once (util/array-output-stream.ts) and publishes the same partial arrays
// as reparsing the accumulated text, falling back to reparsing for other
// envelopes and malformed or invalid elements; each element is validated
// once instead of once per chunk.
func TestR91genStreamObjectArrayIncremental(t *testing.T) {
	elem := schema.NewSimpleJSONSchema(map[string]interface{}{
		"type":       "object",
		"properties": map[string]interface{}{"q": map[string]interface{}{"type": "string"}, "n": map[string]interface{}{"type": "number"}},
		"required":   []interface{}{"q"},
	})
	texts := []string{
		`{"elements":[{"q":"a,]}","n":1},{"q":"b\"[","n":2}, {"q":"é😀"} ]}`,
		` { "elements" : [ ] } `,
		"{\n\"elements\":[{\"q\":\"x\"}\n,\n{\"q\":\"y\"}]}",
		`{"elements":[{"q":"a"},{"n":1},{"q":"c"}]}`,
		`{"other":1,"elements":[{"q":"a"}]}`,
		`{"elements":[{"q":"a"},,{"q":"b"}]}`,
		`{"elements":[{"q":"a"}]} x`,
		`{"elements":[{"q":"a"}`,
	}
	for _, text := range texts {
		runes := []rune(text)
		for _, size := range []int{1, 2, 3, 7, len(runes)} {
			var deltas []string
			for i := 0; i < len(runes); i += size {
				end := i + size
				if end > len(runes) {
					end = len(runes)
				}
				deltas = append(deltas, string(runes[i:end]))
			}
			want := r91genReparsePartials(elem, deltas)
			got := r91genStreamObjectPartials(t, elem, deltas)
			if strings.Join(got, " | ") != strings.Join(want, " | ") {
				t.Errorf("%q size %d:\n got %v\nwant %v", text, size, got, want)
			}
		}
	}

	t.Run("validates each element once", func(t *testing.T) {
		var b strings.Builder
		b.WriteString(`{"elements":[`)
		for i := 0; i < 50; i++ {
			if i > 0 {
				b.WriteString(",")
			}
			fmt.Fprintf(&b, `{"q":"item %d","n":%d}`, i, i)
		}
		b.WriteString(`]}`)
		var deltas []string
		text := b.String()
		for i := 0; i < len(text); i += 4 {
			end := i + 4
			if end > len(text) {
				end = len(text)
			}
			deltas = append(deltas, text[i:end])
		}
		calls := 0
		got := r91genStreamObjectPartials(t, r91genCountingSchema{inner: elem, calls: &calls}, deltas)
		if len(got) != 51 {
			t.Fatalf("partials = %d, want 51", len(got))
		}
		// 50 incremental validations plus the final result's validation.
		if calls > 120 {
			t.Fatalf("validations = %d, want about one per element", calls)
		}
	})
}

// TS 2ebc78b654 (generated-file data, ruling 1): experimental_download also
// handles the generated file URLs a model returns, called with
// isUrlSupportedByModel false; a null result is a DownloadError. Without it
// the default validated downloader is used (protection stays on).
func TestR91genGeneratedFileUsesExperimentalDownload(t *testing.T) {
	model := &testutil.MockLanguageModel{DoGenerateFunc: func(context.Context, *provider.GenerateOptions) (*types.GenerateResult, error) {
		return &types.GenerateResult{
			Content:      []types.ContentPart{types.GeneratedFileContent{MediaType: "image/png", FileData: types.FileData{Type: types.FileDataTypeURL, URL: "https://files.example.com/a b.png"}}},
			FinishReason: types.FinishReasonStop,
		}, nil
	}}
	t.Run("custom download", func(t *testing.T) {
		var got []promptutils.DownloadRequest
		res, err := GenerateText(context.Background(), GenerateTextOptions{Model: model, Prompt: "hi",
			ExperimentalDownload: func(_ context.Context, reqs []promptutils.DownloadRequest) ([]*promptutils.DownloadResult, error) {
				got = append(got, reqs...)
				return []*promptutils.DownloadResult{{Data: []byte("png"), MediaType: "image/png"}}, nil
			}})
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 || got[0].URL != "https://files.example.com/a b.png" || got[0].IsURLSupportedByModel {
			t.Fatalf("download requests = %+v", got)
		}
		if len(res.Files) != 1 || string(res.Files[0].Data) != "png" {
			t.Fatalf("files = %+v", res.Files)
		}
	})
	t.Run("nil result", func(t *testing.T) {
		_, err := GenerateText(context.Background(), GenerateTextOptions{Model: model, Prompt: "hi",
			ExperimentalDownload: func(context.Context, []promptutils.DownloadRequest) ([]*promptutils.DownloadResult, error) {
				return []*promptutils.DownloadResult{nil}, nil
			}})
		var dlErr *providererrors.DownloadError
		if !errors.As(err, &dlErr) || err.Error() != "Generated file URL https://files.example.com/a%20b.png must be downloaded, but the download function returned null" || dlErr.URL != "https://files.example.com/a%20b.png" {
			t.Fatalf("err = %v", err)
		}
	})
	t.Run("custom error is returned as is", func(t *testing.T) {
		boom := errors.New("boom")
		_, err := GenerateText(context.Background(), GenerateTextOptions{Model: model, Prompt: "hi",
			ExperimentalDownload: func(context.Context, []promptutils.DownloadRequest) ([]*promptutils.DownloadResult, error) {
				return nil, boom
			}})
		if !errors.Is(err, boom) || providererrors.IsDownloadError(err) {
			t.Fatalf("err = %v", err)
		}
	})
}

type r91genRoundTripper func(*http.Request) (*http.Response, error)

func (f r91genRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// TS 2ebc78b654 createDownload({fetch}): the download uses the given
// transport; URL validation stays on (a private host is still rejected
// before the transport is called; Go also keeps the DNS pre-check).
func TestR91genCreateDownloadTransport(t *testing.T) {
	var calls []string
	download := CreateDownload(&DownloadOptions{Transport: r91genRoundTripper(func(r *http.Request) (*http.Response, error) {
		calls = append(calls, r.URL.String())
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"text/plain"}}, Body: io.NopCloser(strings.NewReader("Hello World")), Request: r}, nil
	})})
	results, err := download(context.Background(), []DownloadRequest{
		{URL: "https://93.184.215.14/supported", IsURLSupportedByModel: true},
		{URL: "https://93.184.215.14/unsupported"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 2 || results[0] != nil || results[1] == nil || string(results[1].Data) != "Hello World" || results[1].MediaType != "text/plain" {
		t.Fatalf("results = %+v", results)
	}
	if len(calls) != 1 || calls[0] != "https://93.184.215.14/unsupported" {
		t.Fatalf("transport calls = %v", calls)
	}
	if _, err := download(context.Background(), []DownloadRequest{{URL: "http://127.0.0.1/x"}}); err == nil || len(calls) != 1 {
		t.Fatalf("private URL: err = %v, calls = %v", err, calls)
	}
}
