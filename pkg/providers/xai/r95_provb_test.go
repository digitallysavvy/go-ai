package xai

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func r95provbFinish(t *testing.T, sse string) *provider.StreamChunk {
	t.Helper()
	for _, c := range drainXAIResponses(t, sse) {
		if c.Type == provider.ChunkTypeFinish {
			return c
		}
	}
	t.Fatal("no finish chunk")
	return nil
}

// TestR95provbIncompleteReasonEmptyStringKept: TS keeps a present ""
// incomplete_details.reason with `??`: doGenerate's finishRaw is
// `incomplete_details?.reason ?? status` (raw "", unified other), the stream's
// response.incomplete raw is `reason ?? 'incomplete'` and response.failed's
// raw is `reason ?? 'error'`. Go fell back to the status / the default.
func TestR95provbIncompleteReasonEmptyStringKept(t *testing.T) {
	p := r91provbServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"response","id":"resp_1","status":"completed","incomplete_details":{"reason":""},"output":[],"usage":{"input_tokens":1,"output_tokens":1}}`))
	})
	result, err := NewResponsesLanguageModel(p, "grok-4").DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	if result.RawFinishReason != "" || result.FinishReason != types.FinishReasonOther {
		t.Errorf("generate: finish = %q / %q, want other / \"\"", result.FinishReason, result.RawFinishReason)
	}

	incomplete := r95provbFinish(t, `data: {"type":"response.incomplete","response":{"incomplete_details":{"reason":""},"usage":{"input_tokens":1,"output_tokens":1}}}

`)
	if incomplete.RawFinishReason != "" || incomplete.FinishReason != types.FinishReasonOther {
		t.Errorf("stream incomplete: finish = %q / %q, want other / \"\"", incomplete.FinishReason, incomplete.RawFinishReason)
	}
	failed := r95provbFinish(t, `data: {"type":"response.failed","response":{"incomplete_details":{"reason":""}}}

`)
	if failed.RawFinishReason != "" || failed.FinishReason != types.FinishReasonError {
		t.Errorf("stream failed: finish = %q / %q, want error / \"\"", failed.FinishReason, failed.RawFinishReason)
	}
	// Absent reasons keep the defaults.
	absent := r95provbFinish(t, `data: {"type":"response.failed","response":{}}

`)
	if absent.RawFinishReason != "error" {
		t.Errorf("stream failed without reason: raw = %q, want error", absent.RawFinishReason)
	}
}

// TestR95provbMCPCallEmptyNameKept: TS names an mcp_call
// `mcpToolName ?? part.name ?? 'mcp'`, so a present "" name stays "" and
// only an absent one is "mcp" (generate and stream).
func TestR95provbMCPCallEmptyNameKept(t *testing.T) {
	m := NewResponsesLanguageModel(nil, "grok-4")
	for _, tc := range []struct{ item, want string }{
		{`{"type":"mcp_call","id":"c1","status":"completed","name":"","arguments":"{}"}`, ""},
		{`{"type":"mcp_call","id":"c1","status":"completed","arguments":"{}"}`, "mcp"},
		{`{"type":"mcp_call","id":"c1","status":"completed","name":"lookup","arguments":"{}"}`, "lookup"},
	} {
		var resp xaiResponseWire
		if err := json.Unmarshal([]byte(`{"status":"completed","output":[`+tc.item+`]}`), &resp); err != nil {
			t.Fatal(err)
		}
		result, err := m.convertResponse(resp.response(), nil)
		if err != nil {
			t.Fatal(err)
		}
		if len(result.ToolCalls) != 1 || result.ToolCalls[0].ToolName != tc.want {
			t.Errorf("generate %s: tool calls = %+v, want name %q", tc.item, result.ToolCalls, tc.want)
		}

		var name string
		found := false
		for _, c := range drainXAIResponses(t, `data: {"type":"response.output_item.done","output_index":0,"item":`+tc.item+`}

`) {
			if c.Type == provider.ChunkTypeToolCall {
				name, found = c.ToolCall.ToolName, true
			}
		}
		if !found || name != tc.want {
			t.Errorf("stream %s: tool name = %q (found %v), want %q", tc.item, name, found, tc.want)
		}
	}
}

// TestR95provbSpeechEmptyTraceIDHeader: TS reports
// `responseHeaders['x-trace-id']` when `!= null`, so a present empty header
// is traceId "".
func TestR95provbSpeechEmptyTraceIDHeader(t *testing.T) {
	p := r91provbServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header()["X-Trace-Id"] = []string{""}
		_, _ = w.Write([]byte("audio"))
	})
	res, err := NewSpeechModel(p, "").DoGenerate(context.Background(), &provider.SpeechGenerateOptions{Text: "hi"})
	if err != nil {
		t.Fatal(err)
	}
	meta := res.ProviderMetadata["xai"].(map[string]interface{})
	if v, ok := meta["traceId"]; !ok || v != "" {
		t.Fatalf("metadata = %#v, want traceId \"\"", meta)
	}
}

// TestR95provbFilesFractionalBytesMetadata: xaiFilesResponseSchema `bytes`
// is z.number(), so TS reports a fractional count as is in
// providerMetadata.xai.bytes; ByteSize (an int64 field) is truncated.
func TestR95provbFilesFractionalBytesMetadata(t *testing.T) {
	p := r91provbServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"id":"file-1","bytes":10.5}`))
	})
	res, err := p.Files().(*FilesAPI).GetFileMetadata(context.Background(), provider.FileMetadataOptions{File: types.ProviderReference{"xai": "file-1"}})
	if err != nil {
		t.Fatal(err)
	}
	if got := res.ProviderMetadata["xai"].(map[string]interface{})["bytes"]; got != 10.5 {
		t.Errorf("metadata bytes = %#v, want 10.5", got)
	}
	if res.ByteSize == nil || *res.ByteSize != 10 {
		t.Errorf("ByteSize = %v, want 10", res.ByteSize)
	}
}

// TestR95provbVideoOptionNullsAndPublicURL: TS runtimeSchema members mode,
// videoUrl, referenceVoiceIds, keyframes, storageOptions and user are
// .optional() (a present null is invalid), as are storageOptions.expiresAfter
// and publicUrl; publicUrl is a boolean or an object whose expiresAfter is
// an integer.
func TestR95provbVideoOptionNullsAndPublicURL(t *testing.T) {
	invalid := []map[string]interface{}{
		{"mode": nil}, {"videoUrl": nil}, {"referenceVoiceIds": nil}, {"keyframes": nil},
		{"storageOptions": nil}, {"user": nil},
		{"storageOptions": map[string]interface{}{"filename": "a.mp4", "expiresAfter": nil}},
		{"storageOptions": map[string]interface{}{"filename": "a.mp4", "publicUrl": nil}},
		{"storageOptions": map[string]interface{}{"filename": "a.mp4", "publicUrl": "yes"}},
		{"storageOptions": map[string]interface{}{"filename": "a.mp4", "publicUrl": map[string]interface{}{"expiresAfter": "4000"}}},
		{"storageOptions": map[string]interface{}{"filename": "a.mp4", "publicUrl": map[string]interface{}{"expiresAfter": 4000.5}}},
		{"storageOptions": map[string]interface{}{"filename": "a.mp4", "publicUrl": map[string]interface{}{"expiresAfter": nil}}},
	}
	for _, opts := range invalid {
		if _, _, err := extractVideoProviderOptions(map[string]interface{}{"xai": opts}); err == nil {
			t.Errorf("%v: no error, want invalid xai provider options", opts)
		}
	}
	valid := []map[string]interface{}{
		{"resolution": nil, "pollIntervalMs": nil},
		{"storageOptions": map[string]interface{}{"filename": "a.mp4", "publicUrl": true}},
		{"storageOptions": map[string]interface{}{"filename": "a.mp4", "publicUrl": map[string]interface{}{"expiresAfter": 4000}}},
		{"storageOptions": XAIVideoStorageOptions{Filename: "a.mp4", PublicURL: XAIVideoPublicURLOptions{}}},
	}
	for _, opts := range valid {
		if _, _, err := extractVideoProviderOptions(map[string]interface{}{"xai": opts}); err != nil {
			t.Errorf("%v: %v, want valid", opts, err)
		}
	}
}

// TestR95provbVideoStatusLenientParse: a 202 body that fails TS's status
// schema is `{ status: 'pending' }` (safeParseJSON with the schema); the
// warnings member, which TS's schema strips, never fails a status parse; a
// fractional file_output time is reported as is.
func TestR95provbVideoStatusLenientParse(t *testing.T) {
	body := ""
	p := r91provbServer(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(body, "pending-test") {
			w.WriteHeader(http.StatusAccepted)
		}
		_, _ = w.Write([]byte(body))
	})
	m := NewVideoModel(p, "grok-imagine-video")
	op := []byte(`{"requestId":"r1"}`)

	body = `{"status":"done","video":{"url":"https://cdn.example/v.mp4","file_output":{"file_id":"f"}},"model":"pending-test"}`
	res, err := m.DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: op})
	if err != nil || res.Status != provider.VideoOperationStatusPending {
		t.Fatalf("202 schema failure: %v / %+v, want pending", err, res)
	}

	body = `{"status":"done","video":{"url":"https://cdn.example/v.mp4","file_output":{"file_id":"f","filename":"v.mp4","expires_at":1.5}},"warnings":["note"]}`
	res, err = m.DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: op})
	if err != nil || res.Status != provider.VideoOperationStatusCompleted {
		t.Fatalf("warnings of another shape: %v / %+v, want completed", err, res)
	}
	fo := res.ProviderMetadata["xai"].(map[string]interface{})["fileOutput"].(map[string]interface{})
	if fo["expiresAt"] != 1.5 {
		t.Errorf("fileOutput.expiresAt = %#v, want 1.5", fo["expiresAt"])
	}
}
