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
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
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
		m["dynamic"] = tr.Dynamic
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
		m["finishReason"] = string(c.FinishReason)
		if c.RawFinishReason != "" {
			m["rawFinishReason"] = c.RawFinishReason
		}
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
		m["finishReason"] = string(c.FinishReason)
		if c.RawFinishReason != "" {
			m["rawFinishReason"] = c.RawFinishReason
		}
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

// r87cstreamClockStream advances a fake clock to at[i] (when non-zero)
// before it returns chunk i, so step timings are deterministic: the Go
// analog of TS _internal.now mockValues, tied to the chunks instead of the
// call order.
type r87cstreamClockStream struct {
	chunks []provider.StreamChunk
	at     []int64
	clock  *r87cstreamClock
	i      int
}

type r87cstreamClock struct {
	mu sync.Mutex
	ms int64
}

func (c *r87cstreamClock) set(ms int64) { c.mu.Lock(); c.ms = ms; c.mu.Unlock() }
func (c *r87cstreamClock) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return time.UnixMilli(c.ms)
}

func (s *r87cstreamClockStream) Next() (*provider.StreamChunk, error) {
	if s.i >= len(s.chunks) {
		return nil, io.EOF
	}
	if s.i < len(s.at) && s.at[s.i] != 0 {
		s.clock.set(s.at[s.i])
	}
	c := s.chunks[s.i]
	s.i++
	return &c, nil
}

func (s *r87cstreamClockStream) Err() error   { return nil }
func (s *r87cstreamClockStream) Close() error { return nil }

// stream-text.test.ts 9285: result.steps > should include step performance
// (now: 1000 at the step and call start, the three text deltas at 1200,
// 1300 and 1400, the finish and step end at 1500).
func TestR87cstreamStepPerformance(t *testing.T) {
	clock := &r87cstreamClock{ms: 1000}
	model := &testutil.MockLanguageModel{ProviderName: "mock-provider", ModelName: "mock-model-id", DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
		// stream-start, response-metadata, text-start, 3 deltas, text-end, finish
		return &r87cstreamClockStream{chunks: r87cstreamDefaultStream(nil), at: []int64{0, 0, 0, 1200, 1300, 1400, 0, 1500}, clock: clock}, nil
	}}
	internal := r87cstreamDefaultInternal()
	internal.now = clock.now
	r := r71streamRun(t, StreamTextOptions{Model: model, Prompt: "prompt", Internal: internal})
	r75streamAssertJSON(t, "performance", r.FinalStep().Performance, `{"effectiveOutputTokensPerSecond":20,"outputTokensPerSecond":33.333333333333336,"inputTokensPerSecond":15,"effectiveTotalTokensPerSecond":26,"stepTimeMs":500,"responseTimeMs":500,"toolExecutionMs":{},"timeToFirstOutputMs":200,"timeBetweenOutputChunksMs":{"min":100,"p10":100,"median":100,"avg":100,"p90":100,"max":100}}`)
}

// r87cstreamTSContent projects a Go content part onto the TS content part
// snapshot shape (files as TS DefaultGeneratedFileWithType).
func r87cstreamTSContent(t *testing.T, p types.ContentPart) interface{} {
	t.Helper()
	md := func(m map[string]interface{}, raw json.RawMessage) {
		if len(raw) > 0 && string(raw) != "null" {
			m["providerMetadata"] = raw
		}
	}
	switch c := p.(type) {
	case types.GeneratedFileContent:
		m := map[string]interface{}{"type": "file", "file": r87cstreamFileJSON(c.MediaType, c.FileData, c.Data)}
		md(m, c.ProviderMetadata)
		return m
	case types.ReasoningFileContent:
		m := map[string]interface{}{"type": "reasoning-file", "file": r87cstreamFileJSON(c.MediaType, c.FileData, c.Data)}
		md(m, c.ProviderMetadata)
		return m
	case types.ReasoningContent:
		m := map[string]interface{}{"type": "reasoning", "text": c.Text}
		md(m, c.ProviderMetadata)
		return m
	case types.TextContent:
		m := map[string]interface{}{"type": "text", "text": c.Text}
		md(m, c.ProviderMetadata)
		return m
	case types.ToolCallContent:
		m := map[string]interface{}{"type": "tool-call", "toolCallId": c.ToolCallID, "toolName": c.ToolName, "input": c.Arguments}
		if c.ProviderExecuted {
			m["providerExecuted"] = true
		}
		if c.Title != "" {
			m["title"] = c.Title
		}
		md(m, c.ProviderMetadata)
		return m
	case types.ToolResultContent:
		m := map[string]interface{}{"type": "tool-result", "toolCallId": c.ToolCallID, "toolName": c.ToolName, "input": c.Input, "output": c.Result, "dynamic": c.Dynamic}
		if c.ProviderExecuted {
			m["providerExecuted"] = true
		}
		if c.Preliminary {
			m["preliminary"] = true
		}
		if c.Title != "" {
			m["title"] = c.Title
		}
		md(m, c.ProviderMetadata)
		return m
	}
	return r75streamNormalize(t, p)
}

// r87cstreamTSMessages renders messages as JSON with reasoning-file parts in
// the TS ModelMessage shape ({type, data, mediaType, providerOptions}): Go
// marshals ReasoningFileContent with its tagged fileData and a null data
// (cr-r87c-stream-to-gen.md). Go's legacy tool_calls mirror is dropped.
func r87cstreamTSMessages(t *testing.T, msgs []types.Message) interface{} {
	t.Helper()
	v := r75streamNormalize(t, msgs)
	list, _ := v.([]interface{})
	for _, m := range list {
		mm, _ := m.(map[string]interface{})
		parts, _ := mm["content"].([]interface{})
		// Go's legacy Message.ToolCalls mirror (no TS twin) must match the
		// tool-call parts.
		if tcs, ok := mm["tool_calls"].([]interface{}); ok {
			var ids []interface{}
			for _, p := range parts {
				if pm, _ := p.(map[string]interface{}); pm["type"] == "tool-call" {
					ids = append(ids, pm["toolCallId"])
				}
			}
			for i, tc := range tcs {
				if i >= len(ids) || tc.(map[string]interface{})["id"] != ids[i] {
					t.Fatalf("message tool_calls %v do not mirror the tool-call parts", tcs)
				}
			}
			delete(mm, "tool_calls")
		}
		for _, p := range parts {
			pm, _ := p.(map[string]interface{})
			if pm["type"] != "reasoning-file" {
				continue
			}
			if fd, ok := pm["fileData"].(map[string]interface{}); ok {
				pm["data"] = fd["data"]
				delete(pm, "fileData")
			}
		}
	}
	return v
}

// r87cstreamTSStep projects a Go step result onto the TS DefaultStepResult
// snapshot (its own fields; Go's nil runtime / tools contexts are TS's {}).
func r87cstreamTSStep(t *testing.T, s types.StepResult) map[string]interface{} {
	t.Helper()
	content := []interface{}{}
	for _, p := range s.Content {
		content = append(content, r87cstreamTSContent(t, p))
	}
	req := map[string]interface{}{}
	if s.Request.Body != nil {
		req["body"] = s.Request.Body
	}
	if s.Request.Messages != nil {
		req["messages"] = s.Request.Messages
	}
	resp := map[string]interface{}{"id": s.Response.ID, "modelId": s.Response.ModelID, "timestamp": s.Response.Timestamp.UTC().Format("2006-01-02T15:04:05.000Z07:00"), "messages": r87cstreamTSMessages(t, s.Response.Messages)}
	if len(s.Response.Headers) > 0 {
		resp["headers"] = s.Response.Headers
	}
	if s.Response.Body != nil {
		resp["body"] = s.Response.Body
	}
	var runtimeCtx interface{} = map[string]interface{}{}
	if s.RuntimeContext != nil {
		runtimeCtx = s.RuntimeContext
	}
	toolsCtx := s.ToolsContext
	if toolsCtx == nil {
		toolsCtx = map[string]interface{}{}
	}
	warnings := s.Warnings
	if warnings == nil {
		warnings = []types.Warning{}
	}
	m := map[string]interface{}{
		"callId": s.CallID, "content": content, "finishReason": string(s.FinishReason),
		"model": map[string]interface{}{"modelId": s.Model.ModelID, "provider": s.Model.Provider},
		"performance": s.Performance, "request": req, "response": resp,
		"runtimeContext": runtimeCtx, "stepNumber": s.StepNumber, "toolsContext": toolsCtx,
		"usage": s.Usage, "warnings": warnings,
	}
	if s.RawFinishReason != "" {
		m["rawFinishReason"] = s.RawFinishReason
	}
	if len(s.ProviderMetadata) > 0 {
		m["providerMetadata"] = s.ProviderMetadata
	}
	return m
}

const r87cstreamUsageJSON = `{"inputTokenDetails":{"noCacheTokens":3},"inputTokens":3,"outputTokenDetails":{"textTokens":10},"outputTokens":10,"totalTokens":13}`

// stream-text.test.ts 7707: result.warnings > should resolve with warnings.
func TestR87cstreamResultWarnings(t *testing.T) {
	r := r71streamRun(t, StreamTextOptions{Model: r71streamModel(nil, r87cstreamDefaultStream([]types.Warning{{Type: "other", Message: "test-warning"}})), Prompt: "test-input"})
	r75streamAssertJSON(t, "warnings", r.Warnings(), `[{"type":"other","message":"test-warning"}]`)
}

// stream-text.test.ts 7858: result.usage > should resolve with token usage.
func TestR87cstreamResultUsage(t *testing.T) {
	r := r71streamRun(t, StreamTextOptions{Model: r71streamModel(nil, []provider.StreamChunk{
		{Type: provider.ChunkTypeTextStart, ID: "1"}, {Type: provider.ChunkTypeText, ID: "1", Text: "Hello"}, {Type: provider.ChunkTypeTextEnd, ID: "1"},
		r87cstreamFinish(types.FinishReasonStop, "stop"),
	}), Prompt: "test-input"})
	r75streamAssertJSON(t, "usage", r.Usage(), r87cstreamUsageJSON)
}

// stream-text.test.ts 7944: result.responseMessages > should contain
// reasoning (modelWithReasoning).
func TestR87cstreamResponseMessagesReasoning(t *testing.T) {
	r := r71streamRun(t, StreamTextOptions{Model: r71streamModel(nil, r74streamReasoningSteps()), Prompt: "prompt", Internal: r87cstreamDefaultInternal()})
	r75streamAssertJSON(t, "responseMessages", r.ResponseMessages(), `[{"content":[{"providerOptions":{"testProvider":{"signature":"1234567890"}},"text":"I will open the conversation with witty banter.","type":"reasoning"},{"providerOptions":{"testProvider":{"redactedData":"redacted-reasoning-data"}},"text":"","type":"reasoning"},{"providerOptions":{"testProvider":{"signature":"1234567890"}},"text":" Once the user has relaxed, I will pry for valuable information.","type":"reasoning"},{"providerOptions":{"testProvider":{"signature":"0987654321"}},"text":" I need to think about this problem carefully.","type":"reasoning"},{"providerOptions":{"testProvider":{"signature":"0987654321"}},"text":" The best solution requires careful consideration of all factors.","type":"reasoning"},{"providerOptions":{"testProvider":{"signature":"0987654321"}},"text":"Hi there!","type":"text"}],"role":"assistant"}]`)
}

// r87cstreamBodyModel streams the TS result.request fixture with request
// body "test body".
func r87cstreamBodyModel() *testutil.MockLanguageModel {
	return &testutil.MockLanguageModel{ProviderName: "mock-provider", ModelName: "mock-model-id", DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
		return &requestBodyTextStream{TextStream: testutil.NewMockTextStream([]provider.StreamChunk{
			r87cstreamMeta("id-0"),
			{Type: provider.ChunkTypeTextStart, ID: "1"}, {Type: provider.ChunkTypeText, ID: "1", Text: "Hello"}, {Type: provider.ChunkTypeTextEnd, ID: "1"},
			r87cstreamFinish(types.FinishReasonStop, "stop"),
		}), body: "test body"}, nil
	}}
}

// stream-text.test.ts 8078 / 8109: result.request > should accept deprecated
// experimental_include as an alias for include; should prefer include over
// deprecated experimental_include.
func TestR87cstreamRequestExperimentalInclude(t *testing.T) {
	r := r71streamRun(t, StreamTextOptions{Model: r87cstreamBodyModel(), Prompt: "test-input", ExperimentalInclude: &IncludeOptions{RequestBody: true}})
	r75streamAssertJSON(t, "8078 request", r.Request(), `{"body":"test body"}`)
	if r.Request().Messages != nil {
		t.Fatalf("8078 messages = %v", r.Request().Messages)
	}
	r = r71streamRun(t, StreamTextOptions{Model: r87cstreamBodyModel(), Prompt: "test-input", Include: &IncludeOptions{RequestBody: false}, ExperimentalInclude: &IncludeOptions{RequestBody: true}})
	if req := r.Request(); req.Body != nil || req.Messages != nil {
		t.Fatalf("8109 request = %+v", req)
	}
}

// stream-text.test.ts 8175: result.request > should resolve with messages
// from after prepareStep.
func TestR87cstreamRequestMessagesAfterPrepareStep(t *testing.T) {
	prepared := r71streamUserMessages("prepared prompt")
	r := r71streamRun(t, StreamTextOptions{Model: r87cstreamBodyModel(), Prompt: "test-input",
		PrepareStep: func(_ context.Context, o PrepareStepOptions) PrepareStepOptions {
			o.Messages = prepared
			return o
		},
		Include: &IncludeOptions{RequestMessages: true}})
	if !reflect.DeepEqual(r.Request().Messages, prepared) {
		t.Fatalf("request messages = %#v", r.Request().Messages)
	}
	if !reflect.DeepEqual(r.Steps()[0].Request.Messages, prepared) {
		t.Fatalf("steps[0] request messages = %#v", r.Steps()[0].Request.Messages)
	}
}

// stream-text.test.ts 8212: result.request > should carry messages from
// prepareStep forward to the next step.
func TestR87cstreamRequestMessagesCarriedForward(t *testing.T) {
	prepared := r71streamUserMessages("prepared prompt")
	var mu sync.Mutex
	var seen [][]types.Message
	r := r71streamRun(t, StreamTextOptions{
		Model: r71streamModel(nil,
			[]provider.StreamChunk{{Type: provider.ChunkTypeStreamStart}, {Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "tool1", RawArguments: `{ "value": "test" }`}}, r87cstreamFinish(types.FinishReasonToolCalls, "")},
			[]provider.StreamChunk{{Type: provider.ChunkTypeStreamStart}, {Type: provider.ChunkTypeTextStart, ID: "1"}, {Type: provider.ChunkTypeText, ID: "1", Text: "Done."}, {Type: provider.ChunkTypeTextEnd, ID: "1"}, r87cstreamFinish(types.FinishReasonStop, "stop")}),
		Tools:    []types.Tool{r71streamTool1(true, nil, nil)},
		Prompt:   "prompt",
		StopWhen: []StopCondition{StepCountIs(3)},
		PrepareStep: func(_ context.Context, o PrepareStepOptions) PrepareStepOptions {
			mu.Lock()
			seen = append(seen, append([]types.Message(nil), o.Messages...))
			mu.Unlock()
			if o.StepNumber == 0 {
				o.Messages = prepared
			}
			return o
		},
		Include: &IncludeOptions{RequestMessages: true},
	})
	steps := r.Steps()
	if len(steps) != 2 || len(seen) != 2 {
		t.Fatalf("steps = %d, prepareStep calls = %d", len(steps), len(seen))
	}
	want := append(append([]types.Message(nil), prepared...), steps[0].Response.Messages...)
	r75streamAssertJSON(t, "prepareStep messages[1]", seen[1], r71streamJSON(t, want))
	r75streamAssertJSON(t, "steps[1].request.messages", steps[1].Request.Messages, r71streamJSON(t, want))
}

// stream-text.test.ts 8292: result.response > should resolve with response
// information.
func TestR87cstreamResultResponse(t *testing.T) {
	model := &testutil.MockLanguageModel{ProviderName: "mock-provider", ModelName: "mock-model-id", DoStreamFunc: func(context.Context, *provider.GenerateOptions) (provider.TextStream, error) {
		meta := r87cstreamMeta("id-0")
		// TS createTestModel({response: {headers: {call: '2'}}}): the doStream
		// result's response headers.
		meta.ResponseMetadata.Headers = map[string]string{"call": "2"}
		return testutil.NewMockTextStream([]provider.StreamChunk{
			meta,
			{Type: provider.ChunkTypeTextStart, ID: "1"}, {Type: provider.ChunkTypeText, ID: "1", Text: "Hello"}, {Type: provider.ChunkTypeTextEnd, ID: "1"},
			r87cstreamFinish(types.FinishReasonStop, "stop"),
		}), nil
	}}
	r := r71streamRun(t, StreamTextOptions{Model: model, Prompt: "prompt", Internal: r87cstreamDefaultInternal()})
	resp := r.Response()
	r75streamAssertJSON(t, "response", map[string]interface{}{"headers": resp.Headers, "id": resp.ID, "messages": resp.Messages, "modelId": resp.ModelID, "timestamp": resp.Timestamp.UTC().Format("2006-01-02T15:04:05.000Z07:00")},
		`{"headers":{"call":"2"},"id":"id-0","messages":[{"content":[{"text":"Hello","type":"text"}],"role":"assistant"}],"modelId":"mock-model-id","timestamp":"1970-01-01T00:00:00.000Z"}`)
	if resp.Body != nil {
		t.Fatalf("response body = %v", resp.Body)
	}
}

// stream-text.test.ts 8645: result.files > should contain file content parts
// from all steps in result.content.
func TestR87cstreamContentFilesFromAllSteps(t *testing.T) {
	r := r71streamRun(t, StreamTextOptions{
		Model: r71streamModel(nil,
			[]provider.StreamChunk{r87cstreamDataFile("text/plain", "c3RlcC0w", nil), {Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "tool1", RawArguments: `{}`}}, r87cstreamFinish(types.FinishReasonToolCalls, "")},
			[]provider.StreamChunk{r87cstreamDataFile("text/plain", "c3RlcC0x", nil), r87cstreamFinish(types.FinishReasonStop, "stop")}),
		Tools: []types.Tool{{Name: "tool1", Parameters: map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}, Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return "result1", nil
		}}},
		Prompt: "prompt", Internal: r87cstreamDefaultInternal(), StopWhen: []StopCondition{StepCountIs(3)},
	})
	var files []map[string]interface{}
	for _, p := range r.Content() {
		if f, ok := p.(types.GeneratedFileContent); ok {
			files = append(files, map[string]interface{}{"base64": r87cstreamFileJSON(f.MediaType, f.FileData, f.Data)["base64Data"], "mediaType": f.MediaType})
		}
	}
	r75streamAssertJSON(t, "content files", files, `[{"base64":"c3RlcC0w","mediaType":"text/plain"},{"base64":"c3RlcC0x","mediaType":"text/plain"}]`)
	n := 0
	for _, p := range r.FinalStep().Content {
		if _, ok := p.(types.GeneratedFileContent); ok {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("final step files = %d, want 1", n)
	}
}

// stream-text.test.ts 9135: result.steps > should add reasoning-files from
// the model response to the step result (modelWithReasoningFiles).
func TestR87cstreamStepReasoningFiles(t *testing.T) {
	r := r71streamRun(t, StreamTextOptions{Model: r87cstreamModelWithReasoningFiles(), Prompt: "prompt", Internal: r87cstreamDefaultInternal()})
	var steps []interface{}
	for _, s := range r.Steps() {
		steps = append(steps, r87cstreamTSStep(t, s))
	}
	r87cstreamAssert(t, "steps", steps, `[{"callId":"test-telemetry-call-id","content":[{"file":{"base64Data":"reasoning-file-data-1","mediaType":"image/png","type":"file"},"type":"reasoning-file"},{"text":"Some reasoning text.","type":"reasoning"},{"file":{"base64Data":"reasoning-file-data-2","mediaType":"image/jpeg","type":"file"},"providerMetadata":{"testProvider":{"signature":"rf-sig-1"}},"type":"reasoning-file"},{"text":"Hello!","type":"text"}],"finishReason":"stop","model":{"modelId":"mock-model-id","provider":"mock-provider"},"performance":`+r87cstreamPerfKeys+`,"rawFinishReason":"stop","request":{},"response":{"id":"id-0","messages":[{"content":[{"data":"reasoning-file-data-1","mediaType":"image/png","type":"reasoning-file"},{"text":"Some reasoning text.","type":"reasoning"},{"data":"reasoning-file-data-2","mediaType":"image/jpeg","providerOptions":{"testProvider":{"signature":"rf-sig-1"}},"type":"reasoning-file"},{"text":"Hello!","type":"text"}],"role":"assistant"}],"modelId":"mock-model-id","timestamp":"1970-01-01T00:00:00.000Z"},"runtimeContext":{},"stepNumber":0,"toolsContext":{},"usage":`+r87cstreamUsageJSON+`,"warnings":[]}]`)
}

// stream-text.test.ts 9274: result.steps > should expose the final step
// (finalStep is steps.at(-1)).
func TestR87cstreamFinalStep(t *testing.T) {
	r := r71streamRun(t, StreamTextOptions{Model: r71streamModel(nil, r87cstreamDefaultStream(nil)), Prompt: "prompt", Internal: r87cstreamDefaultInternal()})
	steps := r.Steps()
	if len(steps) != 1 || !reflect.DeepEqual(r.FinalStep(), steps[len(steps)-1]) {
		t.Fatalf("finalStep = %+v, steps = %d", r.FinalStep(), len(steps))
	}
}

// r87cstreamToolCallJSON projects tool calls onto the TS TypedToolCall
// snapshot.
func r87cstreamToolCallJSON(calls []types.ToolCall) []interface{} {
	out := []interface{}{}
	for _, c := range calls {
		m := map[string]interface{}{"type": "tool-call", "toolCallId": c.ID, "toolName": c.ToolName, "input": c.Arguments}
		if c.ProviderExecuted {
			m["providerExecuted"] = true
		}
		if c.Dynamic {
			m["dynamic"] = true
		}
		if c.Title != "" {
			m["title"] = c.Title
		}
		if len(c.ProviderMetadata) > 0 {
			m["providerMetadata"] = c.ProviderMetadata
		}
		out = append(out, m)
	}
	return out
}

// stream-text.test.ts 9377: result.toolCalls > should resolve with tool
// calls.
func TestR87cstreamResultToolCalls(t *testing.T) {
	r := r71streamRun(t, StreamTextOptions{Model: r71streamModel(nil, []provider.StreamChunk{
		{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "tool1", RawArguments: `{ "value": "value" }`}},
		r87cstreamFinish(types.FinishReasonStop, "stop"),
	}), Tools: []types.Tool{r71streamTool1(false, nil, nil)}, Prompt: "test-input"})
	r75streamAssertJSON(t, "toolCalls", r87cstreamToolCallJSON(r.ToolCalls()), `[{"input":{"value":"value"},"toolCallId":"call-1","toolName":"tool1","type":"tool-call"}]`)
}

// r87cstreamAllStepsModel is the TS "from all steps" fixture: tool1, then
// dynamicTool, then text.
func r87cstreamAllStepsRun(t *testing.T) *StreamTextResult {
	t.Helper()
	return r71streamRun(t, StreamTextOptions{
		Model: r71streamModel(nil,
			[]provider.StreamChunk{{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "tool1", RawArguments: `{ "value": "value-1" }`}}, r87cstreamFinish(types.FinishReasonToolCalls, "")},
			[]provider.StreamChunk{{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call-2", ToolName: "dynamicTool", RawArguments: `{ "value": "value-2" }`}}, r87cstreamFinish(types.FinishReasonToolCalls, "")},
			[]provider.StreamChunk{{Type: provider.ChunkTypeTextStart, ID: "1"}, {Type: provider.ChunkTypeText, ID: "1", Text: "done"}, {Type: provider.ChunkTypeTextEnd, ID: "1"}, r87cstreamFinish(types.FinishReasonStop, "stop")}),
		Tools: []types.Tool{r71streamTool1(true, nil, nil), {Name: "dynamicTool", Type: types.ToolTypeDynamic, Parameters: r71streamValueSchema, Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
			return "value-2-result", nil
		}}},
		Prompt:   "test-input",
		StopWhen: []StopCondition{StepCountIs(4)},
	})
}

func r87cstreamCallIDs(calls []types.ToolCall) []string {
	out := []string{}
	for _, c := range calls {
		out = append(out, c.ID)
	}
	return out
}

func r87cstreamResultIDs(results []types.ToolResult) []string {
	out := []string{}
	for _, c := range results {
		out = append(out, c.ToolCallID)
	}
	return out
}

// stream-text.test.ts 9419: result.toolCalls > should resolve with tool
// calls from all steps.
func TestR87cstreamResultToolCallsAllSteps(t *testing.T) {
	r := r87cstreamAllStepsRun(t)
	r87cstreamAssertStrings(t, "toolCalls", r87cstreamCallIDs(r.ToolCalls()), []string{"call-1", "call-2"})
	r87cstreamAssertStrings(t, "staticToolCalls", r87cstreamCallIDs(r.StaticToolCalls()), []string{"call-1"})
	r87cstreamAssertStrings(t, "dynamicToolCalls", r87cstreamCallIDs(r.DynamicToolCalls()), []string{"call-2"})
	if n := len(r.FinalStep().ToolCalls); n != 0 {
		t.Fatalf("finalStep toolCalls = %d", n)
	}
}

// stream-text.test.ts 9627: result.toolResults > should resolve with tool
// results from all steps.
func TestR87cstreamResultToolResultsAllSteps(t *testing.T) {
	r := r87cstreamAllStepsRun(t)
	r87cstreamAssertStrings(t, "toolResults", r87cstreamResultIDs(r.ToolResults()), []string{"call-1", "call-2"})
	r87cstreamAssertStrings(t, "staticToolResults", r87cstreamResultIDs(r.StaticToolResults()), []string{"call-1"})
	r87cstreamAssertStrings(t, "dynamicToolResults", r87cstreamResultIDs(r.DynamicToolResults()), []string{"call-2"})
	if n := len(r.FinalStep().ToolResults); n != 0 {
		t.Fatalf("finalStep toolResults = %d", n)
	}
}

// stream-text.test.ts 9585: result.toolResults > should resolve with tool
// results.
func TestR87cstreamResultToolResults(t *testing.T) {
	r := r71streamRun(t, StreamTextOptions{Model: r71streamModel(nil, []provider.StreamChunk{
		{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "tool1", RawArguments: `{ "value": "value" }`}},
		r87cstreamFinish(types.FinishReasonStop, "stop"),
	}), Tools: []types.Tool{r71streamTool1(true, nil, nil)}, Prompt: "test-input"})
	var got []interface{}
	for _, tr := range r.ToolResults() {
		got = append(got, map[string]interface{}{"type": "tool-result", "dynamic": tr.Dynamic, "input": tr.Input, "output": tr.Result, "toolCallId": tr.ToolCallID, "toolName": tr.ToolName, "providerExecuted": tr.ProviderExecuted, "preliminary": tr.Preliminary})
	}
	r75streamAssertJSON(t, "toolResults", got, `[{"dynamic":false,"input":{"value":"value"},"output":"value-result","toolCallId":"call-1","toolName":"tool1","type":"tool-result","providerExecuted":false,"preliminary":false}]`)
}

// stream-text.test.ts 9505: toolCallConcurrency > should execute tool calls
// sequentially in model order with concurrency 1.
func TestR87cstreamToolCallConcurrencyOne(t *testing.T) {
	firstStarted, secondStarted, releaseFirst := make(chan struct{}), make(chan struct{}), make(chan struct{})
	var order r71streamOrder
	one := 1
	empty := map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}
	r, err := StreamText(context.Background(), StreamTextOptions{
		Model: r71streamModel(nil, []provider.StreamChunk{
			{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "first", RawArguments: `{}`}},
			{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call-2", ToolName: "second", RawArguments: `{}`}},
			r87cstreamFinish(types.FinishReasonToolCalls, "tool-calls"),
		}),
		Tools: []types.Tool{
			{Name: "first", Parameters: empty, Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
				order.add("first:start")
				close(firstStarted)
				<-releaseFirst
				order.add("first:end")
				return nil, nil
			}},
			{Name: "second", Parameters: empty, Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) {
				order.add("second:start")
				close(secondStarted)
				order.add("second:end")
				return nil, nil
			}},
		},
		Prompt:              "test-input",
		ToolCallConcurrency: &one,
	})
	if err != nil {
		t.Fatal(err)
	}
	done := make(chan []types.ToolResult)
	go func() { done <- r.ToolResults() }()
	<-firstStarted
	time.Sleep(20 * time.Millisecond)
	r87cstreamAssertStrings(t, "events after first start", order.get(), []string{"first:start"})
	close(releaseFirst)
	<-secondStarted
	<-done
	r87cstreamAssertStrings(t, "events", order.get(), []string{"first:start", "first:end", "second:start", "second:end"})
}

// stream-text.test.ts 9824 / 9931: options.onStart > should include
// configured runtimeContext properties in telemetry; should exclude
// runtimeContext from telemetry by default (onStart, onStepStart, onStepEnd,
// onEnd of the telemetry integration, in that order).
func TestR87cstreamTelemetryRuntimeContext(t *testing.T) {
	runtimeCtx := map[string]interface{}{"userId": "user-123", "requestId": "request-123"}
	for _, tc := range []struct {
		line     int
		settings TelemetrySettings
		want     string
	}{
		{9824, TelemetrySettings{IncludeRuntimeContext: map[string]bool{"requestId": true}}, `[{"requestId":"request-123"},{"requestId":"request-123"},{"requestId":"request-123"},{"requestId":"request-123"}]`},
		{9931, TelemetrySettings{}, `[{},{},{},{}]`},
	} {
		rec := &r73streamCtxRecorder{}
		settings := tc.settings
		settings.Integrations = []telemetry.TelemetryIntegration{rec}
		r71streamRun(t, StreamTextOptions{Model: r71streamModel(nil, r87cstreamDefaultStream(nil)), Prompt: "test-input", RuntimeContext: runtimeCtx, Telemetry: &settings})
		rec.mu.Lock()
		got := append([]interface{}(nil), rec.runtime...)
		rec.mu.Unlock()
		r75streamAssertJSON(t, fmt.Sprintf("%d telemetry runtime contexts", tc.line), got, tc.want)
	}
}

// stream-text.test.ts 10576: options.onStepStart > should validate model
// call settings returned from prepareStep.
func TestR87cstreamPrepareStepSettingsValidated(t *testing.T) {
	var errs r82streamErrors
	zero := 0
	r, err := StreamText(context.Background(), StreamTextOptions{Model: r71streamModel(nil, r87cstreamDefaultStream(nil)), Prompt: "test-input",
		PrepareStep: func(context.Context, PrepareStepOptions) PrepareStepOptions { return PrepareStepOptions{MaxOutputTokens: &zero} },
		OnError:     errs.onError})
	if err != nil {
		t.Fatal(err)
	}
	_ = r.ConsumeStream()
	got := errs.all()
	if len(got) != 1 || got[0].Error() != "Invalid argument for parameter maxOutputTokens: maxOutputTokens must be >= 1" {
		t.Fatalf("onError = %v", got)
	}
}

// stream-text.test.ts 10645: options.onStepStart > should pass
// initialInstructions, initialMessages, and responseMessages to prepareStep.
func TestR87cstreamPrepareStepInitialAndResponseMessages(t *testing.T) {
	type call struct {
		instructions, initialInstructions          *string
		initialMessages, responseMessages, messages []types.Message
	}
	var mu sync.Mutex
	var calls []call
	var prompts []types.Prompt
	model := r71streamModel(nil,
		[]provider.StreamChunk{r87cstreamMeta("id-0"), {Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "tool1", RawArguments: `{ "value": "test" }`}}, r87cstreamFinish(types.FinishReasonToolCalls, "")},
		[]provider.StreamChunk{{Type: provider.ChunkTypeResponseMetadata, ResponseMetadata: &provider.ResponseMetadata{ID: "id-1", ModelID: "mock-model-id", Timestamp: time.UnixMilli(1000)}}, {Type: provider.ChunkTypeTextStart, ID: "1"}, {Type: provider.ChunkTypeText, ID: "1", Text: "Done."}, {Type: provider.ChunkTypeTextEnd, ID: "1"}, r87cstreamFinish(types.FinishReasonStop, "stop")})
	inner := model.DoStreamFunc
	model.DoStreamFunc = func(ctx context.Context, o *provider.GenerateOptions) (provider.TextStream, error) {
		mu.Lock()
		prompts = append(prompts, o.Prompt)
		mu.Unlock()
		return inner(ctx, o)
	}
	instructions := "test instructions"
	prepared := "prepared instructions"
	r71streamRun(t, StreamTextOptions{Model: model, Tools: []types.Tool{r71streamTool1(true, nil, nil)}, Instructions: &instructions,
		Messages: r71streamUserMessages("test-input"), StopWhen: []StopCondition{StepCountIs(3)},
		PrepareStep: func(_ context.Context, o PrepareStepOptions) PrepareStepOptions {
			mu.Lock()
			calls = append(calls, call{o.Instructions, o.InitialInstructions, append([]types.Message(nil), o.InitialMessages...), append([]types.Message(nil), o.ResponseMessages...), append([]types.Message(nil), o.Messages...)})
			mu.Unlock()
			if o.StepNumber == 0 {
				o.Instructions = &prepared
			}
			return o
		}})
	if len(calls) != 2 {
		t.Fatalf("prepareStep calls = %d", len(calls))
	}
	str := func(p *string) string {
		if p == nil {
			return "<nil>"
		}
		return *p
	}
	user := r71streamJSON(t, r71streamUserMessages("test-input"))
	if str(calls[0].instructions) != "test instructions" || str(calls[0].initialInstructions) != "test instructions" {
		t.Fatalf("call 0 instructions = %q / %q", str(calls[0].instructions), str(calls[0].initialInstructions))
	}
	r75streamAssertJSON(t, "call 0 initialMessages", calls[0].initialMessages, user)
	if len(calls[0].responseMessages) != 0 {
		t.Fatalf("call 0 responseMessages = %v", calls[0].responseMessages)
	}
	r75streamAssertJSON(t, "call 0 messages", calls[0].messages, user)
	if str(calls[1].instructions) != "prepared instructions" || str(calls[1].initialInstructions) != "test instructions" {
		t.Fatalf("call 1 instructions = %q / %q", str(calls[1].instructions), str(calls[1].initialInstructions))
	}
	r75streamAssertJSON(t, "call 1 initialMessages", calls[1].initialMessages, user)
	r87cstreamAssert(t, "call 1 responseMessages", r87cstreamTSMessages(t, calls[1].responseMessages), `[{"content":[{"input":{"value":"test"},"toolCallId":"call-1","toolName":"tool1","type":"tool-call"}],"role":"assistant"},{"content":[{"output":{"type":"text","value":"test-result"},"toolCallId":"call-1","toolName":"tool1","type":"tool-result"}],"role":"tool"}]`)
	want := append(append([]types.Message(nil), calls[1].initialMessages...), calls[1].responseMessages...)
	if !reflect.DeepEqual(calls[1].messages, want) {
		t.Fatalf("call 1 messages = %#v", calls[1].messages)
	}
	if len(prompts) != 2 || prompts[1].System != "prepared instructions" {
		t.Fatalf("doStream[1] system = %q", prompts[1].System)
	}
}

// stream-text.test.ts 10911: options.onLanguageModelCallStart and
// onLanguageModelCallEnd > should expose provider metadata on model-call end.
func TestR87cstreamModelCallEndProviderMetadata(t *testing.T) {
	finish := r87cstreamFinish(types.FinishReasonStop, "stop")
	finish.ProviderMetadata = r87cstreamRawMD(`{"gateway":{"generationId":"generation-id"}}`)
	var mu sync.Mutex
	var ends []LanguageModelCallEndEvent
	r71streamRun(t, StreamTextOptions{Model: r71streamModel(nil, []provider.StreamChunk{finish}), Prompt: "test-input",
		OnLanguageModelCallEnd: func(_ context.Context, e LanguageModelCallEndEvent) { mu.Lock(); ends = append(ends, e); mu.Unlock() }})
	if len(ends) != 1 {
		t.Fatalf("onLanguageModelCallEnd calls = %d", len(ends))
	}
	r75streamAssertJSON(t, "providerMetadata", ends[0].ProviderMetadata, `{"gateway":{"generationId":"generation-id"}}`)
}

// stream-text.test.ts 12439: options.onChunk > should include custom parts in
// onChunk events (modelWithCustom; TS onChunk gets every part including
// start, start-step, finish-step and finish).
func TestR87cstreamOnChunkCustomParts(t *testing.T) {
	var mu sync.Mutex
	var chunks []provider.StreamChunk
	r71streamRun(t, StreamTextOptions{Model: r71streamModel(nil, []provider.StreamChunk{
		{Type: provider.ChunkTypeTextStart, ID: "1"},
		{Type: provider.ChunkTypeText, ID: "1", Text: "Hello!"},
		{Type: provider.ChunkTypeTextEnd, ID: "1"},
		{Type: provider.ChunkTypeCustom, CustomContent: &types.CustomContent{Kind: "openai.compaction", ProviderMetadata: r87cstreamRawMD(`{"openai":{"itemId":"cmp_123"}}`)}},
		r87cstreamFinish(types.FinishReasonStop, "stop"),
	}), Prompt: "test-input", Internal: r87cstreamDefaultInternal(),
		OnChunk: func(c provider.StreamChunk) { mu.Lock(); chunks = append(chunks, c); mu.Unlock() }})
	mu.Lock()
	defer mu.Unlock()
	r87cstreamEpochFallback(t, chunks)
	r87cstreamAssert(t, "onChunk", r87cstreamTSParts(chunks), `[{"type":"start"},{"request":{},"type":"start-step","warnings":[]},{"id":"1","type":"text-start"},{"id":"1","text":"Hello!","type":"text-delta"},{"id":"1","type":"text-end"},{"kind":"openai.compaction","providerMetadata":{"openai":{"itemId":"cmp_123"}},"type":"custom"},{"finishReason":"stop","performance":{"effectiveOutputTokensPerSecond":0,"effectiveTotalTokensPerSecond":0,"inputTokensPerSecond":0,"outputTokensPerSecond":0,"responseTimeMs":0,"stepTimeMs":0,"timeToFirstOutputMs":0,"toolExecutionMs":{}},"rawFinishReason":"stop","response":{"id":"id-0","modelId":"mock-model-id","timestamp":"1970-01-01T00:00:00.000Z"},"type":"finish-step","usage":`+r87cstreamUsageJSON+`},{"finishReason":"stop","rawFinishReason":"stop","totalUsage":`+r87cstreamUsageJSON+`,"type":"finish"}]`)
}

// stream-text.test.ts 12591: options.onError > should invoke onError.
func TestR87cstreamOnErrorInvoked(t *testing.T) {
	var errs r82streamErrors
	r, err := StreamText(context.Background(), StreamTextOptions{Model: r87cstreamThrowingModel(), Prompt: "test-input", OnError: errs.onError})
	if err != nil {
		t.Fatal(err)
	}
	_ = r.ConsumeStream()
	got := errs.all()
	if len(got) != 1 || got[0].Error() != "test error" {
		t.Fatalf("onError = %v", got)
	}
}

// stream-text.test.ts 13947: options.onEnd > should not prevent error from
// being forwarded (onFinish defined; the stream is start + error).
func TestR87cstreamOnFinishDoesNotSwallowError(t *testing.T) {
	_, parts := r73streamParts(t, StreamTextOptions{Model: r87cstreamThrowingModel(), Prompt: "test-input", OnFinish: func(*StreamTextResult) {}})
	r87cstreamAssert(t, "parts", r87cstreamTSParts(parts), `[{"type":"start"},{"type":"error","error":"test error"}]`)
}

// stream-text.test.ts 13972 / 14006: result.responseMessages > should
// contain assistant response message when there are no tool calls; and
// tool message when there are tool calls with results.
func TestR87cstreamResponseMessagesTextAndTools(t *testing.T) {
	text := []provider.StreamChunk{{Type: provider.ChunkTypeTextStart, ID: "1"}, {Type: provider.ChunkTypeText, ID: "1", Text: "Hello, "}, {Type: provider.ChunkTypeText, ID: "1", Text: "world!"}, {Type: provider.ChunkTypeTextEnd, ID: "1"}}
	r := r71streamRun(t, StreamTextOptions{Model: r71streamModel(nil, append(append([]provider.StreamChunk(nil), text...), r87cstreamFinish(types.FinishReasonStop, "stop"))), Prompt: "test-input"})
	r87cstreamAssert(t, "13972", r87cstreamTSMessages(t, r.ResponseMessages()), `[{"content":[{"text":"Hello, world!","type":"text"}],"role":"assistant"}]`)

	withTool := append(append([]provider.StreamChunk(nil), text...),
		provider.StreamChunk{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "tool1", RawArguments: `{ "value": "value" }`}},
		r87cstreamFinish(types.FinishReasonStop, "stop"))
	tool := r71streamTool1(false, nil, nil)
	tool.Execute = func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) { return "result1", nil }
	r = r71streamRun(t, StreamTextOptions{Model: r71streamModel(nil, withTool), Tools: []types.Tool{tool}, Prompt: "test-input"})
	r87cstreamAssert(t, "14006", r87cstreamTSMessages(t, r.ResponseMessages()), `[{"content":[{"text":"Hello, world!","type":"text"},{"input":{"value":"value"},"toolCallId":"call-1","toolName":"tool1","type":"tool-call"}],"role":"assistant"},{"content":[{"output":{"type":"text","value":"result1"},"toolCallId":"call-1","toolName":"tool1","type":"tool-result"}],"role":"tool"}]`)
}

// r87cstreamTestUsage2 is TS testUsage2 (cache 0, reasoning 10).
func r87cstreamTestUsage2() *types.Usage {
	in, out, total, zero := int64(3), int64(10), int64(13), int64(0)
	return &types.Usage{InputTokens: &in, OutputTokens: &out, TotalTokens: &total,
		InputDetails:  &types.InputTokenDetails{NoCacheTokens: &in, CacheReadTokens: &zero, CacheWriteTokens: &zero},
		OutputDetails: &types.OutputTokenDetails{TextTokens: &out, ReasoningTokens: &out}}
}

func r87cstreamMetaAt(id string, ms int64, headers map[string]string) provider.StreamChunk {
	return provider.StreamChunk{Type: provider.ChunkTypeResponseMetadata, ResponseMetadata: &provider.ResponseMetadata{ID: id, ModelID: "mock-model-id", Timestamp: time.UnixMilli(ms).UTC(), Headers: headers}}
}

// r87cstreamTwoStepChunks are the TS options.stopWhen "2 steps: initial,
// tool-result" model streams (doStream response headers as the
// response-metadata headers).
func r87cstreamTwoStepChunks() [][]provider.StreamChunk {
	finish2 := provider.StreamChunk{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, RawFinishReason: "stop", Usage: r87cstreamTestUsage2()}
	return [][]provider.StreamChunk{
		{
			r87cstreamMetaAt("id-0", 0, map[string]string{"call": "1"}),
			{Type: provider.ChunkTypeReasoningStart, ID: "0"},
			{Type: provider.ChunkTypeReasoning, ID: "0", Reasoning: "thinking"},
			{Type: provider.ChunkTypeReasoningEnd, ID: "0"},
			{Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "tool1", RawArguments: `{ "value": "value" }`}},
			r87cstreamFinish(types.FinishReasonToolCalls, ""),
		},
		{
			r87cstreamMetaAt("id-1", 1000, map[string]string{"call": "2"}),
			{Type: provider.ChunkTypeTextStart, ID: "1"},
			{Type: provider.ChunkTypeText, ID: "1", Text: "Hello, "},
			{Type: provider.ChunkTypeText, ID: "1", Text: "world!"},
			{Type: provider.ChunkTypeTextEnd, ID: "1"},
			finish2,
		},
	}
}

type r87cstreamTwoStepRun struct {
	result      *StreamTextResult
	parts       []provider.StreamChunk
	end         OnFinishEvent
	stepResults []types.StepResult
}

func r87cstreamTwoSteps(t *testing.T, opts StreamTextOptions) *r87cstreamTwoStepRun {
	t.Helper()
	run := &r87cstreamTwoStepRun{}
	var mu sync.Mutex
	if opts.Model == nil {
		opts.Model = r71streamModel(nil, r87cstreamTwoStepChunks()...)
	}
	if len(opts.Tools) == 0 {
		tool := r71streamTool1(false, nil, nil)
		tool.Execute = func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) { return "result1", nil }
		opts.Tools = []types.Tool{tool}
	}
	opts.Prompt = "test-input"
	opts.OnEndEvent = func(_ context.Context, e OnFinishEvent) { mu.Lock(); run.end = e; mu.Unlock() }
	opts.OnStepFinish = func(_ context.Context, s types.StepResult, _ interface{}) {
		mu.Lock()
		run.stepResults = append(run.stepResults, s)
		mu.Unlock()
	}
	opts.StopWhen = []StopCondition{StepCountIs(3)}
	opts.Internal = r87cstreamDefaultInternal()
	run.result, run.parts = r73streamParts(t, opts)
	return run
}

func r87cstreamTSToolCall(c types.ToolCall) map[string]interface{} {
	return r87cstreamToolCallJSON([]types.ToolCall{c})[0].(map[string]interface{})
}

func r87cstreamTSToolResult(tr types.ToolResult) map[string]interface{} {
	m := map[string]interface{}{"type": "tool-result", "toolCallId": tr.ToolCallID, "toolName": tr.ToolName, "input": tr.Input, "output": tr.Result, "dynamic": tr.Dynamic}
	if tr.ProviderExecuted {
		m["providerExecuted"] = true
	}
	if tr.Preliminary {
		m["preliminary"] = true
	}
	if len(tr.ProviderMetadata) > 0 {
		m["providerMetadata"] = tr.ProviderMetadata
	}
	if tr.Title != "" {
		m["title"] = tr.Title
	}
	return m
}

func r87cstreamTSToolResults(results []types.ToolResult) []interface{} {
	out := []interface{}{}
	for _, tr := range results {
		out = append(out, r87cstreamTSToolResult(tr))
	}
	return out
}

func r87cstreamTSContents(t *testing.T, parts []types.ContentPart) []interface{} {
	t.Helper()
	out := []interface{}{}
	for _, p := range parts {
		out = append(out, r87cstreamTSContent(t, p))
	}
	return out
}

func r87cstreamTSSteps(t *testing.T, steps []types.StepResult) []interface{} {
	t.Helper()
	out := []interface{}{}
	for _, s := range steps {
		out = append(out, r87cstreamTSStep(t, s))
	}
	return out
}

// r87cstreamTSEnd projects an OnFinishEvent onto the TS onEnd event
// snapshot keys.
func r87cstreamTSEnd(t *testing.T, e OnFinishEvent) map[string]interface{} {
	t.Helper()
	orEmpty := func(v interface{}) interface{} {
		if reflect.ValueOf(v).IsNil() {
			return []interface{}{}
		}
		return v
	}
	req := map[string]interface{}{}
	if e.Request.Body != nil {
		req["body"] = e.Request.Body
	}
	if e.Request.Messages != nil {
		req["messages"] = e.Request.Messages
	}
	resp := r75streamNormalize(t, e.Response).(map[string]interface{})
	resp["timestamp"] = e.Response.Timestamp.UTC().Format("2006-01-02T15:04:05.000Z07:00")
	resp["messages"] = r87cstreamTSMessages(t, e.Response.Messages)
	var runtimeCtx interface{} = map[string]interface{}{}
	if e.RuntimeContext != nil {
		runtimeCtx = e.RuntimeContext
	}
	toolsCtx := e.ToolsContext
	if toolsCtx == nil {
		toolsCtx = map[string]interface{}{}
	}
	reasoning := []interface{}{}
	for _, rc := range e.Reasoning {
		reasoning = append(reasoning, r87cstreamTSContent(t, rc))
	}
	m := map[string]interface{}{
		"callId": e.CallID, "content": r87cstreamTSContents(t, e.Content),
		"dynamicToolCalls": r87cstreamToolCallJSON(e.DynamicToolCalls), "dynamicToolResults": r87cstreamTSToolResults(e.DynamicToolResults),
		"files": orEmpty(e.Files), "finalStep": r87cstreamTSStep(t, e.FinalStep), "finishReason": string(e.FinishReason),
		"model": map[string]interface{}{"modelId": e.Model.ModelID, "provider": e.Model.Provider},
		"reasoning": reasoning, "request": req, "response": resp, "responseMessages": r87cstreamTSMessages(t, e.ResponseMessages),
		"runtimeContext": runtimeCtx, "sources": orEmpty(e.Sources),
		"staticToolCalls": r87cstreamToolCallJSON(e.StaticToolCalls), "staticToolResults": r87cstreamTSToolResults(e.StaticToolResults),
		"stepNumber": e.StepNumber, "steps": r87cstreamTSSteps(t, e.Steps), "text": e.Text,
		"toolCalls": r87cstreamToolCallJSON(e.ToolCalls), "toolResults": r87cstreamTSToolResults(e.ToolResults),
		"toolsContext": toolsCtx, "totalUsage": e.TotalUsage, "usage": e.Usage, "warnings": orEmpty(e.Warnings),
	}
	if e.RawFinishReason != "" {
		m["rawFinishReason"] = e.RawFinishReason
	}
	if len(e.ProviderMetadata) > 0 {
		m["providerMetadata"] = e.ProviderMetadata
	}
	return m
}

// stream-text.test.ts 14347: options.stopWhen > 2 steps: initial,
// tool-result > should contain assistant response message and tool message
// from all steps (the full stream).
func TestR87cstreamTwoStepsFullStream(t *testing.T) {
	run := r87cstreamTwoSteps(t, StreamTextOptions{})
	r87cstreamAssert(t, "parts", r87cstreamTSParts(run.parts), `[{"type":"start"},{"request":{},"type":"start-step","warnings":[]},{"id":"0","type":"reasoning-start"},{"id":"0","text":"thinking","type":"reasoning-delta"},{"id":"0","type":"reasoning-end"},{"input":{"value":"value"},"toolCallId":"call-1","toolName":"tool1","type":"tool-call"},{"dynamic":false,"input":{"value":"value"},"output":"result1","toolCallId":"call-1","toolName":"tool1","type":"tool-result"},{"finishReason":"tool-calls","performance":{"effectiveOutputTokensPerSecond":16.666666666666668,"effectiveTotalTokensPerSecond":21.666666666666668,"inputTokensPerSecond":30,"outputTokensPerSecond":20,"responseTimeMs":600,"stepTimeMs":1000,"timeBetweenOutputChunksMs":{"avg":400,"max":400,"median":400,"min":400,"p10":400,"p90":400},"timeToFirstOutputMs":100,"toolExecutionMs":{"call-1":0}},"response":{"headers":{"call":"1"},"id":"id-0","modelId":"mock-model-id","timestamp":"1970-01-01T00:00:00.000Z"},"type":"finish-step","usage":{"inputTokenDetails":{"noCacheTokens":3},"inputTokens":3,"outputTokenDetails":{"textTokens":10},"outputTokens":10,"totalTokens":13}},{"request":{},"type":"start-step","warnings":[]},{"id":"1","type":"text-start"},{"id":"1","text":"Hello, ","type":"text-delta"},{"id":"1","text":"world!","type":"text-delta"},{"id":"1","type":"text-end"},{"finishReason":"stop","performance":{"effectiveOutputTokensPerSecond":0,"effectiveTotalTokensPerSecond":0,"inputTokensPerSecond":0,"outputTokensPerSecond":0,"responseTimeMs":0,"stepTimeMs":0,"timeBetweenOutputChunksMs":{"avg":0,"max":0,"median":0,"min":0,"p10":0,"p90":0},"timeToFirstOutputMs":0,"toolExecutionMs":{}},"rawFinishReason":"stop","response":{"headers":{"call":"2"},"id":"id-1","modelId":"mock-model-id","timestamp":"1970-01-01T00:00:01.000Z"},"type":"finish-step","usage":{"inputTokenDetails":{"cacheReadTokens":0,"cacheWriteTokens":0,"noCacheTokens":3},"inputTokens":3,"outputTokenDetails":{"reasoningTokens":10,"textTokens":10},"outputTokens":10,"totalTokens":13}},{"finishReason":"stop","rawFinishReason":"stop","totalUsage":{"inputTokenDetails":{"cacheReadTokens":0,"cacheWriteTokens":0,"noCacheTokens":6},"inputTokens":6,"outputTokenDetails":{"reasoningTokens":10,"textTokens":20},"outputTokens":20,"totalTokens":26},"type":"finish"}]`)
}

// stream-text.test.ts 14549: options.stopWhen > 2 steps: initial,
// tool-result > callbacks > onFinish should send correct information.
func TestR87cstreamTwoStepsOnFinish(t *testing.T) {
	run := r87cstreamTwoSteps(t, StreamTextOptions{})
	r87cstreamAssert(t, "onFinish", r87cstreamTSEnd(t, run.end), `{"callId":"test-telemetry-call-id","content":[{"text":"thinking","type":"reasoning"},{"input":{"value":"value"},"toolCallId":"call-1","toolName":"tool1","type":"tool-call"},{"dynamic":false,"input":{"value":"value"},"output":"result1","toolCallId":"call-1","toolName":"tool1","type":"tool-result"},{"text":"Hello, world!","type":"text"}],"dynamicToolCalls":[],"dynamicToolResults":[],"files":[],"finalStep":{"callId":"test-telemetry-call-id","content":[{"text":"Hello, world!","type":"text"}],"finishReason":"stop","model":{"modelId":"mock-model-id","provider":"mock-provider"},"performance":{"effectiveOutputTokensPerSecond":0,"effectiveTotalTokensPerSecond":0,"inputTokensPerSecond":0,"outputTokensPerSecond":0,"responseTimeMs":0,"stepTimeMs":0,"timeBetweenOutputChunksMs":{"avg":0,"max":0,"median":0,"min":0,"p10":0,"p90":0},"timeToFirstOutputMs":0,"toolExecutionMs":{}},"rawFinishReason":"stop","request":{},"response":{"headers":{"call":"2"},"id":"id-1","messages":[{"content":[{"text":"Hello, world!","type":"text"}],"role":"assistant"}],"modelId":"mock-model-id","timestamp":"1970-01-01T00:00:01.000Z"},"runtimeContext":{},"stepNumber":1,"toolsContext":{},"usage":{"inputTokenDetails":{"cacheReadTokens":0,"cacheWriteTokens":0,"noCacheTokens":3},"inputTokens":3,"outputTokenDetails":{"reasoningTokens":10,"textTokens":10},"outputTokens":10,"totalTokens":13},"warnings":[]},"finishReason":"stop","model":{"modelId":"mock-model-id","provider":"mock-provider"},"rawFinishReason":"stop","reasoning":[],"request":{},"response":{"headers":{"call":"2"},"id":"id-1","messages":[{"content":[{"text":"Hello, world!","type":"text"}],"role":"assistant"}],"modelId":"mock-model-id","timestamp":"1970-01-01T00:00:01.000Z"},"responseMessages":[{"content":[{"text":"thinking","type":"reasoning"},{"input":{"value":"value"},"toolCallId":"call-1","toolName":"tool1","type":"tool-call"}],"role":"assistant"},{"content":[{"output":{"type":"text","value":"result1"},"toolCallId":"call-1","toolName":"tool1","type":"tool-result"}],"role":"tool"},{"content":[{"text":"Hello, world!","type":"text"}],"role":"assistant"}],"runtimeContext":{},"sources":[],"staticToolCalls":[{"input":{"value":"value"},"toolCallId":"call-1","toolName":"tool1","type":"tool-call"}],"staticToolResults":[{"dynamic":false,"input":{"value":"value"},"output":"result1","toolCallId":"call-1","toolName":"tool1","type":"tool-result"}],"stepNumber":1,"steps":[{"callId":"test-telemetry-call-id","content":[{"text":"thinking","type":"reasoning"},{"input":{"value":"value"},"toolCallId":"call-1","toolName":"tool1","type":"tool-call"},{"dynamic":false,"input":{"value":"value"},"output":"result1","toolCallId":"call-1","toolName":"tool1","type":"tool-result"}],"finishReason":"tool-calls","model":{"modelId":"mock-model-id","provider":"mock-provider"},"performance":{"effectiveOutputTokensPerSecond":16.666666666666668,"effectiveTotalTokensPerSecond":21.666666666666668,"inputTokensPerSecond":30,"outputTokensPerSecond":20,"responseTimeMs":600,"stepTimeMs":1000,"timeBetweenOutputChunksMs":{"avg":400,"max":400,"median":400,"min":400,"p10":400,"p90":400},"timeToFirstOutputMs":100,"toolExecutionMs":{"call-1":0}},"request":{},"response":{"headers":{"call":"1"},"id":"id-0","messages":[{"content":[{"text":"thinking","type":"reasoning"},{"input":{"value":"value"},"toolCallId":"call-1","toolName":"tool1","type":"tool-call"}],"role":"assistant"},{"content":[{"output":{"type":"text","value":"result1"},"toolCallId":"call-1","toolName":"tool1","type":"tool-result"}],"role":"tool"}],"modelId":"mock-model-id","timestamp":"1970-01-01T00:00:00.000Z"},"runtimeContext":{},"stepNumber":0,"toolsContext":{},"usage":{"inputTokenDetails":{"noCacheTokens":3},"inputTokens":3,"outputTokenDetails":{"textTokens":10},"outputTokens":10,"totalTokens":13},"warnings":[]},{"callId":"test-telemetry-call-id","content":[{"text":"Hello, world!","type":"text"}],"finishReason":"stop","model":{"modelId":"mock-model-id","provider":"mock-provider"},"performance":{"effectiveOutputTokensPerSecond":0,"effectiveTotalTokensPerSecond":0,"inputTokensPerSecond":0,"outputTokensPerSecond":0,"responseTimeMs":0,"stepTimeMs":0,"timeBetweenOutputChunksMs":{"avg":0,"max":0,"median":0,"min":0,"p10":0,"p90":0},"timeToFirstOutputMs":0,"toolExecutionMs":{}},"rawFinishReason":"stop","request":{},"response":{"headers":{"call":"2"},"id":"id-1","messages":[{"content":[{"text":"Hello, world!","type":"text"}],"role":"assistant"}],"modelId":"mock-model-id","timestamp":"1970-01-01T00:00:01.000Z"},"runtimeContext":{},"stepNumber":1,"toolsContext":{},"usage":{"inputTokenDetails":{"cacheReadTokens":0,"cacheWriteTokens":0,"noCacheTokens":3},"inputTokens":3,"outputTokenDetails":{"reasoningTokens":10,"textTokens":10},"outputTokens":10,"totalTokens":13},"warnings":[]}],"text":"Hello, world!","toolCalls":[{"input":{"value":"value"},"toolCallId":"call-1","toolName":"tool1","type":"tool-call"}],"toolResults":[{"dynamic":false,"input":{"value":"value"},"output":"result1","toolCallId":"call-1","toolName":"tool1","type":"tool-result"}],"toolsContext":{},"totalUsage":{"inputTokenDetails":{"cacheReadTokens":0,"cacheWriteTokens":0,"noCacheTokens":6},"inputTokens":6,"outputTokenDetails":{"reasoningTokens":10,"textTokens":20},"outputTokens":20,"totalTokens":26},"usage":{"inputTokenDetails":{"cacheReadTokens":0,"cacheWriteTokens":0,"noCacheTokens":6},"inputTokens":6,"outputTokenDetails":{"reasoningTokens":10,"textTokens":20},"outputTokens":20,"totalTokens":26},"warnings":[]}`)
}

// stream-text.test.ts 15039: options.stopWhen > 2 steps: initial,
// tool-result > callbacks > onStepFinish should send correct information.
func TestR87cstreamTwoStepsOnStepFinish(t *testing.T) {
	run := r87cstreamTwoSteps(t, StreamTextOptions{})
	r87cstreamAssert(t, "onStepFinish", r87cstreamTSSteps(t, run.stepResults), `[{"callId":"test-telemetry-call-id","content":[{"text":"thinking","type":"reasoning"},{"input":{"value":"value"},"toolCallId":"call-1","toolName":"tool1","type":"tool-call"},{"dynamic":false,"input":{"value":"value"},"output":"result1","toolCallId":"call-1","toolName":"tool1","type":"tool-result"}],"finishReason":"tool-calls","model":{"modelId":"mock-model-id","provider":"mock-provider"},"performance":{"effectiveOutputTokensPerSecond":16.666666666666668,"effectiveTotalTokensPerSecond":21.666666666666668,"inputTokensPerSecond":30,"outputTokensPerSecond":20,"responseTimeMs":600,"stepTimeMs":1000,"timeBetweenOutputChunksMs":{"avg":400,"max":400,"median":400,"min":400,"p10":400,"p90":400},"timeToFirstOutputMs":100,"toolExecutionMs":{"call-1":0}},"request":{},"response":{"headers":{"call":"1"},"id":"id-0","messages":[{"content":[{"text":"thinking","type":"reasoning"},{"input":{"value":"value"},"toolCallId":"call-1","toolName":"tool1","type":"tool-call"}],"role":"assistant"},{"content":[{"output":{"type":"text","value":"result1"},"toolCallId":"call-1","toolName":"tool1","type":"tool-result"}],"role":"tool"}],"modelId":"mock-model-id","timestamp":"1970-01-01T00:00:00.000Z"},"runtimeContext":{},"stepNumber":0,"toolsContext":{},"usage":{"inputTokenDetails":{"noCacheTokens":3},"inputTokens":3,"outputTokenDetails":{"textTokens":10},"outputTokens":10,"totalTokens":13},"warnings":[]},{"callId":"test-telemetry-call-id","content":[{"text":"Hello, world!","type":"text"}],"finishReason":"stop","model":{"modelId":"mock-model-id","provider":"mock-provider"},"performance":{"effectiveOutputTokensPerSecond":0,"effectiveTotalTokensPerSecond":0,"inputTokensPerSecond":0,"outputTokensPerSecond":0,"responseTimeMs":0,"stepTimeMs":0,"timeBetweenOutputChunksMs":{"avg":0,"max":0,"median":0,"min":0,"p10":0,"p90":0},"timeToFirstOutputMs":0,"toolExecutionMs":{}},"rawFinishReason":"stop","request":{},"response":{"headers":{"call":"2"},"id":"id-1","messages":[{"content":[{"text":"Hello, world!","type":"text"}],"role":"assistant"}],"modelId":"mock-model-id","timestamp":"1970-01-01T00:00:01.000Z"},"runtimeContext":{},"stepNumber":1,"toolsContext":{},"usage":{"inputTokenDetails":{"cacheReadTokens":0,"cacheWriteTokens":0,"noCacheTokens":3},"inputTokens":3,"outputTokenDetails":{"reasoningTokens":10,"textTokens":10},"outputTokens":10,"totalTokens":13},"warnings":[]}]`)
}

// stream-text.test.ts 15605: options.stopWhen > 2 steps: initial,
// tool-result > should have correct ui message stream.
func TestR87cstreamTwoStepsUIMessageStream(t *testing.T) {
	r, err := StreamText(context.Background(), StreamTextOptions{Model: r71streamModel(nil, r87cstreamTwoStepChunks()...),
		Tools: []types.Tool{func() types.Tool {
			tool := r71streamTool1(false, nil, nil)
			tool.Execute = func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) { return "result1", nil }
			return tool
		}()},
		Prompt: "test-input", StopWhen: []StopCondition{StepCountIs(3)}, Internal: r87cstreamDefaultInternal(), OnError: func(context.Context, error) {}})
	if err != nil {
		t.Fatal(err)
	}
	r87cstreamAssert(t, "ui", r87cstreamUI(t, r), `[{"type":"start"},{"type":"start-step"},{"id":"0","type":"reasoning-start"},{"delta":"thinking","id":"0","type":"reasoning-delta"},{"id":"0","type":"reasoning-end"},{"input":{"value":"value"},"toolCallId":"call-1","toolName":"tool1","type":"tool-input-available"},{"output":"result1","toolCallId":"call-1","type":"tool-output-available"},{"type":"finish-step"},{"type":"start-step"},{"id":"1","type":"text-start"},{"delta":"Hello, ","id":"1","type":"text-delta"},{"delta":"world!","id":"1","type":"text-delta"},{"id":"1","type":"text-end"},{"type":"finish-step"},{"finishReason":"stop","type":"finish"}]`)
}

// r87cstreamZodValueSchema is the JSON Schema zod gives TS
// z.object({ value: z.string() }).
var r87cstreamZodValueSchema = map[string]interface{}{"$schema": "http://json-schema.org/draft-07/schema#", "additionalProperties": false, "type": "object",
	"properties": map[string]interface{}{"value": map[string]interface{}{"type": "string"}}, "required": []interface{}{"value"}}

// r87cstreamTSCallOptions projects a doStream call onto the TS
// LanguageModelV4CallOptions snapshot (prompt with the system message
// first; undefined settings dropped).
func r87cstreamTSCallOptions(t *testing.T, o *provider.GenerateOptions) map[string]interface{} {
	t.Helper()
	prompt := []interface{}{}
	if o.Prompt.System != "" {
		prompt = append(prompt, map[string]interface{}{"role": "system", "content": o.Prompt.System})
	}
	if msgs, ok := r87cstreamTSMessages(t, o.Prompt.Messages).([]interface{}); ok {
		prompt = append(prompt, msgs...)
	}
	m := map[string]interface{}{"includeRawChunks": o.IncludeRawChunks, "prompt": prompt}
	if o.ToolChoice.Type != "" {
		tc := map[string]interface{}{"type": string(o.ToolChoice.Type)}
		if o.ToolChoice.ToolName != "" {
			tc["toolName"] = o.ToolChoice.ToolName
		}
		m["toolChoice"] = tc
	}
	if len(o.Tools) > 0 {
		var tools []interface{}
		for _, tool := range o.Tools {
			tm := map[string]interface{}{"type": "function", "name": tool.Name, "inputSchema": tool.Parameters}
			if tool.Description != "" {
				tm["description"] = tool.Description
			}
			tools = append(tools, tm)
		}
		m["tools"] = tools
	}
	for k, v := range map[string]interface{}{"maxOutputTokens": o.MaxTokens, "temperature": o.Temperature, "topP": o.TopP, "topK": o.TopK, "seed": o.Seed, "presencePenalty": o.PresencePenalty, "frequencyPenalty": o.FrequencyPenalty} {
		if !reflect.ValueOf(v).IsNil() {
			m[k] = v
		}
	}
	if o.Headers != nil {
		m["headers"] = o.Headers
	}
	if o.ProviderOptions != nil {
		m["providerOptions"] = o.ProviderOptions
	}
	return m
}

type r87cstreamPrepareCall struct {
	modelID        string
	stepNumber     int
	steps          []types.StepResult
	messages       []types.Message
	runtimeContext interface{}
}

// r87cstreamPrepareStepRun is the TS options.stopWhen "2 steps: initial,
// tool-result with prepareStep" setup.
func r87cstreamPrepareStepRun(t *testing.T) ([]*provider.GenerateOptions, []r87cstreamPrepareCall, *StreamTextResult) {
	t.Helper()
	var mu sync.Mutex
	var calls []*provider.GenerateOptions
	var prepares []r87cstreamPrepareCall
	steps := [][]provider.StreamChunk{
		{r87cstreamMetaAt("id-0", 0, map[string]string{"call": "1"}), {Type: provider.ChunkTypeToolCall, ToolCall: &types.ToolCall{ID: "call-1", ToolName: "tool1", RawArguments: `{ "value": "value" }`}}, r87cstreamFinish(types.FinishReasonToolCalls, "")},
		{r87cstreamMetaAt("id-1", 1000, map[string]string{"call": "2"}), {Type: provider.ChunkTypeTextStart, ID: "2"}, {Type: provider.ChunkTypeText, ID: "2", Text: "Hello, "}, {Type: provider.ChunkTypeText, ID: "2", Text: "world!"}, {Type: provider.ChunkTypeTextEnd, ID: "2"},
			{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop, RawFinishReason: "stop", Usage: r87cstreamTestUsage2()}},
	}
	model := &testutil.MockLanguageModel{ProviderName: "mock-provider", ModelName: "mock-model-id", DoStreamFunc: func(_ context.Context, o *provider.GenerateOptions) (provider.TextStream, error) {
		mu.Lock()
		defer mu.Unlock()
		calls = append(calls, o)
		if len(calls) > 2 {
			return nil, fmt.Errorf("Unexpected response count: %d", len(calls))
		}
		return testutil.NewMockTextStream(steps[len(calls)-1]), nil
	}}
	tool := types.Tool{Name: "tool1", Parameters: r87cstreamZodValueSchema, Execute: func(context.Context, map[string]interface{}, types.ToolExecutionOptions) (interface{}, error) { return "result1", nil }}
	r := r71streamRun(t, StreamTextOptions{Model: model, Tools: []types.Tool{tool}, RuntimeContext: map[string]interface{}{"context": "state1"}, Prompt: "test-input",
		StopWhen: []StopCondition{StepCountIs(3)},
		Internal: &InternalOptions{GenerateID: func() string { return "test-call-id" }, GenerateCallID: func() string { return "test-telemetry-call-id" }},
		PrepareStep: func(_ context.Context, o PrepareStepOptions) PrepareStepOptions {
			mu.Lock()
			prepares = append(prepares, r87cstreamPrepareCall{modelID: o.Model.ModelID(), stepNumber: o.StepNumber, steps: append([]types.StepResult(nil), o.Steps...), messages: append([]types.Message(nil), o.Messages...), runtimeContext: o.RuntimeContext})
			mu.Unlock()
			switch o.StepNumber {
			case 0:
				s0 := "system-message-0"
				return PrepareStepOptions{ToolChoice: types.ToolChoice{Type: types.ToolChoiceTool, ToolName: "tool1"}, Instructions: &s0, Messages: r71streamUserMessages("new input from prepareStep"), RuntimeContext: map[string]interface{}{"context": "state2"}}
			case 1:
				s1 := "system-message-1"
				return PrepareStepOptions{ActiveTools: []string{}, Instructions: &s1, RuntimeContext: map[string]interface{}{"context": "state3"}}
			}
			return PrepareStepOptions{}
		}})
	return calls, prepares, r
}

// stream-text.test.ts 15803: options.stopWhen > 2 steps: initial,
// tool-result with prepareStep > should contain all doStream calls.
func TestR87cstreamPrepareStepDoStreamCalls(t *testing.T) {
	calls, _, _ := r87cstreamPrepareStepRun(t)
	var got []interface{}
	for _, c := range calls {
		got = append(got, r87cstreamTSCallOptions(t, c))
	}
	r87cstreamAssert(t, "doStream calls", got, `[{"includeRawChunks":false,"prompt":[{"content":"system-message-0","role":"system"},{"content":[{"text":"new input from prepareStep","type":"text"}],"role":"user"}],"toolChoice":{"toolName":"tool1","type":"tool"},"tools":[{"inputSchema":{"$schema":"http://json-schema.org/draft-07/schema#","additionalProperties":false,"properties":{"value":{"type":"string"}},"required":["value"],"type":"object"},"name":"tool1","type":"function"}]},{"includeRawChunks":false,"prompt":[{"content":"system-message-1","role":"system"},{"content":[{"text":"new input from prepareStep","type":"text"}],"role":"user"},{"content":[{"input":{"value":"value"},"toolCallId":"call-1","toolName":"tool1","type":"tool-call"}],"role":"assistant"},{"content":[{"output":{"type":"text","value":"result1"},"toolCallId":"call-1","toolName":"tool1","type":"tool-result"}],"role":"tool"}],"toolChoice":{"type":"auto"}}]`)
}

// stream-text.test.ts 15934: options.stopWhen > 2 steps: initial,
// tool-result with prepareStep > should contain all prepareStep calls. TS
// passes the live steps array, so its snapshot (taken after the run) shows
// both steps in both calls; Go passes a snapshot, so the steps each call saw
// are checked against the final steps at call time ([] and [step 0]), and
// the final steps against the TS step snapshot.
func TestR87cstreamPrepareStepCalls(t *testing.T) {
	_, prepares, r := r87cstreamPrepareStepRun(t)
	var want []map[string]interface{}
	if err := json.Unmarshal([]byte(`[{"messages":[{"content":"test-input","role":"user"}],"modelId":"mock-model-id","runtimeContext":{"context":"state1"},"stepNumber":0,"steps":[{"callId":"test-telemetry-call-id","content":[{"input":{"value":"value"},"toolCallId":"call-1","toolName":"tool1","type":"tool-call"},{"dynamic":false,"input":{"value":"value"},"output":"result1","toolCallId":"call-1","toolName":"tool1","type":"tool-result"}],"finishReason":"tool-calls","model":{"modelId":"mock-model-id","provider":"mock-provider"},"performance":{"effectiveOutputTokensPerSecond":0,"effectiveTotalTokensPerSecond":0,"inputTokensPerSecond":0,"outputTokensPerSecond":0,"responseTimeMs":0,"stepTimeMs":0,"timeToFirstOutputMs":0,"toolExecutionMs":{"call-1":0}},"request":{},"response":{"headers":{"call":"1"},"id":"id-0","messages":[{"content":[{"input":{"value":"value"},"toolCallId":"call-1","toolName":"tool1","type":"tool-call"}],"role":"assistant"},{"content":[{"output":{"type":"text","value":"result1"},"toolCallId":"call-1","toolName":"tool1","type":"tool-result"}],"role":"tool"}],"modelId":"mock-model-id","timestamp":"1970-01-01T00:00:00.000Z"},"runtimeContext":{"context":"state2"},"stepNumber":0,"toolsContext":{},"usage":{"inputTokenDetails":{"noCacheTokens":3},"inputTokens":3,"outputTokenDetails":{"textTokens":10},"outputTokens":10,"totalTokens":13},"warnings":[]},{"callId":"test-telemetry-call-id","content":[{"text":"Hello, world!","type":"text"}],"finishReason":"stop","model":{"modelId":"mock-model-id","provider":"mock-provider"},"performance":{"effectiveOutputTokensPerSecond":0,"effectiveTotalTokensPerSecond":0,"inputTokensPerSecond":0,"outputTokensPerSecond":0,"responseTimeMs":0,"stepTimeMs":0,"timeBetweenOutputChunksMs":{"avg":0,"max":0,"median":0,"min":0,"p10":0,"p90":0},"timeToFirstOutputMs":0,"toolExecutionMs":{}},"rawFinishReason":"stop","request":{},"response":{"headers":{"call":"2"},"id":"id-1","messages":[{"content":[{"text":"Hello, world!","type":"text"}],"role":"assistant"}],"modelId":"mock-model-id","timestamp":"1970-01-01T00:00:01.000Z"},"runtimeContext":{"context":"state3"},"stepNumber":1,"toolsContext":{},"usage":{"inputTokenDetails":{"cacheReadTokens":0,"cacheWriteTokens":0,"noCacheTokens":3},"inputTokens":3,"outputTokenDetails":{"reasoningTokens":10,"textTokens":10},"outputTokens":10,"totalTokens":13},"warnings":[]}]},{"messages":[{"content":"new input from prepareStep","role":"user"},{"content":[{"input":{"value":"value"},"toolCallId":"call-1","toolName":"tool1","type":"tool-call"}],"role":"assistant"},{"content":[{"output":{"type":"text","value":"result1"},"toolCallId":"call-1","toolName":"tool1","type":"tool-result"}],"role":"tool"}],"modelId":"mock-model-id","runtimeContext":{"context":"state2"},"stepNumber":1,"steps":[{"callId":"test-telemetry-call-id","content":[{"input":{"value":"value"},"toolCallId":"call-1","toolName":"tool1","type":"tool-call"},{"dynamic":false,"input":{"value":"value"},"output":"result1","toolCallId":"call-1","toolName":"tool1","type":"tool-result"}],"finishReason":"tool-calls","model":{"modelId":"mock-model-id","provider":"mock-provider"},"performance":{"effectiveOutputTokensPerSecond":0,"effectiveTotalTokensPerSecond":0,"inputTokensPerSecond":0,"outputTokensPerSecond":0,"responseTimeMs":0,"stepTimeMs":0,"timeToFirstOutputMs":0,"toolExecutionMs":{"call-1":0}},"request":{},"response":{"headers":{"call":"1"},"id":"id-0","messages":[{"content":[{"input":{"value":"value"},"toolCallId":"call-1","toolName":"tool1","type":"tool-call"}],"role":"assistant"},{"content":[{"output":{"type":"text","value":"result1"},"toolCallId":"call-1","toolName":"tool1","type":"tool-result"}],"role":"tool"}],"modelId":"mock-model-id","timestamp":"1970-01-01T00:00:00.000Z"},"runtimeContext":{"context":"state2"},"stepNumber":0,"toolsContext":{},"usage":{"inputTokenDetails":{"noCacheTokens":3},"inputTokens":3,"outputTokenDetails":{"textTokens":10},"outputTokens":10,"totalTokens":13},"warnings":[]},{"callId":"test-telemetry-call-id","content":[{"text":"Hello, world!","type":"text"}],"finishReason":"stop","model":{"modelId":"mock-model-id","provider":"mock-provider"},"performance":{"effectiveOutputTokensPerSecond":0,"effectiveTotalTokensPerSecond":0,"inputTokensPerSecond":0,"outputTokensPerSecond":0,"responseTimeMs":0,"stepTimeMs":0,"timeBetweenOutputChunksMs":{"avg":0,"max":0,"median":0,"min":0,"p10":0,"p90":0},"timeToFirstOutputMs":0,"toolExecutionMs":{}},"rawFinishReason":"stop","request":{},"response":{"headers":{"call":"2"},"id":"id-1","messages":[{"content":[{"text":"Hello, world!","type":"text"}],"role":"assistant"}],"modelId":"mock-model-id","timestamp":"1970-01-01T00:00:01.000Z"},"runtimeContext":{"context":"state3"},"stepNumber":1,"toolsContext":{},"usage":{"inputTokenDetails":{"cacheReadTokens":0,"cacheWriteTokens":0,"noCacheTokens":3},"inputTokens":3,"outputTokenDetails":{"reasoningTokens":10,"textTokens":10},"outputTokens":10,"totalTokens":13},"warnings":[]}]}]`), &want); err != nil {
		t.Fatal(err)
	}
	if len(prepares) != len(want) {
		t.Fatalf("prepareStep calls = %d, want %d", len(prepares), len(want))
	}
	steps := r.Steps()
	r87cstreamAssert(t, "final steps", r87cstreamTSSteps(t, steps), r71streamJSON(t, want[0]["steps"]))
	tsUser := func(text string) []types.Message { return r71streamUserMessages(text) }
	wantMessages := [][]types.Message{tsUser("test-input"), append(tsUser("new input from prepareStep"), steps[0].Response.Messages...)}
	for i, p := range prepares {
		w := want[i]
		if p.modelID != w["modelId"] || float64(p.stepNumber) != w["stepNumber"] {
			t.Fatalf("call %d = %s / %d", i, p.modelID, p.stepNumber)
		}
		r75streamAssertJSON(t, fmt.Sprintf("call %d runtimeContext", i), p.runtimeContext, r71streamJSON(t, w["runtimeContext"]))
		if !reflect.DeepEqual(p.messages, wantMessages[i]) {
			t.Fatalf("call %d messages = %#v", i, p.messages)
		}
		if len(p.steps) != i || (i == 1 && !reflect.DeepEqual(p.steps[0], steps[0])) {
			t.Fatalf("call %d steps = %d", i, len(p.steps))
		}
	}
	// The TS messages snapshot of call 1 (the response messages of step 0
	// after the prepared user message).
	r87cstreamAssert(t, "call 1 response messages", r87cstreamTSMessages(t, prepares[1].messages[1:]), r71streamJSON(t, want[1]["messages"].([]interface{})[1:]))
}

// r87cstreamUpperToolResults is TS upperCaseToolResultTransform: a static
// tool-result's output and input value are upper-cased.
func r87cstreamUpperToolResults(_ context.Context, c provider.StreamChunk) []provider.StreamChunk {
	if c.Type == provider.ChunkTypeToolResult && c.ToolResult != nil && !c.ToolResult.Dynamic {
		tr := *c.ToolResult
		if s, ok := tr.Result.(string); ok {
			tr.Result = strings.ToUpper(s)
		}
		input := map[string]interface{}{}
		for k, v := range tr.Input {
			input[k] = v
		}
		if s, ok := input["value"].(string); ok {
			input["value"] = strings.ToUpper(s)
		}
		tr.Input = input
		c.ToolResult = &tr
	}
	return []provider.StreamChunk{c}
}

// r87cstreamTransformedRun is the TS options.stopWhen "2 steps: initial,
// tool-result with transformed tool results" setup (reasoning id "id-0").
func r87cstreamTransformedRun(t *testing.T) *r87cstreamTwoStepRun {
	t.Helper()
	chunks := r87cstreamTwoStepChunks()
	for i := 1; i <= 3; i++ {
		chunks[0][i].ID = "id-0"
	}
	return r87cstreamTwoSteps(t, StreamTextOptions{Model: r71streamModel(nil, chunks...), ExperimentalTransform: []StreamTransformFunc{r87cstreamUpperToolResults}})
}

// stream-text.test.ts 16491: options.stopWhen > 2 steps: initial,
// tool-result with transformed tool results > should contain assistant
// response message and tool message from all steps.
func TestR87cstreamTransformedFullStream(t *testing.T) {
	run := r87cstreamTransformedRun(t)
	r87cstreamAssert(t, "parts", r87cstreamTSParts(run.parts), `[{"type":"start"},{"request":{},"type":"start-step","warnings":[]},{"id":"id-0","type":"reasoning-start"},{"id":"id-0","text":"thinking","type":"reasoning-delta"},{"id":"id-0","type":"reasoning-end"},{"input":{"value":"value"},"toolCallId":"call-1","toolName":"tool1","type":"tool-call"},{"dynamic":false,"input":{"value":"VALUE"},"output":"RESULT1","toolCallId":"call-1","toolName":"tool1","type":"tool-result"},{"finishReason":"tool-calls","performance":{"effectiveOutputTokensPerSecond":16.666666666666668,"effectiveTotalTokensPerSecond":21.666666666666668,"inputTokensPerSecond":30,"outputTokensPerSecond":20,"responseTimeMs":600,"stepTimeMs":1000,"timeBetweenOutputChunksMs":{"avg":400,"max":400,"median":400,"min":400,"p10":400,"p90":400},"timeToFirstOutputMs":100,"toolExecutionMs":{"call-1":0}},"response":{"headers":{"call":"1"},"id":"id-0","modelId":"mock-model-id","timestamp":"1970-01-01T00:00:00.000Z"},"type":"finish-step","usage":{"inputTokenDetails":{"noCacheTokens":3},"inputTokens":3,"outputTokenDetails":{"textTokens":10},"outputTokens":10,"totalTokens":13}},{"request":{},"type":"start-step","warnings":[]},{"id":"1","type":"text-start"},{"id":"1","text":"Hello, ","type":"text-delta"},{"id":"1","text":"world!","type":"text-delta"},{"id":"1","type":"text-end"},{"finishReason":"stop","performance":{"effectiveOutputTokensPerSecond":0,"effectiveTotalTokensPerSecond":0,"inputTokensPerSecond":0,"outputTokensPerSecond":0,"responseTimeMs":0,"stepTimeMs":0,"timeBetweenOutputChunksMs":{"avg":0,"max":0,"median":0,"min":0,"p10":0,"p90":0},"timeToFirstOutputMs":0,"toolExecutionMs":{}},"rawFinishReason":"stop","response":{"headers":{"call":"2"},"id":"id-1","modelId":"mock-model-id","timestamp":"1970-01-01T00:00:01.000Z"},"type":"finish-step","usage":{"inputTokenDetails":{"cacheReadTokens":0,"cacheWriteTokens":0,"noCacheTokens":3},"inputTokens":3,"outputTokenDetails":{"reasoningTokens":10,"textTokens":10},"outputTokens":10,"totalTokens":13}},{"finishReason":"stop","rawFinishReason":"stop","totalUsage":{"inputTokenDetails":{"cacheReadTokens":0,"cacheWriteTokens":0,"noCacheTokens":6},"inputTokens":6,"outputTokenDetails":{"reasoningTokens":10,"textTokens":20},"outputTokens":20,"totalTokens":26},"type":"finish"}]`)
}

// stream-text.test.ts 17395, 17414, 17433, 17453, 17457, 17461, 17671:
// options.stopWhen > 2 steps: initial, tool-result with transformed tool
// results > value promises.
func TestR87cstreamTransformedValuePromises(t *testing.T) {
	r := r87cstreamTransformedRun(t).result
	r87cstreamAssert(t, "17395 totalUsage", r.TotalUsage(), `{"inputTokenDetails":{"cacheReadTokens":0,"cacheWriteTokens":0,"noCacheTokens":6},"inputTokens":6,"outputTokenDetails":{"reasoningTokens":10,"textTokens":20},"outputTokens":20,"totalTokens":26}`)
	r87cstreamAssert(t, "17414 usage", r.Usage(), `{"inputTokenDetails":{"cacheReadTokens":0,"cacheWriteTokens":0,"noCacheTokens":6},"inputTokens":6,"outputTokenDetails":{"reasoningTokens":10,"textTokens":20},"outputTokens":20,"totalTokens":26}`)
	r87cstreamAssert(t, "17433 finalStep.usage", r.FinalStep().Usage, `{"inputTokenDetails":{"cacheReadTokens":0,"cacheWriteTokens":0,"noCacheTokens":3},"inputTokens":3,"outputTokenDetails":{"reasoningTokens":10,"textTokens":10},"outputTokens":10,"totalTokens":13}`)
	if r.FinishReason() != types.FinishReasonStop {
		t.Fatalf("17453 finishReason = %q", r.FinishReason())
	}
	if r.Text() != "Hello, world!" {
		t.Fatalf("17457 text = %q", r.Text())
	}
	r87cstreamAssert(t, "17461 steps", r87cstreamTSSteps(t, r.Steps()), `[{"callId":"test-telemetry-call-id","content":[{"text":"thinking","type":"reasoning"},{"input":{"value":"value"},"toolCallId":"call-1","toolName":"tool1","type":"tool-call"},{"dynamic":false,"input":{"value":"VALUE"},"output":"RESULT1","toolCallId":"call-1","toolName":"tool1","type":"tool-result"}],"finishReason":"tool-calls","model":{"modelId":"mock-model-id","provider":"mock-provider"},"performance":{"effectiveOutputTokensPerSecond":16.666666666666668,"effectiveTotalTokensPerSecond":21.666666666666668,"inputTokensPerSecond":30,"outputTokensPerSecond":20,"responseTimeMs":600,"stepTimeMs":1000,"timeBetweenOutputChunksMs":{"avg":400,"max":400,"median":400,"min":400,"p10":400,"p90":400},"timeToFirstOutputMs":100,"toolExecutionMs":{"call-1":0}},"request":{},"response":{"headers":{"call":"1"},"id":"id-0","messages":[{"content":[{"text":"thinking","type":"reasoning"},{"input":{"value":"value"},"toolCallId":"call-1","toolName":"tool1","type":"tool-call"}],"role":"assistant"},{"content":[{"output":{"type":"text","value":"RESULT1"},"toolCallId":"call-1","toolName":"tool1","type":"tool-result"}],"role":"tool"}],"modelId":"mock-model-id","timestamp":"1970-01-01T00:00:00.000Z"},"runtimeContext":{},"stepNumber":0,"toolsContext":{},"usage":{"inputTokenDetails":{"noCacheTokens":3},"inputTokens":3,"outputTokenDetails":{"textTokens":10},"outputTokens":10,"totalTokens":13},"warnings":[]},{"callId":"test-telemetry-call-id","content":[{"text":"Hello, world!","type":"text"}],"finishReason":"stop","model":{"modelId":"mock-model-id","provider":"mock-provider"},"performance":{"effectiveOutputTokensPerSecond":0,"effectiveTotalTokensPerSecond":0,"inputTokensPerSecond":0,"outputTokensPerSecond":0,"responseTimeMs":0,"stepTimeMs":0,"timeBetweenOutputChunksMs":{"avg":0,"max":0,"median":0,"min":0,"p10":0,"p90":0},"timeToFirstOutputMs":0,"toolExecutionMs":{}},"rawFinishReason":"stop","request":{},"response":{"headers":{"call":"2"},"id":"id-1","messages":[{"content":[{"text":"Hello, world!","type":"text"}],"role":"assistant"}],"modelId":"mock-model-id","timestamp":"1970-01-01T00:00:01.000Z"},"runtimeContext":{},"stepNumber":1,"toolsContext":{},"usage":{"inputTokenDetails":{"cacheReadTokens":0,"cacheWriteTokens":0,"noCacheTokens":3},"inputTokens":3,"outputTokenDetails":{"reasoningTokens":10,"textTokens":10},"outputTokens":10,"totalTokens":13},"warnings":[]}]`)
	r87cstreamAssert(t, "17671 responseMessages", r87cstreamTSMessages(t, r.ResponseMessages()), `[{"content":[{"text":"thinking","type":"reasoning"},{"input":{"value":"value"},"toolCallId":"call-1","toolName":"tool1","type":"tool-call"}],"role":"assistant"},{"content":[{"output":{"type":"text","value":"RESULT1"},"toolCallId":"call-1","toolName":"tool1","type":"tool-result"}],"role":"tool"},{"content":[{"text":"Hello, world!","type":"text"}],"role":"assistant"}]`)
}
