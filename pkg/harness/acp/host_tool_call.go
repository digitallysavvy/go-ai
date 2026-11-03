package acp

import (
	"encoding/json"
	"fmt"

	"github.com/digitallysavvy/go-ai/pkg/harness"
)

// TypeHostToolCallResolutionRequest is the ACP bridge frame that asks the
// host to resolve a native tool call to one of the harness host tools (TS
// `acp-host-tool-call-resolution-request`, 03dac28304).
const TypeHostToolCallResolutionRequest = "acp-host-tool-call-resolution-request"

// ResolvedHostToolCall is an ACP tool call resolved to one of the host tools
// the harness exposes through its own MCP server. Mirrors TS
// `ACPResolvedHostToolCall`.
type ResolvedHostToolCall struct {
	// ToolName is the host tool's name. It must be one of the ToolNames
	// passed to the resolver.
	ToolName string
	// Input is the host tool's input, with any agent-specific wrapper
	// removed. It must be non-nil.
	Input map[string]any
}

// ResolveHostToolCallOptions is the argument of a ResolveHostToolCallFunc.
// Mirrors the TS `ACPResolveHostToolCall` options object.
type ResolveHostToolCallOptions struct {
	// ToolCall is the merged snapshot of all updates seen so far for a tool
	// call, or a standalone permission-request tool call.
	ToolCall ToolCall
	// ServerName is the harness-owned MCP server name.
	ServerName string
	// ToolNames are the host tool names exposed through that server.
	ToolNames []string
}

// ResolveHostToolCallFunc resolves an ACP tool call to the host tool it
// invokes, or returns nil when the call does not invoke a host tool or
// cannot be resolved from the data seen so far. Mirrors TS
// `ACPResolveHostToolCall`. ACP does not standardize how implementations
// represent MCP tool calls, so each ACP-backed adapter supplies its own.
type ResolveHostToolCallFunc func(ResolveHostToolCallOptions) *ResolvedHostToolCall

// HostToolCallResolutionRequestFrame is the bridge's request to resolve a
// tool call. The host replies with a `tool-result` frame keyed by RequestID
// whose output is `{hostToolCall: {toolName, input} | null}`.
type HostToolCallResolutionRequestFrame struct {
	RequestID  string   `json:"requestId"`
	ToolCall   ToolCall `json:"toolCall"`
	ServerName string   `json:"serverName"`
	ToolNames  []string `json:"toolNames"`
}

// FrameType returns "acp-host-tool-call-resolution-request".
func (HostToolCallResolutionRequestFrame) FrameType() string {
	return TypeHostToolCallResolutionRequest
}

// validateHostToolCallResolutionFields checks the keys TS
// `acpHostToolCallResolutionRequestSchema` requires beyond requestId and
// toolCall: a string serverName and a string-array toolNames.
func validateHostToolCallResolutionFields(fields map[string]json.RawMessage) error {
	invalid := func(key string) error {
		return fmt.Errorf("acp: invalid %s frame: %q is missing or has the wrong type", TypeHostToolCallResolutionRequest, key)
	}
	raw, ok := fields["serverName"]
	if !ok || len(raw) == 0 || raw[0] != '"' {
		return invalid("serverName")
	}
	var names []json.RawMessage
	raw, ok = fields["toolNames"]
	if !ok || len(raw) == 0 || raw[0] != '[' || json.Unmarshal(raw, &names) != nil {
		return invalid("toolNames")
	}
	for _, n := range names {
		var s string
		if len(n) == 0 || n[0] != '"' || json.Unmarshal(n, &s) != nil {
			return invalid("toolNames")
		}
	}
	return nil
}

// resolveHostToolCall runs the adapter resolver for one resolution request
// and checks its result like TS (`acp-v1-harness.ts` 1229-1274): a non-nil
// result must name one of the offered tools and carry an object input. A
// panic in the resolver is reported as an error, like a TS throw.
func resolveHostToolCall(harnessID string, resolver ResolveHostToolCallFunc, f HostToolCallResolutionRequestFrame) (out *ResolvedHostToolCall, err error) {
	defer func() {
		if r := recover(); r != nil {
			out, err = nil, harness.ErrorFromValue(r)
		}
	}()
	out = resolver(ResolveHostToolCallOptions{ToolCall: f.ToolCall, ServerName: f.ServerName, ToolNames: append([]string(nil), f.ToolNames...)})
	if out == nil {
		return nil, nil
	}
	known := false
	for _, n := range f.ToolNames {
		if n == out.ToolName {
			known = true
			break
		}
	}
	if !known || out.Input == nil {
		return nil, fmt.Errorf("%s ACP resolveHostToolCall must return undefined or one of the provided toolNames with an object input.", harnessID) //nolint:staticcheck // matches TS SDK exact error text
	}
	return out, nil
}

// hostToolCallResolutionOutput is the tool-result output TS sends:
// `{hostToolCall: null}` or `{hostToolCall: {toolName, input}}`.
func hostToolCallResolutionOutput(r *ResolvedHostToolCall) map[string]any {
	if r == nil {
		return map[string]any{"hostToolCall": nil}
	}
	return map[string]any{"hostToolCall": map[string]any{"toolName": r.ToolName, "input": r.Input}}
}

// LegacyResolveHostToolCall is the host-tool resolver the ACP bridge ran
// itself before TS 03dac28304 (`resolve-host-tool-call.ts` at ai@7.0.153),
// ported for callers that built acp.Settings directly in go-ai v0.5 and
// want that behavior unchanged: set Settings.ResolveHostToolCall to it.
// It accepts the deferred-tool (`tool_name`/`tool_input`), provider-tool
// (`providerIdentifier`/`toolName`/`args`), server-tool
// (`server`/`tool`/`arguments`) and origin-operation
// (`origin`/`operation`/`arguments`) envelopes and qualified names
// (`mcp__<server>__<tool>`, `<server>__<tool>`, `mcp_<server>_<tool>`), and
// returns a result only when exactly one host tool matches.
func LegacyResolveHostToolCall(opts ResolveHostToolCallOptions) *ResolvedHostToolCall {
	toolCall := opts.ToolCall
	rawInput, ok := toolCall.RawInput.(map[string]any)
	if !ok || rawInput == nil {
		return nil
	}
	has := func(k string) bool { _, ok := rawInput[k]; return ok }
	deferred := has("tool_name")
	providerTool := has("providerIdentifier") && has("toolName") && has("args")
	originOp := has("origin") && has("operation") && has("arguments")
	serverTool := has("server") && has("tool") && has("arguments")
	count := 0
	for _, b := range []bool{deferred, providerTool, originOp, serverTool} {
		if b {
			count++
		}
	}
	if count > 1 {
		return nil
	}
	serverName := opts.ServerName
	// toolCall.name == null in TS: Go carries an absent name as "".
	nameAbsent := toolCall.Name == ""
	var matches []ResolvedHostToolCall
	for _, toolName := range opts.ToolNames {
		isQualified := func(v any) bool {
			s, ok := v.(string)
			return ok && (s == "mcp__"+serverName+"__"+toolName || s == serverName+"__"+toolName || s == "mcp_"+serverName+"_"+toolName)
		}
		var name any
		if !nameAbsent {
			name = toolCall.Name
		}
		metaServer, metaOK := toolCall.Meta["serverName"].(string)
		isDirect := !nameAbsent && toolCall.Name == toolName && metaOK && metaServer == serverName
		switch {
		case deferred:
			if in, ok := rawInput["tool_input"].(map[string]any); ok && isQualified(rawInput["tool_name"]) &&
				(nameAbsent || toolCall.Name == "use_tool" || isQualified(name)) {
				matches = append(matches, ResolvedHostToolCall{ToolName: toolName, Input: in})
			}
		case providerTool:
			if in, ok := rawInput["args"].(map[string]any); ok && rawInput["providerIdentifier"] == serverName && rawInput["toolName"] == toolName &&
				(nameAbsent || isQualified(name)) {
				matches = append(matches, ResolvedHostToolCall{ToolName: toolName, Input: in})
			}
		case serverTool:
			if in, ok := rawInput["arguments"].(map[string]any); ok && rawInput["server"] == serverName && rawInput["tool"] == toolName &&
				(nameAbsent || isQualified(name)) {
				matches = append(matches, ResolvedHostToolCall{ToolName: toolName, Input: in})
			}
		case originOp:
			if in, ok := rawInput["arguments"].(map[string]any); ok && rawInput["origin"] == serverName && rawInput["operation"] == toolName &&
				(nameAbsent || toolCall.Name == toolName || isQualified(name)) {
				matches = append(matches, ResolvedHostToolCall{ToolName: toolName, Input: in})
			}
		default:
			if isDirect || isQualified(name) ||
				(nameAbsent && (isQualified(toolCall.Title) || toolCall.Title == serverName+"-"+toolName)) {
				matches = append(matches, ResolvedHostToolCall{ToolName: toolName, Input: rawInput})
			}
		}
	}
	if len(matches) != 1 {
		return nil
	}
	return &matches[0]
}
