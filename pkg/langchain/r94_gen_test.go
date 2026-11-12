package langchain

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Round 94 gen tests (TS 2ebc78b654; @langchain/langgraph 1.7.1,
// @langchain/langgraph-sdk 1.14.0, @langchain/core 1.3.5).

func r94genServer(t *testing.T, status int, contentType, events string, check func(*http.Request, []byte)) *httptest.Server {
	t.Helper()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if check != nil {
			check(r, body)
		}
		if contentType != "" {
			w.Header().Set("Content-Type", contentType)
		}
		w.WriteHeader(status)
		_, _ = io.WriteString(w, events)
	}))
	t.Cleanup(server.Close)
	return server
}

func r94genSendHi(t *testing.T, transport *LangSmithDeploymentTransport) ([]ai.UIMessageChunk, []error) {
	t.Helper()
	chunks, errs := transport.SendMessages(context.Background(), ai.ChatTransportSendMessagesRequest{
		Messages: []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}}},
	})
	return drain(t, chunks, errs)
}

// RemoteGraph.stream runs through Runnable.stream, whose ensureConfig sets
// recursionLimit 25, and _sanitizeConfig sends it as config.recursion_limit.
// Body captured from node (RemoteGraph against a local server).
func TestR94genRunStreamBody(t *testing.T) {
	var got string
	server := r94genServer(t, 200, "text/event-stream", "event: values\ndata: {}\n\n", func(_ *http.Request, body []byte) { got = string(body) })
	if _, errs := r94genSendHi(t, NewLangSmithDeploymentTransport(LangSmithDeploymentTransportOptions{URL: server.URL})); len(errs) != 0 {
		t.Fatal(errs)
	}
	want := `{"input":{"messages":[{"content":"hi","additional_kwargs":{},"response_metadata":{},"role":"human"}]},"config":{"tags":[],"metadata":{},"configurable":{},"recursion_limit":25},"stream_mode":["values","messages-tuple","updates"],"stream_subgraphs":false,"assistant_id":"agent","if_not_exists":"create"}`
	if got != want {
		t.Errorf("body =\n%s\nwant\n%s", got, want)
	}
}

// Each message goes out as RemoteGraph _serializeInputs writes it:
// {...toDict().data, role: getType()}, with no "type" key and the
// constructor's key order (node: @langchain/core 1.3.5 messages).
func TestR94genRunStreamWireMessages(t *testing.T) {
	var got string
	server := r94genServer(t, 200, "text/event-stream", "event: values\ndata: {}\n\n", func(_ *http.Request, body []byte) { got = string(body) })
	transport := NewLangSmithDeploymentTransport(LangSmithDeploymentTransportOptions{URL: server.URL})
	chunks, errs := transport.SendMessages(context.Background(), ai.ChatTransportSendMessagesRequest{Messages: []types.Message{
		{Role: types.RoleSystem, Content: []types.ContentPart{types.TextContent{Text: "sys"}}},
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "a"}, types.ImageContent{URL: "https://x/i.png"}}},
		{Role: types.RoleAssistant, Content: []types.ContentPart{types.TextContent{Text: "txt"}, types.ToolCallContent{ToolCallID: "c1", ToolName: "get", Input: `{"q":"<x>"}`}}},
		{Role: types.RoleAssistant, Content: []types.ContentPart{types.TextContent{Text: "only"}}},
		{Role: types.RoleTool, Content: []types.ContentPart{types.ToolResultContent{ToolCallID: "c1", Output: &types.ToolResultOutput{Type: "text", Value: "res"}}}},
	}})
	if _, gotErrs := drain(t, chunks, errs); len(gotErrs) != 0 {
		t.Fatal(gotErrs)
	}
	want := `{"input":{"messages":[` +
		`{"content":"sys","additional_kwargs":{},"response_metadata":{},"role":"system"},` +
		// The multimodal message keeps content (known difference: TS sends
		// content_blocks, which the server's message coercion drops).
		`{"content":[{"type":"text","text":"a"},{"type":"image","url":"https://x/i.png"}],"additional_kwargs":{},"response_metadata":{},"role":"human"},` +
		`{"content":"txt","tool_calls":[{"id":"c1","name":"get","args":{"q":"<x>"}}],"invalid_tool_calls":[],"additional_kwargs":{},"response_metadata":{},"role":"ai"},` +
		`{"content":"only","tool_calls":[],"invalid_tool_calls":[],"additional_kwargs":{},"response_metadata":{},"role":"ai"},` +
		`{"tool_call_id":"c1","content":"res","additional_kwargs":{},"response_metadata":{},"role":"tool"}]},`
	if !strings.HasPrefix(got, want) {
		t.Errorf("body =\n%s\nwant prefix\n%s", got, want)
	}
}

// langgraph-sdk sets no Accept header, so a configured one goes out as given
// (node: RemoteGraph headers {Accept: "application/json"} reaches the server).
func TestR94genRunStreamConfiguredAccept(t *testing.T) {
	var accept, contentType string
	server := r94genServer(t, 200, "text/event-stream", "event: values\ndata: {}\n\n", func(r *http.Request, _ []byte) {
		accept, contentType = r.Header.Get("Accept"), r.Header.Get("Content-Type")
	})
	transport := NewLangSmithDeploymentTransport(LangSmithDeploymentTransportOptions{URL: server.URL, Headers: map[string]string{"accept": "application/json", "Content-Type": "text/plain"}})
	if _, errs := r94genSendHi(t, transport); len(errs) != 0 {
		t.Fatal(errs)
	}
	if accept != "application/json" || contentType != "application/json" {
		t.Errorf("Accept = %q, Content-Type = %q; want the configured Accept and application/json", accept, contentType)
	}
}

// fetch gives 204 and 205 a null body, and streamWithRetry fails with
// "Expected response body from stream endpoint" (node, 204 with
// text/event-stream).
func TestR94genRunStreamNullBodyStatus(t *testing.T) {
	for _, status := range []int{204, 205} {
		server := r94genServer(t, status, "text/event-stream", "", nil)
		_, errs := r94genSendHi(t, NewLangSmithDeploymentTransport(LangSmithDeploymentTransportOptions{URL: server.URL}))
		if len(errs) != 1 || errs[0].Error() != "Expected response body from stream endpoint" {
			t.Errorf("status %d: errors = %v", status, errs)
		}
	}
}

// RemoteException and GraphInterrupt texts are JSON.stringify of the parsed
// event data, so they keep the wire key order (node: error data
// {"message":"m","error":"E","a":1.50} reads {"message":"m","error":"E","a":1.5}).
func TestR94genRunStreamErrorTextKeyOrder(t *testing.T) {
	for _, tc := range []struct{ events, want string }{
		{"event: error\ndata: {\"message\":\"m\",\"error\":\"E\",\"a\":1.50}\n\n", `{"message":"m","error":"E","a":1.5}`},
		{"event: error\n\n", "null"},
		{"event: updates\ndata: {\"__interrupt__\":[{\"value\":\"v\",\"id\":\"i\",\"e\":[],\"o\":{}}]}\n\n", "[\n  {\n    \"value\": \"v\",\n    \"id\": \"i\",\n    \"e\": [],\n    \"o\": {}\n  }\n]"},
	} {
		server := r94genServer(t, 200, "text/event-stream", tc.events, nil)
		chunks, _ := r94genSendHi(t, NewLangSmithDeploymentTransport(LangSmithDeploymentTransportOptions{URL: server.URL}))
		if len(chunks) == 0 || chunks[len(chunks)-1]["type"] != "error" || chunks[len(chunks)-1]["errorText"] != tc.want {
			t.Errorf("events %q: chunks = %v, want error %q", tc.events, chunks, tc.want)
		}
	}
}
