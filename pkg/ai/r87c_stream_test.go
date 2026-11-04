package ai

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// Round 87c (stream): faithful ports of the stream-text.test.ts cases that
// Appendix A still listed as partial (TS lines below 20263). Each test names
// the TS line it ports. Expected values are the TS inline snapshots
// converted to JSON (undefined keys dropped). Two translations apply
// throughout:
//   - performance: TS mocks the clock, so every number is 0; Go measures real
//     time, so the projection keeps the TS key set and zeroes the numbers on
//     both sides (r87cstreamZeroPerformance).
//   - fallback response timestamps: TS mocks now() to the epoch; Go stamps
//     the real time, which the tests check is current and then replace with
//     the epoch.

// r87cstreamTestUsage is TS testUsage (noCache 3, text 10).
func r87cstreamTestUsage() *types.Usage {
	in, out, total := int64(3), int64(10), int64(13)
	return &types.Usage{InputTokens: &in, OutputTokens: &out, TotalTokens: &total,
		InputDetails:  &types.InputTokenDetails{NoCacheTokens: &in},
		OutputDetails: &types.OutputTokenDetails{TextTokens: &out}}
}

func r87cstreamFinish(reason types.FinishReason, raw string) provider.StreamChunk {
	return provider.StreamChunk{Type: provider.ChunkTypeFinish, FinishReason: reason, RawFinishReason: raw, Usage: r87cstreamTestUsage()}
}

func r87cstreamMeta(id string) provider.StreamChunk {
	return provider.StreamChunk{Type: provider.ChunkTypeResponseMetadata, ResponseMetadata: &provider.ResponseMetadata{ID: id, ModelID: "mock-model-id", Timestamp: time.UnixMilli(0).UTC()}}
}

// r87cstreamDefaultInternal is TS defaultSettings()._internal: mockId({prefix:'id'}).
func r87cstreamDefaultInternal() *InternalOptions {
	return &InternalOptions{GenerateID: r73streamCounterIDs("id"), GenerateCallID: func() string { return "test-telemetry-call-id" }}
}

func r87cstreamRawMD(s string) json.RawMessage { return json.RawMessage(s) }

func r87cstreamFileJSON(mediaType string, fd types.FileData, data []byte) map[string]interface{} {
	b64 := fd.DataString
	if b64 == "" {
		raw := fd.Data
		if len(raw) == 0 {
			raw = data
		}
		b64 = base64.StdEncoding.EncodeToString(raw)
	}
	return map[string]interface{}{"base64Data": b64, "mediaType": mediaType, "type": "file"}
}

// r87cstreamTSPart projects a Go full-stream part onto the TS TextStreamPart
// snapshot shape. Go-only lifecycle markers return nil.
func r87cstreamTSPart(c provider.StreamChunk) map[string]interface{} {
	m := map[string]interface{}{"type": string(c.Type)}
	md := func(raw json.RawMessage) {
		if len(raw) > 0 && string(raw) != "null" {
			m["providerMetadata"] = raw
		}
	}
	switch c.Type {
	case provider.ChunkTypeResponseMetadata, provider.ChunkTypeFirstChunk, provider.ChunkTypeStreamFinish, provider.ChunkTypeStreamStart, provider.ChunkTypeDiscardStep:
		return nil
	case provider.ChunkTypeStartStep:
		req := map[string]interface{}{}
		if c.Request != nil {
			if c.Request.Body != nil {
				req["body"] = c.Request.Body
			}
			if c.Request.Messages != nil {
				req["messages"] = c.Request.Messages
			}
		}
		m["request"] = req
		w := c.Warnings
		if w == nil {
			w = []types.Warning{}
		}
		m["warnings"] = w
	case provider.ChunkTypeTextStart, provider.ChunkTypeTextEnd, provider.ChunkTypeReasoningStart, provider.ChunkTypeReasoningEnd:
		m["id"] = c.ID
		md(c.ProviderMetadata)
	case provider.ChunkTypeText:
		m["type"], m["id"], m["text"] = "text-delta", c.ID, c.Text
		md(c.ProviderMetadata)
	case provider.ChunkTypeReasoning:
		text := c.Reasoning
		if text == "" {
			text = c.Text
		}
		m["type"], m["id"], m["text"] = "reasoning-delta", c.ID, text
		md(c.ProviderMetadata)
	case provider.ChunkTypeFile:
		f := c.GeneratedFileContent
		m["file"] = r87cstreamFileJSON(f.MediaType, f.FileData, f.Data)
		md(f.ProviderMetadata)
		md(c.ProviderMetadata)
	case provider.ChunkTypeReasoningFile:
		f := c.ReasoningFileContent
		m["file"] = r87cstreamFileJSON(f.MediaType, f.FileData, f.Data)
		md(f.ProviderMetadata)
		md(c.ProviderMetadata)
	case provider.ChunkTypeCustom:
		m["kind"] = c.CustomContent.Kind
		md(c.CustomContent.ProviderMetadata)
		md(c.ProviderMetadata)
	case provider.ChunkTypeSource:
		s := c.SourceContent
		b, _ := json.Marshal(s)
		_ = json.Unmarshal(b, &m)
		m["type"] = "source"
	case provider.ChunkTypeToolInputStart:
		m["id"], m["toolName"] = c.ID, c.ToolCall.ToolName
		if c.ToolCall.ProviderExecuted {
			m["providerExecuted"] = true
		}
		if c.ToolCall.Dynamic {
			m["dynamic"] = true
		}
		if c.ToolCall.Title != "" {
			m["title"] = c.ToolCall.Title
		}
		md(c.ProviderMetadata)
	case provider.ChunkTypeToolInputDelta:
		m["id"], m["delta"] = c.ID, c.Text
		md(c.ProviderMetadata)
	case provider.ChunkTypeToolInputEnd:
		m["id"] = c.ID
		md(c.ProviderMetadata)
	case provider.ChunkTypeToolCall:
		tc := c.ToolCall
		m["toolCallId"], m["toolName"], m["input"] = tc.ID, tc.ToolName, tc.Arguments
		if tc.ProviderExecuted {
			m["providerExecuted"] = true
		}
		if tc.Dynamic {
			m["dynamic"] = true
		}
		if tc.Title != "" {
			m["title"] = tc.Title
		}
		if len(tc.ProviderMetadata) > 0 {
			m["providerMetadata"] = tc.ProviderMetadata
		}
	case provider.ChunkTypeToolResult:
		tr := c.ToolResult
		m["toolCallId"], m["toolName"], m["input"] = tr.ToolCallID, tr.ToolName, tr.Input
		if tr.Error != nil {
			m["type"], m["error"] = "tool-error", tr.Error.Error()
		} else {
			m["output"] = tr.Result
		}
		if tr.ProviderExecuted {
			m["providerExecuted"] = true
		}
		if tr.Dynamic {
			m["dynamic"] = true
		}
		if tr.Preliminary {
			m["preliminary"] = true
		}
		if tr.Title != "" {
			m["title"] = tr.Title
		}
		if len(tr.ProviderMetadata) > 0 {
			m["providerMetadata"] = tr.ProviderMetadata
		}
	case provider.ChunkTypeFinishStep:
		m["finishReason"], m["rawFinishReason"] = string(c.FinishReason), c.RawFinishReason
		if c.Response != nil {
			resp := map[string]interface{}{"id": c.Response.ID, "modelId": c.Response.ModelID, "timestamp": c.Response.Timestamp.UTC().Format("2006-01-02T15:04:05.000Z07:00")}
			if len(c.Response.Headers) > 0 {
				resp["headers"] = c.Response.Headers
			}
			m["response"] = resp
		}
		m["usage"] = c.Usage
		if c.Performance != nil {
			m["performance"] = c.Performance
		}
		md(c.ProviderMetadata)
	case provider.ChunkTypeFinish:
		m["finishReason"], m["rawFinishReason"] = string(c.FinishReason), c.RawFinishReason
		m["totalUsage"] = c.Usage
	case provider.ChunkTypeError:
		if c.Err != nil {
			m["error"] = c.Err.Error()
		} else {
			m["error"] = c.Text
		}
	case provider.ChunkTypeAbort:
		if c.AbortReason != "" {
			m["reason"] = c.AbortReason
		}
	case provider.ChunkTypeRaw:
		m["rawValue"] = c.Raw
	}
	return m
}

func r87cstreamTSParts(parts []provider.StreamChunk) []interface{} {
	out := []interface{}{}
	for _, p := range parts {
		if m := r87cstreamTSPart(p); m != nil {
			out = append(out, m)
		}
	}
	return out
}

// r87cstreamZeroPerformance zeroes every number below a "performance" key.
func r87cstreamZeroPerformance(v interface{}, inPerf bool) interface{} {
	switch x := v.(type) {
	case map[string]interface{}:
		for k, e := range x {
			x[k] = r87cstreamZeroPerformance(e, inPerf || k == "performance")
		}
		return x
	case []interface{}:
		for i, e := range x {
			x[i] = r87cstreamZeroPerformance(e, inPerf)
		}
		return x
	case float64:
		if inPerf {
			return float64(0)
		}
	}
	return v
}

// r87cstreamPerfKeys is the TS performance key set every finish-step carries.
const r87cstreamPerfKeys = `{"effectiveOutputTokensPerSecond":0,"effectiveTotalTokensPerSecond":0,"inputTokensPerSecond":0,"outputTokensPerSecond":0,"responseTimeMs":0,"stepTimeMs":0,"timeBetweenOutputChunksMs":{"avg":0,"max":0,"median":0,"min":0,"p10":0,"p90":0},"timeToFirstOutputMs":0,"toolExecutionMs":{}}`

func r87cstreamAssert(t *testing.T, label string, got interface{}, want string) {
	t.Helper()
	var w interface{}
	if err := json.Unmarshal([]byte(want), &w); err != nil {
		t.Fatalf("%s: bad want JSON: %v", label, err)
	}
	g := r75streamNormalize(t, got)
	g = r87cstreamZeroPerformance(g, false)
	w = r87cstreamZeroPerformance(w, false)
	if !reflect.DeepEqual(g, w) {
		gb, _ := json.Marshal(g)
		wb, _ := json.Marshal(w)
		t.Fatalf("%s:\n got  %s\n want %s", label, gb, wb)
	}
}

// r87cstreamEpochFallback checks that every finish-step response timestamp
// that is not the epoch is the current time and replaces it with the epoch
// (TS mocks now() to new Date(0)).
func r87cstreamEpochFallback(t *testing.T, parts []provider.StreamChunk) {
	t.Helper()
	for i := range parts {
		if parts[i].Type != provider.ChunkTypeFinishStep || parts[i].Response == nil || parts[i].Response.Timestamp.UnixMilli() == 0 {
			continue
		}
		if d := time.Since(parts[i].Response.Timestamp); d < -time.Second || d > time.Minute {
			t.Fatalf("fallback timestamp %v is not now", parts[i].Response.Timestamp)
		}
		resp := *parts[i].Response
		resp.Timestamp = time.UnixMilli(0)
		parts[i].Response = &resp
	}
}

func r87cstreamDataFile(mediaType, data string, md json.RawMessage) provider.StreamChunk {
	return provider.StreamChunk{Type: provider.ChunkTypeFile, GeneratedFileContent: &types.GeneratedFileContent{MediaType: mediaType, FileData: types.FileData{Type: types.FileDataTypeData, DataString: data}, ProviderMetadata: md}}
}

func r87cstreamReasoningFile(mediaType, data string, md json.RawMessage) provider.StreamChunk {
	return provider.StreamChunk{Type: provider.ChunkTypeReasoningFile, ReasoningFileContent: &types.ReasoningFileContent{MediaType: mediaType, FileData: types.FileData{Type: types.FileDataTypeData, DataString: data}, ProviderMetadata: md}}
}

// r87cstreamModelWithFiles is TS modelWithFiles.
func r87cstreamModelWithFiles() *testutil.MockLanguageModel {
	return r71streamModel(nil, []provider.StreamChunk{
		r87cstreamDataFile("text/plain", "Hello World", nil),
		{Type: provider.ChunkTypeTextStart, ID: "1"},
		{Type: provider.ChunkTypeText, ID: "1", Text: "Hello!"},
		{Type: provider.ChunkTypeTextEnd, ID: "1"},
		r87cstreamDataFile("image/jpeg", "QkFVRw==", nil),
		r87cstreamFinish(types.FinishReasonStop, "stop"),
	})
}

// r87cstreamModelWithReasoningFiles is TS modelWithReasoningFiles.
func r87cstreamModelWithReasoningFiles() *testutil.MockLanguageModel {
	return r71streamModel(nil, []provider.StreamChunk{
		r87cstreamMeta("id-0"),
		r87cstreamReasoningFile("image/png", "reasoning-file-data-1", nil),
		{Type: provider.ChunkTypeReasoningStart, ID: "1"},
		{Type: provider.ChunkTypeReasoning, ID: "1", Reasoning: "Some reasoning text."},
		{Type: provider.ChunkTypeReasoningEnd, ID: "1"},
		r87cstreamReasoningFile("image/jpeg", "reasoning-file-data-2", r87cstreamRawMD(`{"testProvider":{"signature":"rf-sig-1"}}`)),
		{Type: provider.ChunkTypeTextStart, ID: "1"},
		{Type: provider.ChunkTypeText, ID: "1", Text: "Hello!"},
		{Type: provider.ChunkTypeTextEnd, ID: "1"},
		r87cstreamFinish(types.FinishReasonStop, "stop"),
	})
}

const r87cstreamFinishStepStop = `{"finishReason":"stop","performance":` + r87cstreamPerfKeys + `,"rawFinishReason":"stop","response":{"id":"id-0","modelId":"mock-model-id","timestamp":"1970-01-01T00:00:00.000Z"},"type":"finish-step","usage":{"inputTokenDetails":{"noCacheTokens":3},"inputTokens":3,"outputTokenDetails":{"textTokens":10},"outputTokens":10,"totalTokens":13}}`
const r87cstreamFinishStop = `{"finishReason":"stop","rawFinishReason":"stop","totalUsage":{"inputTokenDetails":{"noCacheTokens":3},"inputTokens":3,"outputTokenDetails":{"textTokens":10},"outputTokens":10,"totalTokens":13},"type":"finish"}`

// stream-text.test.ts 1284: result.stream > should send files.
func TestR87cstreamSendFiles(t *testing.T) {
	_, parts := r73streamParts(t, StreamTextOptions{Model: r87cstreamModelWithFiles(), Prompt: "prompt", Internal: r87cstreamDefaultInternal()})
	r87cstreamEpochFallback(t, parts)
	r87cstreamAssert(t, "parts", r87cstreamTSParts(parts), `[{"type":"start"},{"request":{},"type":"start-step","warnings":[]},`+
		`{"file":{"base64Data":"Hello World","mediaType":"text/plain","type":"file"},"type":"file"},`+
		`{"id":"1","type":"text-start"},{"id":"1","text":"Hello!","type":"text-delta"},{"id":"1","type":"text-end"},`+
		`{"file":{"base64Data":"QkFVRw==","mediaType":"image/jpeg","type":"file"},"type":"file"},`+
		r87cstreamFinishStepStop+`,`+r87cstreamFinishStop+`]`)
}

// stream-text.test.ts 1449: result.stream > should send reasoning-files.
func TestR87cstreamSendReasoningFiles(t *testing.T) {
	_, parts := r73streamParts(t, StreamTextOptions{Model: r87cstreamModelWithReasoningFiles(), Prompt: "prompt", Internal: r87cstreamDefaultInternal()})
	r87cstreamAssert(t, "parts", r87cstreamTSParts(parts), `[{"type":"start"},{"request":{},"type":"start-step","warnings":[]},`+
		`{"file":{"base64Data":"reasoning-file-data-1","mediaType":"image/png","type":"file"},"type":"reasoning-file"},`+
		`{"id":"1","type":"reasoning-start"},{"id":"1","text":"Some reasoning text.","type":"reasoning-delta"},{"id":"1","type":"reasoning-end"},`+
		`{"file":{"base64Data":"reasoning-file-data-2","mediaType":"image/jpeg","type":"file"},"providerMetadata":{"testProvider":{"signature":"rf-sig-1"}},"type":"reasoning-file"},`+
		`{"id":"1","type":"text-start"},{"id":"1","text":"Hello!","type":"text-delta"},{"id":"1","type":"text-end"},`+
		r87cstreamFinishStepStop+`,`+r87cstreamFinishStop+`]`)
}

// stream-text.test.ts 2085: result.stream > tool choice enforcement > should
// surface an error when a required tool choice produces no tool call.
func TestR87cstreamRequiredToolChoiceNoToolCall(t *testing.T) {
	var errs r82streamErrors
	r, err := StreamText(context.Background(), StreamTextOptions{
		Model: r71streamModel(nil, []provider.StreamChunk{
			{Type: provider.ChunkTypeReasoningStart, ID: "1"},
			{Type: provider.ChunkTypeReasoning, ID: "1", Reasoning: "I will not call the tool."},
			{Type: provider.ChunkTypeReasoningEnd, ID: "1"},
			{Type: provider.ChunkTypeTextStart, ID: "2"},
			{Type: provider.ChunkTypeText, ID: "2", Text: "No tool call."},
			{Type: provider.ChunkTypeTextEnd, ID: "2"},
			r87cstreamFinish(types.FinishReasonStop, "stop"),
		}),
		Tools:      []types.Tool{{Name: "tool1", Parameters: r82streamValueSchema}},
		ToolChoice: types.ToolChoice{Type: types.ToolChoiceRequired},
		Prompt:     "test-input",
		OnError:    errs.onError,
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = r.ConsumeStream()
	got := errs.all()
	if len(got) != 1 {
		t.Fatalf("onError calls = %d (%v)", len(got), got)
	}
	var v *ToolChoiceViolationError
	if !errors.As(got[0], &v) {
		t.Fatalf("error = %T %v", got[0], got[0])
	}
	if v.Error() != "Model response did not contain a tool call even though tool choice was required." {
		t.Fatalf("message = %q", v.Error())
	}
	if v.ToolChoice.Type != types.ToolChoiceRequired || v.FinishReason != types.FinishReasonStop || v.Provider != "mock-provider" || v.ModelID != "mock-model-id" {
		t.Fatalf("violation = %+v", v)
	}
	if len(v.Content) != 2 {
		t.Fatalf("content = %#v", v.Content)
	}
	if rc, ok := v.Content[0].(types.ReasoningContent); !ok || rc.Text != "I will not call the tool." {
		t.Fatalf("content[0] = %#v", v.Content[0])
	}
	if tc, ok := v.Content[1].(types.TextContent); !ok || tc.Text != "No tool call." {
		t.Fatalf("content[1] = %#v", v.Content[1])
	}
	if fr := r.FinishReason(); fr != types.FinishReasonError {
		t.Fatalf("finishReason = %q, want error", fr)
	}
	r87cstreamAssert(t, "usage", r.Usage(), `{"inputTokenDetails":{"noCacheTokens":3},"inputTokens":3,"outputTokenDetails":{"textTokens":10},"outputTokens":10,"totalTokens":13}`)
}

// stream-text.test.ts 2714: result.stream > should filter out empty text
// deltas.
func TestR87cstreamFullStreamFiltersEmptyTextDeltas(t *testing.T) {
	steps := []provider.StreamChunk{r87cstreamMeta("id-0"), {Type: provider.ChunkTypeTextStart, ID: "1"}}
	for _, d := range []string{"", "Hello", "", ", ", "", "world!", ""} {
		steps = append(steps, provider.StreamChunk{Type: provider.ChunkTypeText, ID: "1", Text: d})
	}
	steps = append(steps, provider.StreamChunk{Type: provider.ChunkTypeTextEnd, ID: "1"}, r87cstreamFinish(types.FinishReasonStop, "stop"))
	_, parts := r73streamParts(t, StreamTextOptions{Model: r71streamModel(nil, steps), Prompt: "test-input"})
	r87cstreamAssert(t, "parts", r87cstreamTSParts(parts), `[{"type":"start"},{"request":{},"type":"start-step","warnings":[]},`+
		`{"id":"1","type":"text-start"},{"id":"1","text":"Hello","type":"text-delta"},{"id":"1","text":", ","type":"text-delta"},{"id":"1","text":"world!","type":"text-delta"},{"id":"1","type":"text-end"},`+
		r87cstreamFinishStepStop+`,`+r87cstreamFinishStop+`]`)
}

// r87cstreamThrowingModel is a model whose doStream throws "test error".
func r87cstreamThrowingModel() *testutil.MockLanguageModel {
	return &testutil.MockLanguageModel{ProviderName: "mock-provider", ModelName: "mock-model-id", DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
		return nil, errors.New("test error")
	}}
}

// stream-text.test.ts 2892: errors > should swallow error to prevent server
// crash (the textStream snapshot is `[]`).
func TestR87cstreamSwallowErrorTextStream(t *testing.T) {
	r, err := StreamText(context.Background(), StreamTextOptions{Model: r87cstreamThrowingModel(), Prompt: "test-input", OnError: func(context.Context, error) {}})
	if err != nil {
		t.Fatal(err)
	}
	out, errCh := ToTextStream(context.Background(), r.FullStream())
	got := []string{}
	for s := range out {
		got = append(got, s)
	}
	<-errCh
	if len(got) != 0 {
		t.Fatalf("textStream = %q, want []", got)
	}
}

// stream-text.test.ts 4411: errors > should reject text promise when error is
// thrown. Go's accessors do not reject: Text() is empty, and OutputErr()
// reports the NoOutputGeneratedError TS rejects result.output with.
func TestR87cstreamRejectTextWhenErrorThrown(t *testing.T) {
	r, err := StreamText(context.Background(), StreamTextOptions{Model: r87cstreamThrowingModel(), Prompt: "test-input", OnError: func(context.Context, error) {}})
	if err != nil {
		t.Fatal(err)
	}
	_ = r.ConsumeStream()
	if r.Text() != "" {
		t.Fatalf("text = %q", r.Text())
	}
	done := make(chan struct{})
	go func() { _ = r.Output(); _ = r.OutputErr(); close(done) }()
	select {
	case <-done:
	case <-time.After(100 * time.Millisecond):
		t.Fatal("output did not settle")
	}
	var noOutput *NoOutputGeneratedError
	if oe := r.OutputErr(); !errors.As(oe, &noOutput) || noOutput.Message != "No output generated. Check the stream for errors." {
		t.Fatalf("OutputErr = %v", oe)
	}
	if r.Output() != nil {
		t.Fatalf("Output = %#v", r.Output())
	}
}

// stream-text.test.ts 5099: retries > should retry after a 500 error and
// stream the successful response.
func TestR87cstreamRetryAfter500(t *testing.T) {
	calls := 0
	model := &testutil.MockLanguageModel{ProviderName: "mock-provider", ModelName: "mock-model-id", DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
		calls++
		if calls == 1 {
			return nil, r87cstreamAPICallError500()
		}
		return testutil.NewMockTextStream([]provider.StreamChunk{
			{Type: provider.ChunkTypeTextStart, ID: "1"},
			{Type: provider.ChunkTypeText, ID: "1", Text: "hello"},
			{Type: provider.ChunkTypeText, ID: "1", Text: " "},
			{Type: provider.ChunkTypeText, ID: "1", Text: "world"},
			{Type: provider.ChunkTypeTextEnd, ID: "1"},
			r87cstreamFinish(types.FinishReasonStop, "stop"),
		}), nil
	}}
	r, err := StreamText(context.Background(), StreamTextOptions{Model: model, Prompt: "test-input"})
	if err != nil {
		t.Fatal(err)
	}
	out, errCh := ToTextStream(context.Background(), r.FullStream())
	got := []string{}
	for s := range out {
		got = append(got, s)
	}
	if err := <-errCh; err != nil {
		t.Fatal(err)
	}
	if strings.Join(got, "|") != "hello| |world" || len(got) != 3 {
		t.Fatalf("textStream = %q", got)
	}
	if calls != 2 {
		t.Fatalf("doStream calls = %d, want 2", calls)
	}
}

// r87cstreamAPICallError500 is the TS APICallError (500, retry-after-ms 1).
func r87cstreamAPICallError500() error {
	return &providererrors.ProviderError{Provider: "mock-provider", StatusCode: 500, Message: "Internal Server Error", ResponseHeaders: map[string]string{"retry-after-ms": "1"}}
}

// TS flush (stream-text.ts 1894-1927) rejects result.output with the
// no-finish error when the model stream closes before its finish chunk
// (stream-text.test.ts 4435 asserts that error on result.text).
func TestR87cstreamOutputErrNoFinish(t *testing.T) {
	r, err := StreamText(context.Background(), StreamTextOptions{Model: r71streamModel(nil, []provider.StreamChunk{{Type: provider.ChunkTypeStreamStart}, r87cstreamMeta("id-0")}), Prompt: "test-input", OnError: func(context.Context, error) {}})
	if err != nil {
		t.Fatal(err)
	}
	_ = r.ConsumeStream()
	var noOutput *NoOutputGeneratedError
	if oe := r.OutputErr(); !errors.As(oe, &noOutput) || oe.Error() != "No output generated. The model stream ended without a finish chunk." {
		t.Fatalf("OutputErr = %v, Err = %v", oe, r.Err())
	}
}
