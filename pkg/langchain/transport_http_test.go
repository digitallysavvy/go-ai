package langchain

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func drain(t *testing.T, chunks <-chan ai.UIMessageChunk, errs <-chan error) ([]ai.UIMessageChunk, []error) {
	t.Helper()
	var gotChunks []ai.UIMessageChunk
	var gotErrs []error
	for chunks != nil || errs != nil {
		select {
		case c, ok := <-chunks:
			if !ok {
				chunks = nil
				continue
			}
			gotChunks = append(gotChunks, c)
		case e, ok := <-errs:
			if !ok {
				errs = nil
				continue
			}
			gotErrs = append(gotErrs, e)
		}
	}
	return gotChunks, gotErrs
}

// The transport calls the LangGraph Platform API like TS RemoteGraph: POST
// /runs/stream with x-api-key and an assistant id, and reads server-sent
// events named after the stream mode.
func TestLangSmithDeploymentTransportSpeaksRunsStreamProtocol(t *testing.T) {
	var gotPath, gotKey, gotCustom string
	var body map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath, gotKey, gotCustom = r.URL.Path, r.Header.Get("x-api-key"), r.Header.Get("X-Custom")
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "event: metadata\ndata: {\"run_id\":\"r1\"}\n\n")
		_, _ = io.WriteString(w, "event: messages\ndata: [{\"id\":\"msg-1\",\"type\":\"AIMessageChunk\",\"content\":\"Hel\"},{\"langgraph_step\":1}]\n\n")
		_, _ = io.WriteString(w, "event: updates\ndata: {\"agent\":{}}\n\n")
		_, _ = io.WriteString(w, "event: messages\ndata: [{\"id\":\"msg-1\",\"type\":\"AIMessageChunk\",\"content\":\"lo\"},{\"langgraph_step\":1}]\n\n")
		_, _ = io.WriteString(w, "event: values\ndata: {\"messages\":[]}\n\n")
	}))
	defer server.Close()

	transport := NewLangSmithDeploymentTransport(LangSmithDeploymentTransportOptions{
		URL: server.URL + "/", APIKey: "secret", GraphID: "my-graph", Headers: map[string]string{"X-Custom": "yes"},
	})
	chunks, errs := transport.SendMessages(context.Background(), ai.ChatTransportSendMessagesRequest{
		Messages: []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}}},
	})
	got, gotErrs := drain(t, chunks, errs)
	if len(gotErrs) != 0 {
		t.Fatalf("errors: %v", gotErrs)
	}
	if gotPath != "/runs/stream" || gotKey != "secret" || gotCustom != "yes" {
		t.Fatalf("path=%q key=%q custom=%q", gotPath, gotKey, gotCustom)
	}
	if body["assistant_id"] != "my-graph" || body["if_not_exists"] != "create" {
		t.Fatalf("body = %#v", body)
	}
	modes, _ := json.Marshal(body["stream_mode"])
	if string(modes) != `["values","messages-tuple","updates"]` {
		t.Fatalf("stream_mode = %s", modes)
	}
	input, _ := body["input"].(map[string]interface{})
	if msgs, _ := input["messages"].([]interface{}); len(msgs) != 1 {
		t.Fatalf("input = %#v", input)
	}
	var text strings.Builder
	for _, c := range got {
		if c["type"] == "text-delta" {
			_, _ = fmt.Fprint(&text, c["delta"])
		}
	}
	if text.String() != "Hello" {
		t.Fatalf("text = %q from %v", text.String(), got)
	}
}

func TestLangSmithDeploymentTransportServerErrorEventBecomesErrorChunk(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "event: error\ndata: {\"error\":\"GraphRecursionError\",\"message\":\"too deep\"}\n\n")
	}))
	defer server.Close()
	var seen error
	transport := NewLangSmithDeploymentTransport(LangSmithDeploymentTransportOptions{URL: server.URL})
	chunks, errs := transport.SendMessagesWithCallbacks(context.Background(), nil, &StreamCallbacks{OnError: func(err error) error { seen = err; return nil }})
	got, gotErrs := drain(t, chunks, errs)
	if len(gotErrs) != 0 {
		t.Fatalf("errors: %v", gotErrs)
	}
	last := got[len(got)-1]
	if last["type"] != "error" || !strings.Contains(fmt.Sprint(last["errorText"]), "too deep") {
		t.Fatalf("chunks = %v", got)
	}
	if seen == nil {
		t.Fatal("OnError not called")
	}
}

func TestLangSmithDeploymentTransportHTTPFailure(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad key", http.StatusUnauthorized)
	}))
	defer server.Close()
	transport := NewLangSmithDeploymentTransport(LangSmithDeploymentTransportOptions{URL: server.URL})
	chunks, errs := transport.SendMessages(context.Background(), ai.ChatTransportSendMessagesRequest{})
	_, gotErrs := drain(t, chunks, errs)
	if len(gotErrs) != 1 || gotErrs[0].Error() != "HTTP 401: bad key" {
		t.Fatalf("errors: %v", gotErrs)
	}
}
