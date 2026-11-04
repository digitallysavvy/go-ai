package ai

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"reflect"
	"strings"
	"sync"
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

// r87cstreamDefaultStream is TS createTestModel's default stream.
func r87cstreamDefaultStream(warnings []types.Warning) []provider.StreamChunk {
	finish := r87cstreamFinish(types.FinishReasonStop, "stop")
	finish.ProviderMetadata = r87cstreamRawMD(`{"testProvider":{"testKey":"testValue"}}`)
	return []provider.StreamChunk{
		{Type: provider.ChunkTypeStreamStart, Warnings: warnings},
		r87cstreamMeta("id-0"),
		{Type: provider.ChunkTypeTextStart, ID: "1"},
		{Type: provider.ChunkTypeText, ID: "1", Text: "Hello"},
		{Type: provider.ChunkTypeText, ID: "1", Text: ", "},
		{Type: provider.ChunkTypeText, ID: "1", Text: "world!"},
		{Type: provider.ChunkTypeTextEnd, ID: "1"},
		finish,
	}
}

// r87cstreamWriteRecorder is a Node ServerResponse stand-in: it records the
// status, the headers and every write as one decoded chunk.
type r87cstreamWriteRecorder struct {
	header http.Header
	status int
	chunks []string
}

func (w *r87cstreamWriteRecorder) Header() http.Header {
	if w.header == nil {
		w.header = http.Header{}
	}
	return w.header
}

func (w *r87cstreamWriteRecorder) WriteHeader(status int) {
	if w.status == 0 {
		w.status = status
	}
}

func (w *r87cstreamWriteRecorder) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	w.chunks = append(w.chunks, string(b))
	return len(b), nil
}

func (w *r87cstreamWriteRecorder) Flush() {}

// lowerHeaders is the Node `response.headers` view (lower-case names).
func (w *r87cstreamWriteRecorder) lowerHeaders() map[string]string {
	return r87cstreamLowerHeaders(w.header)
}

func r87cstreamLowerHeaders(h http.Header) map[string]string {
	out := map[string]string{}
	for k, v := range h {
		out[strings.ToLower(k)] = strings.Join(v, ", ")
	}
	return out
}

const r87cstreamUIHeaders = `{"cache-control":"no-cache","connection":"keep-alive","content-type":"text/event-stream","x-accel-buffering":"no","x-vercel-ai-ui-message-stream":"v1"}`

// r87cstreamHelloSSE is the TS SSE snapshot of the default test model.
var r87cstreamHelloSSE = []string{
	`data: {"type":"start"}` + "\n\n",
	`data: {"type":"start-step"}` + "\n\n",
	`data: {"type":"text-start","id":"1"}` + "\n\n",
	`data: {"type":"text-delta","id":"1","delta":"Hello"}` + "\n\n",
	`data: {"type":"text-delta","id":"1","delta":", "}` + "\n\n",
	`data: {"type":"text-delta","id":"1","delta":"world!"}` + "\n\n",
	`data: {"type":"text-end","id":"1"}` + "\n\n",
	`data: {"type":"finish-step"}` + "\n\n",
	`data: {"type":"finish","finishReason":"stop"}` + "\n\n",
	"data: [DONE]\n\n",
}

// r87cstreamSSEEvents splits an SSE body into its events (each with its
// trailing blank line), the TS getDecodedChunks / convertResponseStreamToArray
// view of one write per event.
func r87cstreamSSEEvents(body string) []string {
	var out []string
	for _, e := range strings.SplitAfter(body, "\n\n") {
		if e != "" {
			out = append(out, e)
		}
	}
	return out
}

// r87cstreamAssertSSE compares SSE events, each JSON payload by value: Go
// writes UI chunk object keys sorted (known difference "UI message stream
// JSON key order").
func r87cstreamAssertSSE(t *testing.T, label string, got, want []string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s: %d events, want %d:\n got  %q\n want %q", label, len(got), len(want), got, want)
	}
	for i := range want {
		g, w := got[i], want[i]
		if strings.HasPrefix(w, "data: {") && strings.HasSuffix(g, "\n\n") && strings.HasPrefix(g, "data: ") {
			r87cstreamAssert(t, fmt.Sprintf("%s[%d]", label, i), json.RawMessage(strings.TrimSuffix(strings.TrimPrefix(g, "data: "), "\n\n")), strings.TrimSuffix(strings.TrimPrefix(w, "data: "), "\n\n"))
			continue
		}
		if g != w {
			t.Fatalf("%s[%d] = %q, want %q", label, i, g, w)
		}
	}
}

func r87cstreamAssertStrings(t *testing.T, label string, got, want []string) {
	t.Helper()
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("%s:\n got  %q\n want %q", label, got, want)
	}
}

func r87cstreamHelloResult(t *testing.T) *StreamTextResult {
	t.Helper()
	r, err := StreamText(context.Background(), StreamTextOptions{Model: r71streamModel(nil, r87cstreamDefaultStream(nil)), Prompt: "test-input", Internal: r87cstreamDefaultInternal()})
	if err != nil {
		t.Fatal(err)
	}
	return r
}

// stream-text.test.ts 5143: result.pipeUIMessageStreamToResponse > should
// write data stream parts to a Node.js response-like object.
func TestR87cstreamPipeUIMessageStreamToResponse(t *testing.T) {
	w := &r87cstreamWriteRecorder{}
	if err := r87cstreamHelloResult(t).PipeUIMessageStreamToResponse(context.Background(), w); err != nil {
		t.Fatal(err)
	}
	if w.status != 200 {
		t.Fatalf("status = %d", w.status)
	}
	r87cstreamAssert(t, "headers", w.lowerHeaders(), r87cstreamUIHeaders)
	r87cstreamAssertSSE(t, "chunks", r87cstreamSSEEvents(strings.Join(w.chunks, "")), r87cstreamHelloSSE)
}

// stream-text.test.ts 5205: result.pipeUIMessageStreamToResponse > should
// create a Response with a data stream and custom headers. net/http cannot
// send a custom reason phrase, so statusText ('foo') is not observable on a
// ResponseWriter (known difference); status and headers are.
func TestR87cstreamPipeUIMessageStreamToResponseCustomHeaders(t *testing.T) {
	w := &r87cstreamWriteRecorder{}
	if err := PipeUIMessageStreamToResponseWithInit(context.Background(), r87cstreamHelloResult(t), w, &UIMessageStreamResponseInit{Status: 201, StatusText: "foo", Headers: map[string]string{"custom-header": "custom-value"}}); err != nil {
		t.Fatal(err)
	}
	if w.status != 201 {
		t.Fatalf("status = %d", w.status)
	}
	r87cstreamAssert(t, "headers", w.lowerHeaders(), `{"cache-control":"no-cache","connection":"keep-alive","content-type":"text/event-stream","custom-header":"custom-value","x-accel-buffering":"no","x-vercel-ai-ui-message-stream":"v1"}`)
	r87cstreamAssertSSE(t, "chunks", r87cstreamSSEEvents(strings.Join(w.chunks, "")), r87cstreamHelloSSE)
}

// stream-text.test.ts 5614: result.pipeTextStreamToResponse > should write
// text deltas to a Node.js response-like object.
func TestR87cstreamPipeTextStreamToResponse(t *testing.T) {
	r, err := StreamText(context.Background(), StreamTextOptions{Model: r71streamModel(nil, []provider.StreamChunk{
		{Type: provider.ChunkTypeTextStart, ID: "1"},
		{Type: provider.ChunkTypeText, ID: "1", Text: "Hello"},
		{Type: provider.ChunkTypeText, ID: "1", Text: ", "},
		{Type: provider.ChunkTypeText, ID: "1", Text: "world!"},
		{Type: provider.ChunkTypeTextEnd, ID: "1"},
		r87cstreamFinish(types.FinishReasonStop, "stop"),
	}), Prompt: "test-input"})
	if err != nil {
		t.Fatal(err)
	}
	w := &r87cstreamWriteRecorder{}
	if err := r.PipeTextStreamToResponse(context.Background(), w); err != nil {
		t.Fatal(err)
	}
	if w.status != 200 {
		t.Fatalf("status = %d", w.status)
	}
	r87cstreamAssert(t, "headers", w.lowerHeaders(), `{"content-type":"text/plain; charset=utf-8"}`)
	r87cstreamAssertStrings(t, "chunks", w.chunks, []string{"Hello", ", ", "world!"})
}

// r87cstreamUI collects a StreamText result's UI message stream as JSON.
func r87cstreamUI(t *testing.T, r *StreamTextResult, opts ...UIMessageStreamResultOptions) []interface{} {
	t.Helper()
	out, errCh := r.ToUIMessageStream(context.Background(), opts...)
	got := []interface{}{}
	for c := range out {
		got = append(got, r75streamNormalize(t, c))
	}
	if err := <-errCh; err != nil {
		t.Fatal(err)
	}
	return got
}

// stream-text.test.ts 6029: result.toUIMessageStream > should send message
// metadata as defined in the metadata function (mockValues: one value per
// call, the last one repeating).
func TestR87cstreamUIMessageMetadataFunction(t *testing.T) {
	n := 0
	metadata := func(map[string]interface{}) map[string]interface{} {
		if n < 8 {
			n++
		}
		return map[string]interface{}{fmt.Sprintf("key%d", n): fmt.Sprintf("value%d", n)}
	}
	r, err := StreamText(context.Background(), StreamTextOptions{Model: r71streamModel(nil, r87cstreamDefaultStream(nil)), Prompt: "prompt", Internal: r87cstreamDefaultInternal(), OnError: func(context.Context, error) {}})
	if err != nil {
		t.Fatal(err)
	}
	r87cstreamAssert(t, "ui", r87cstreamUI(t, r, UIMessageStreamResultOptions{MessageMetadata: metadata}),
		`[{"messageMetadata":{"key1":"value1"},"type":"start"},{"type":"start-step"},{"messageMetadata":{"key2":"value2"},"type":"message-metadata"},{"id":"1","type":"text-start"},{"messageMetadata":{"key3":"value3"},"type":"message-metadata"},{"delta":"Hello","id":"1","type":"text-delta"},{"messageMetadata":{"key4":"value4"},"type":"message-metadata"},{"delta":", ","id":"1","type":"text-delta"},{"messageMetadata":{"key5":"value5"},"type":"message-metadata"},{"delta":"world!","id":"1","type":"text-delta"},{"messageMetadata":{"key6":"value6"},"type":"message-metadata"},{"id":"1","type":"text-end"},{"messageMetadata":{"key7":"value7"},"type":"message-metadata"},{"messageMetadata":{"key8":"value8"},"type":"message-metadata"},{"type":"finish-step"},{"finishReason":"stop","messageMetadata":{"key8":"value8"},"type":"finish"}]`)
}

// stream-text.test.ts 6329: result.toUIMessageStream > should send reasoning
// content when sendReasoning is true.
func TestR87cstreamUISendReasoning(t *testing.T) {
	r, err := StreamText(context.Background(), StreamTextOptions{Model: r71streamModel(nil, r74streamReasoningSteps()), Prompt: "prompt", Internal: r87cstreamDefaultInternal(), OnError: func(context.Context, error) {}})
	if err != nil {
		t.Fatal(err)
	}
	send := true
	r87cstreamAssert(t, "ui", r87cstreamUI(t, r, UIMessageStreamResultOptions{SendReasoning: &send}),
		`[{"type":"start"},{"type":"start-step"},{"id":"1","type":"reasoning-start"},{"delta":"I will open the conversation","id":"1","type":"reasoning-delta"},{"delta":" with witty banter.","id":"1","type":"reasoning-delta"},{"delta":"","id":"1","providerMetadata":{"testProvider":{"signature":"1234567890"}},"type":"reasoning-delta"},{"id":"1","type":"reasoning-end"},{"id":"2","providerMetadata":{"testProvider":{"redactedData":"redacted-reasoning-data"}},"type":"reasoning-start"},{"id":"2","type":"reasoning-end"},{"id":"3","type":"reasoning-start"},{"delta":" Once the user has relaxed,","id":"3","type":"reasoning-delta"},{"delta":" I will pry for valuable information.","id":"3","type":"reasoning-delta"},{"id":"3","providerMetadata":{"testProvider":{"signature":"1234567890"}},"type":"reasoning-end"},{"id":"4","providerMetadata":{"testProvider":{"signature":"1234567890"}},"type":"reasoning-start"},{"delta":" I need to think about","id":"4","type":"reasoning-delta"},{"delta":" this problem carefully.","id":"4","type":"reasoning-delta"},{"id":"5","providerMetadata":{"testProvider":{"signature":"1234567890"}},"type":"reasoning-start"},{"delta":" The best solution","id":"5","type":"reasoning-delta"},{"delta":" requires careful","id":"5","type":"reasoning-delta"},{"delta":" consideration of all factors.","id":"5","type":"reasoning-delta"},{"id":"4","providerMetadata":{"testProvider":{"signature":"0987654321"}},"type":"reasoning-end"},{"id":"5","providerMetadata":{"testProvider":{"signature":"0987654321"}},"type":"reasoning-end"},{"id":"1","type":"text-start"},{"delta":"Hi","id":"1","type":"text-delta"},{"delta":" there!","id":"1","type":"text-delta"},{"id":"1","providerMetadata":{"testProvider":{"signature":"0987654321"}},"type":"text-end"},{"type":"finish-step"},{"finishReason":"stop","type":"finish"}]`)
}

// stream-text.test.ts 6633: result.toUIMessageStream > should send custom
// parts in toUIMessageStream (modelWithCustom).
func TestR87cstreamUISendCustomParts(t *testing.T) {
	r, err := StreamText(context.Background(), StreamTextOptions{Model: r71streamModel(nil, []provider.StreamChunk{
		{Type: provider.ChunkTypeTextStart, ID: "1"},
		{Type: provider.ChunkTypeText, ID: "1", Text: "Hello!"},
		{Type: provider.ChunkTypeTextEnd, ID: "1"},
		{Type: provider.ChunkTypeCustom, CustomContent: &types.CustomContent{Kind: "openai.compaction", ProviderMetadata: r87cstreamRawMD(`{"openai":{"itemId":"cmp_123"}}`)}},
		r87cstreamFinish(types.FinishReasonStop, "stop"),
	}), Prompt: "prompt", Internal: r87cstreamDefaultInternal(), OnError: func(context.Context, error) {}})
	if err != nil {
		t.Fatal(err)
	}
	r87cstreamAssert(t, "ui", r87cstreamUI(t, r),
		`[{"type":"start"},{"type":"start-step"},{"id":"1","type":"text-start"},{"delta":"Hello!","id":"1","type":"text-delta"},{"id":"1","type":"text-end"},{"kind":"openai.compaction","providerMetadata":{"openai":{"itemId":"cmp_123"}},"type":"custom"},{"type":"finish-step"},{"finishReason":"stop","type":"finish"}]`)
}

// stream-text.test.ts 6935: result.toUIMessageStream > should call onFinish
// when async iteration stops mid-stream. Go's analog of breaking out of
// `for await` is cancelling the consumer context after five chunks.
func TestR87cstreamUIOnFinishWhenIterationStops(t *testing.T) {
	chunks := []provider.StreamChunk{
		{Type: provider.ChunkTypeResponseMetadata, ResponseMetadata: &provider.ResponseMetadata{ID: "msg-4", ModelID: "test-model", Timestamp: time.Now()}},
		{Type: provider.ChunkTypeTextStart, ID: "1"},
	}
	for _, d := range []string{"First", " chunk", " of", " text", " that", " will", " be", " interrupted"} {
		chunks = append(chunks, provider.StreamChunk{Type: provider.ChunkTypeText, ID: "1", Text: d})
	}
	in, out, zero := int64(10), int64(5), int64(0)
	chunks = append(chunks, provider.StreamChunk{Type: provider.ChunkTypeTextEnd, ID: "1"}, provider.StreamChunk{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, RawFinishReason: "stop",
		Usage: &types.Usage{InputTokens: &in, OutputTokens: &out, InputDetails: &types.InputTokenDetails{NoCacheTokens: &in, CacheReadTokens: &zero, CacheWriteTokens: &zero}, OutputDetails: &types.OutputTokenDetails{TextTokens: &out, ReasoningTokens: &zero}}})
	release := make(chan struct{})
	model := &testutil.MockLanguageModel{ProviderName: "mock-provider", ModelName: "mock-model-id", DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
		return &r87cstreamGateStream{chunks: chunks, gateAt: 5, release: release}, nil
	}}
	r, err := StreamText(context.Background(), StreamTextOptions{Model: model, Prompt: "Generate text"})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	var mu sync.Mutex
	var finishes []map[string]interface{}
	ui, errCh := r.ToUIMessageStream(ctx, UIMessageStreamResultOptions{
		OnFinish:          func(e map[string]interface{}) { mu.Lock(); finishes = append(finishes, e); mu.Unlock() },
		GenerateMessageID: func() string { return "msg-async-iter" },
	})
	var collected []UIMessageChunk
	for c := range ui {
		collected = append(collected, c)
		if len(collected) >= 5 {
			break
		}
	}
	cancel()
	close(release)
	for range ui {
	}
	<-errCh
	if len(collected) != 5 {
		t.Fatalf("collected = %d", len(collected))
	}
	mu.Lock()
	defer mu.Unlock()
	if len(finishes) != 1 {
		t.Fatalf("onFinish calls = %d", len(finishes))
	}
	e := r75streamNormalize(t, finishes[0]).(map[string]interface{})
	msg := e["responseMessage"].(map[string]interface{})
	if msg["id"] != "msg-async-iter" || msg["role"] != "assistant" {
		t.Fatalf("responseMessage = %v", msg)
	}
	var text map[string]interface{}
	for _, p := range msg["parts"].([]interface{}) {
		if pm := p.(map[string]interface{}); pm["type"] == "text" {
			text = pm
			break
		}
	}
	if text == nil || !strings.Contains(text["text"].(string), "First chunk") || text["state"] != "streaming" {
		t.Fatalf("text part = %v", text)
	}
	if e["isAborted"] != false {
		t.Fatalf("isAborted = %v", e["isAborted"])
	}
}

// r87cstreamGateStream streams chunks and blocks before chunk gateAt until
// release is closed, so a consumer can stop mid-stream deterministically.
type r87cstreamGateStream struct {
	chunks  []provider.StreamChunk
	gateAt  int
	release chan struct{}
	i       int
}

func (s *r87cstreamGateStream) Next() (*provider.StreamChunk, error) {
	if s.i == s.gateAt {
		<-s.release
	}
	if s.i >= len(s.chunks) {
		return nil, io.EOF
	}
	c := s.chunks[s.i]
	s.i++
	return &c, nil
}

func (s *r87cstreamGateStream) Err() error   { return nil }
func (s *r87cstreamGateStream) Close() error { return nil }

// stream-text.test.ts 7218: result.toUIMessageStreamResponse > should
// create a Response with a data stream.
func TestR87cstreamToUIMessageStreamResponse(t *testing.T) {
	resp, err := r87cstreamHelloResult(t).ToUIMessageStreamResponse(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 200 {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	r87cstreamAssert(t, "headers", r87cstreamLowerHeaders(resp.Header), r87cstreamUIHeaders)
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	r87cstreamAssertSSE(t, "body", r87cstreamSSEEvents(string(body)), r87cstreamHelloSSE)
}

// stream-text.test.ts 7279: result.toUIMessageStreamResponse > should
// create a Response with a data stream and custom headers.
func TestR87cstreamToUIMessageStreamResponseCustomHeaders(t *testing.T) {
	resp, err := r87cstreamHelloResult(t).ToUIMessageStreamResponse(context.Background(), &UIMessageStreamResponseInit{Status: 201, StatusText: "foo", Headers: map[string]string{"custom-header": "custom-value"}})
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != 201 || resp.Status != "foo" {
		t.Fatalf("status = %d %q", resp.StatusCode, resp.Status)
	}
	r87cstreamAssert(t, "headers", r87cstreamLowerHeaders(resp.Header), `{"cache-control":"no-cache","connection":"keep-alive","content-type":"text/event-stream","custom-header":"custom-value","x-accel-buffering":"no","x-vercel-ai-ui-message-stream":"v1"}`)
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		t.Fatal(err)
	}
	r87cstreamAssertSSE(t, "body", r87cstreamSSEEvents(string(body)), r87cstreamHelloSSE)
}
