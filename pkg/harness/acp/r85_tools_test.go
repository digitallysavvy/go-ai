package acp

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/bridge/bridgetest"
)

// r85toolsRunResolution runs one turn whose bridge sends the given
// resolution requests, then finishes; it returns the tool-result frames the
// host sent and the turn error.
func r85toolsRunResolution(t *testing.T, resolver ResolveHostToolCallFunc, requests ...map[string]any) ([]map[string]any, []harness.StreamPart, error) {
	t.Helper()
	const token = "r85-resolve-token"
	var mu sync.Mutex
	var results []map[string]any
	srv := bridgetest.NewServer(bridgetest.Options{
		Token: token,
		OnStart: func(turn *bridgetest.Turn, _ map[string]any) {
			for _, r := range requests {
				turn.Emit(r)
			}
			time.Sleep(50 * time.Millisecond)
			turn.Emit(map[string]any{
				"type": "finish", "finishReason": map[string]any{"unified": "stop", "raw": "end_turn"},
				"totalUsage": map[string]any{"inputTokens": map[string]any{}, "outputTokens": map[string]any{}},
			})
		},
		OnToolResult: func(frame map[string]any) {
			mu.Lock()
			results = append(results, frame)
			mu.Unlock()
		},
	})
	t.Cleanup(srv.Close)
	h, err := CreateACP(testSettings(func(s *Settings) {
		s.HarnessID = "codex-acp"
		s.MintBridgeToken = func(string) string { return token }
		s.ResolveHostToolCall = resolver
	}))
	if err != nil {
		t.Fatal(err)
	}
	sess, err := h.DoStart(context.Background(), harness.StartOptions{SessionID: "session-1", SessionWorkDir: "/workspace/user-project", SandboxSession: newFakeSandbox(srv)})
	if err != nil {
		t.Fatalf("DoStart: %v", err)
	}
	defer func() { _ = sess.DoDestroy(context.Background()) }()
	var parts []harness.StreamPart
	control, err := sess.DoPromptTurn(context.Background(), harness.PromptTurnOptions{
		Prompt: harness.TextPrompt("Use the weather tool."),
		Emit: func(p harness.StreamPart) {
			mu.Lock()
			parts = append(parts, p)
			mu.Unlock()
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-control.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("turn did not settle")
	}
	time.Sleep(20 * time.Millisecond)
	mu.Lock()
	defer mu.Unlock()
	return append([]map[string]any(nil), results...), append([]harness.StreamPart(nil), parts...), control.Err()
}

func r85toolsResolutionRequest(id string, toolCall map[string]any) map[string]any {
	return map[string]any{
		"type": "acp-host-tool-call-resolution-request", "requestId": id, "toolCall": toolCall,
		"serverName": "ai-sdk-harness-tools", "toolNames": []any{"weather"},
	}
}

// Ports TS acp-harness.test.ts "resolves host tool calls through the adapter
// resolver" (03dac28304).
func TestR85toolsResolvesHostToolCallsThroughResolver(t *testing.T) {
	var mu sync.Mutex
	var calls []ResolveHostToolCallOptions
	resolver := func(o ResolveHostToolCallOptions) *ResolvedHostToolCall {
		mu.Lock()
		calls = append(calls, o)
		mu.Unlock()
		raw, _ := o.ToolCall.RawInput.(map[string]any)
		if raw["server"] == o.ServerName {
			if tool, _ := raw["tool"].(string); tool == "weather" {
				args, _ := raw["arguments"].(map[string]any)
				return &ResolvedHostToolCall{ToolName: tool, Input: args}
			}
		}
		return nil
	}
	results, _, turnErr := r85toolsRunResolution(t, resolver,
		r85toolsResolutionRequest("resolution-1", map[string]any{
			"toolCallId": "call-1", "title": "mcp.ai-sdk-harness-tools.weather",
			"rawInput": map[string]any{"server": "ai-sdk-harness-tools", "tool": "weather", "arguments": map[string]any{"city": "Lima"}},
		}),
		r85toolsResolutionRequest("resolution-2", map[string]any{
			"toolCallId": "call-2", "title": "Read README.md", "rawInput": map[string]any{"path": "README.md"},
		}),
	)
	if turnErr != nil {
		t.Fatalf("turn error = %v", turnErr)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(calls) != 2 || calls[0].ServerName != "ai-sdk-harness-tools" || !reflect.DeepEqual(calls[0].ToolNames, []string{"weather"}) || calls[0].ToolCall.ToolCallID != "call-1" || calls[0].ToolCall.Title != "mcp.ai-sdk-harness-tools.weather" {
		t.Fatalf("resolver calls = %+v", calls)
	}
	got, _ := json.Marshal(results)
	want := `[{"output":{"hostToolCall":{"input":{"city":"Lima"},"toolName":"weather"}},"toolCallId":"resolution-1","type":"tool-result"},{"output":{"hostToolCall":null},"toolCallId":"resolution-2","type":"tool-result"}]`
	if string(got) != want {
		t.Fatalf("tool-result frames =\n%s\nwant\n%s", got, want)
	}
}

// Ports TS "fails the turn when the host tool resolver has a $name": an
// unknown tool name, a nil (non-object) input and a panic (TS throw) each
// answer {hostToolCall: null} and fail the turn with an error part.
func TestR85toolsResolverFailureFailsTurn(t *testing.T) {
	sentinel := errors.New("resolver failed")
	for name, resolver := range map[string]ResolveHostToolCallFunc{
		"unknown tool name": func(ResolveHostToolCallOptions) *ResolvedHostToolCall {
			return &ResolvedHostToolCall{ToolName: "unknown", Input: map[string]any{}}
		},
		"non-object input": func(ResolveHostToolCallOptions) *ResolvedHostToolCall {
			return &ResolvedHostToolCall{ToolName: "weather"}
		},
		"thrown error": func(ResolveHostToolCallOptions) *ResolvedHostToolCall { panic(sentinel) },
	} {
		t.Run(name, func(t *testing.T) {
			results, parts, turnErr := r85toolsRunResolution(t, resolver,
				r85toolsResolutionRequest("resolution-1", map[string]any{"toolCallId": "call-1", "title": "Weather", "rawInput": map[string]any{}}))
			if turnErr == nil {
				t.Fatal("turn succeeded; want the resolver error")
			}
			if name == "thrown error" {
				if !errors.Is(turnErr, sentinel) {
					t.Fatalf("turn error = %v, want the thrown error", turnErr)
				}
			} else if turnErr.Error() != "codex-acp ACP resolveHostToolCall must return undefined or one of the provided toolNames with an object input." {
				t.Fatalf("turn error = %q", turnErr)
			}
			got, _ := json.Marshal(results)
			if string(got) != `[{"output":{"hostToolCall":null},"toolCallId":"resolution-1","type":"tool-result"}]` {
				t.Fatalf("tool-result frames = %s", got)
			}
			sawError := false
			for _, p := range parts {
				if _, ok := p.(*harness.ErrorPart); ok {
					sawError = true
				}
			}
			if !sawError {
				t.Fatal("no error part forwarded")
			}
		})
	}
}

// TS validateACPV1Settings (run by createACP) requires isMcpToolCall and
// resolveHostToolCall, after the harnessId check.
func TestR85toolsDoStartRequiresCallbacks(t *testing.T) {
	for _, tc := range []struct {
		mut  func(*Settings)
		want string
	}{
		{func(s *Settings) { s.IsMcpToolCall = nil }, "ACP isMcpToolCall must be a function."},
		{func(s *Settings) { s.ResolveHostToolCall = nil }, "ACP resolveHostToolCall must be a function."},
		{func(s *Settings) { s.IsMcpToolCall = nil; s.HarnessID = "Bad ID" }, `ACP harnessId must be a stable kebab-case identifier; received "Bad ID".`},
	} {
		h, err := CreateACP(testSettings(tc.mut))
		if h != nil || err == nil || err.Error() != tc.want {
			t.Fatalf("CreateACP err = %v, want %q", err, tc.want)
		}
	}
}

// TS acpHostToolCallResolutionRequestSchema requires a string serverName and
// a string-array toolNames.
func TestR85toolsResolutionFrameValidation(t *testing.T) {
	base := `"type":"acp-host-tool-call-resolution-request","requestId":"r","toolCall":{"toolCallId":"c","title":"t"}`
	if _, _, err := decodeOutbound([]byte(`{` + base + `,"serverName":"s","toolNames":["a"]}`)); err != nil {
		t.Fatalf("valid frame rejected: %v", err)
	}
	for _, bad := range []string{
		`{` + base + `,"toolNames":["a"]}`,
		`{` + base + `,"serverName":1,"toolNames":["a"]}`,
		`{` + base + `,"serverName":"s"}`,
		`{` + base + `,"serverName":"s","toolNames":[1]}`,
		`{"type":"acp-host-tool-call-resolution-request","requestId":"r","serverName":"s","toolNames":[]}`,
	} {
		if _, _, err := decodeOutbound([]byte(bad)); err == nil || !strings.Contains(err.Error(), "invalid acp-host-tool-call-resolution-request frame") {
			t.Errorf("decodeOutbound(%s) err = %v", bad, err)
		}
	}
}

// LegacyResolveHostToolCall ports the cases of the bridge resolver TS
// deleted in 03dac28304 (resolve-host-tool-call.test.ts at e64ed82f27).
func TestR85toolsLegacyResolveHostToolCall(t *testing.T) {
	const server = "ai-sdk-harness-tools"
	names := []string{"weather", "search"}
	in := map[string]any{"city": "Lima"}
	cases := []struct {
		name string
		tc   ToolCall
		want string
	}{
		{"deferred", ToolCall{Name: "use_tool", RawInput: map[string]any{"tool_name": server + "__weather", "tool_input": in}}, "weather"},
		{"provider", ToolCall{RawInput: map[string]any{"providerIdentifier": server, "toolName": "search", "args": in}}, "search"},
		{"server", ToolCall{RawInput: map[string]any{"server": server, "tool": "weather", "arguments": in}}, "weather"},
		{"origin", ToolCall{Name: "weather", RawInput: map[string]any{"origin": server, "operation": "weather", "arguments": in}}, "weather"},
		{"qualified name", ToolCall{Name: "mcp__" + server + "__weather", RawInput: in}, "weather"},
		{"direct", ToolCall{Name: "weather", Meta: map[string]any{"serverName": server}, RawInput: in}, "weather"},
		{"title", ToolCall{Title: server + "-search", RawInput: in}, "search"},
		{"mcp_ title", ToolCall{Title: "mcp_" + server + "_weather", RawInput: in}, "weather"},
		{"ambiguous envelope", ToolCall{RawInput: map[string]any{"tool_name": server + "__weather", "server": server, "tool": "weather", "arguments": in}}, ""},
		{"other server", ToolCall{RawInput: map[string]any{"server": "other", "tool": "weather", "arguments": in}}, ""},
		{"no raw input", ToolCall{Title: server + "-search"}, ""},
		{"named non-qualified", ToolCall{Name: "bash", Title: server + "-search", RawInput: in}, ""},
	}
	for _, c := range cases {
		got := LegacyResolveHostToolCall(ResolveHostToolCallOptions{ToolCall: c.tc, ServerName: server, ToolNames: names})
		switch {
		case c.want == "" && got != nil:
			t.Errorf("%s: got %+v, want nil", c.name, got)
		case c.want != "" && (got == nil || got.ToolName != c.want):
			t.Errorf("%s: got %+v, want %s", c.name, got, c.want)
		}
	}
}
