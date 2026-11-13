package mcp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/internal/jsonexact"
)

// zod object schemas match keys exactly: a server key that matches a
// declared field only case-insensitively is not that field (a loose object
// keeps it as an extra key).
func TestR95toolsServerMessagesDecodeCaseExact(t *testing.T) {
	// ToolSchema: "Title" is not title, it stays an extra key.
	var tools ListToolsResult
	if err := unmarshalSafeJSON([]byte(`{"tools":[{"name":"t","Title":"T","inputSchema":{"type":"object"}}]}`), &tools); err != nil {
		t.Fatal(err)
	}
	if len(tools.Tools) != 1 || tools.Tools[0].Title != "" || tools.Tools[0].Extra["Title"] != "T" {
		t.Fatalf("tool = %+v", tools.Tools)
	}

	// CallToolResultSchema: "IsError" is not isError (which defaults to
	// false); the result is not an error result.
	var result CallToolResult
	if err := unmarshalSafeJSON([]byte(`{"content":[{"type":"text","text":"ok"}],"IsError":true}`), &result); err != nil {
		t.Fatal(err)
	}
	if result.IsError {
		t.Fatalf("IsError = true from a differently cased key")
	}
	out, _ := json.Marshal(result)
	if string(out) != `{"content":[{"type":"text","text":"ok"}],"isError":false,"IsError":true}` {
		t.Fatalf("result JSON = %s", out)
	}

	// ServerCapabilitiesSchema: "Tools" does not advertise tools.
	var init InitializeResult
	if err := unmarshalSafeJSON([]byte(`{"protocolVersion":"2025-11-25","capabilities":{"Tools":{}},"serverInfo":{"name":"s","version":"1"}}`), &init); err != nil {
		t.Fatal(err)
	}
	if init.Capabilities.Tools != nil {
		t.Fatalf("tools capability set from a differently cased key")
	}
	c := &MCPClient{serverCapability: init.Capabilities}
	if err := c.assertCapability("tools/list"); err == nil || err.Error() != "Server does not support tools" {
		t.Fatalf("assertCapability = %v", err)
	}

	// Modern era: `resultType == null` reads the exact key.
	c.protocolEra = "modern"
	if err := c.validateModernResult("tools/list", json.RawMessage(`{"ResultType":"complete","tools":[]}`)); err == nil || err.Error() != "Modern MCP result is missing resultType" {
		t.Fatalf("validateModernResult = %v", err)
	}
	if isInputRequiredResult(json.RawMessage(`{"ResultType":"input_required"}`)) {
		t.Fatal("isInputRequiredResult matched a differently cased key")
	}

	// RequestSchema in inputRequests: method is required with that exact key.
	if _, err := parseInputRequiredResult(json.RawMessage(`{"resultType":"input_required","inputRequests":{"a":{"Method":"elicitation/create"}}}`)); err == nil {
		t.Fatal("inputRequests entry without method accepted")
	}

	// OAuth error responses: "Error_Description" is not error_description.
	parsed, ok := parseOAuthErrorBody([]byte(`{"error":"invalid_grant","Error_Description":"x"}`))
	if !ok || parsed.ErrorDescription != "" {
		t.Fatalf("oauth error = %+v, %v", parsed, ok)
	}
}

// TS HttpMCPTransport / SseMCPTransport send(): an aborted fetch rejects
// with options.signal.reason, which send() rethrows unreported.
func TestR95toolsTransportSendAbortReason(t *testing.T) {
	cause := errors.New("r95 stop")
	release := make(chan struct{})
	hang := func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-release:
		case <-r.Context().Done():
		}
	}

	httpSrv := httptest.NewServer(http.HandlerFunc(hang))
	defer httpSrv.Close()
	reported := 0
	ht := NewHTTPTransport(HTTPTransportConfig{URL: httpSrv.URL, OnError: func(error) { reported++ }})
	if err := ht.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	defer ht.Close() //nolint:errcheck
	ctx, cancel := context.WithCancelCause(context.Background())
	time.AfterFunc(50*time.Millisecond, func() { cancel(cause) })
	msg, _ := CreateRequest(uint64(1), "tools/list", nil)
	if err := ht.Send(ctx, msg); err != cause { //nolint:errorlint // TS rethrows the reason itself
		close(release)
		t.Fatalf("HTTP Send = %v, want the abort reason", err)
	}
	if reported != 0 {
		close(release)
		t.Fatalf("OnError called %d times for the caller's abort", reported)
	}

	sseSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = fmt.Fprint(w, "event: endpoint\ndata: /messages\n\n")
			w.(http.Flusher).Flush()
		}
		hang(w, r)
	}))
	defer sseSrv.Close()
	st := NewSSETransport(SSETransportConfig{URL: sseSrv.URL})
	if err := st.Connect(context.Background()); err != nil {
		close(release)
		t.Fatal(err)
	}
	defer st.Close() //nolint:errcheck
	ctx2, cancel2 := context.WithCancelCause(context.Background())
	time.AfterFunc(50*time.Millisecond, func() { cancel2(cause) })
	err := st.Send(ctx2, msg)
	close(release)    // let the servers' handlers return before they close
	if err != cause { //nolint:errorlint // TS rethrows the reason itself
		t.Fatalf("SSE Send = %v, want the abort reason", err)
	}
}

// Every server-message target decoded with exact keys has json tags: an
// untagged exported field would match only a key spelled like the Go name.
func TestR95toolsResponseTargetsTagged(t *testing.T) {
	if bad := jsonexact.UntaggedFields(
		&ListToolsResult{}, &CallToolResult{}, &InitializeResult{}, &DiscoverResult{},
		&ListResourcesResult{}, &ReadResourceResult{}, &ListResourceTemplatesResult{},
		&ListPromptsResult{}, &GetPromptResult{}, &CompleteResult{}, &ListEventsResult{},
		&OAuthProtectedResourceMetadata{}, &OAuthAuthorizationServerMetadata{}, &oauthErrorResponseBody{},
	); len(bad) > 0 {
		t.Fatalf("response fields without a json tag: %v", bad)
	}
}
