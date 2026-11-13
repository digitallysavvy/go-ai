package mcp

import (
	"encoding/json"
	"math"
	"testing"
)

// TS JSON.stringify writes a non-finite argument (a model tool-call input
// read from 1e400) as null; encoding/json failed the whole tools/call.
func TestR96integToolsCallArgumentsInfinityIsNull(t *testing.T) {
	msg, err := CreateRequest(1, "tools/call", CallToolParams{Name: "t", Arguments: map[string]interface{}{"x": math.Inf(1), "y": 1}})
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"name":"t","arguments":{"x":null,"y":1}}`; string(msg.Params) != want {
		t.Fatalf("params = %s, want %s", msg.Params, want)
	}
	n, err := CreateNotification("notifications/x", map[string]interface{}{"v": math.NaN()})
	if err != nil || string(n.Params) != `{"v":null}` {
		t.Fatalf("notification params = %s, %v", n.Params, err)
	}
}

// TS JSON.parse reads 1e400 as Infinity in server results, metadata, error
// data and loose extra keys; the SDK re-serializes it as null.
func TestR96integToolsServerInfinityDecodes(t *testing.T) {
	msg, err := parseMCPMessage([]byte(`{"jsonrpc":"2.0","id":1,"result":{"content":[],"structuredContent":{"v":1e400}}}`))
	if err != nil {
		t.Fatal(err)
	}
	var result CallToolResult
	if err := unmarshalSafeJSON(msg.Result, &result); err != nil {
		t.Fatal(err)
	}
	if sc, _ := result.StructuredContent.(map[string]interface{}); sc == nil || sc["v"] != math.Inf(1) {
		t.Fatalf("structuredContent = %#v, want v = +Inf", result.StructuredContent)
	}

	errMsg, err := parseMCPMessage([]byte(`{"jsonrpc":"2.0","id":2,"error":{"code":-1,"message":"m","data":{"d":-1e400}}}`))
	if err != nil {
		t.Fatal(err)
	}
	if d, _ := errMsg.Error.Data.(map[string]interface{}); d == nil || d["d"] != math.Inf(-1) {
		t.Fatalf("error data = %#v, want d = -Inf", errMsg.Error.Data)
	}

	var tools ListToolsResult
	if err := unmarshalSafeJSON([]byte(`{"tools":[{"name":"t","x-n":1e400,"inputSchema":{"type":"object"}}]}`), &tools); err != nil {
		t.Fatal(err)
	}
	if tools.Tools[0].Extra["x-n"] != math.Inf(1) {
		t.Fatalf("extra = %#v, want +Inf", tools.Tools[0].Extra)
	}
	out, err := marshalLoose(struct {
		Name string `json:"name"`
	}{"t"}, tools.Tools[0].Extra)
	if err != nil || string(out) != `{"name":"t","x-n":null}` {
		t.Fatalf("loose = %s, %v", out, err)
	}

	var content ToolResultContent
	if err := json.Unmarshal([]byte(`{"type":"text","text":"t","_meta":{"n":1e400}}`), &content); err != nil {
		t.Fatal(err)
	}
	if content.Meta["n"] != math.Inf(1) {
		t.Fatalf("content _meta = %#v, want +Inf", content.Meta)
	}
}

// TS canonicalJSON uses JSON.stringify per leaf: a non-finite leaf hashes as
// null, the rest of the value is kept (Go wrote the whole value as null).
func TestR96integToolsAppFingerprintInfinityLeaf(t *testing.T) {
	if got := canonicalMCPAppJSON(map[string]interface{}{"b": 1, "a": math.Inf(1)}); got != `{"a":null,"b":1}` {
		t.Fatalf("canonical = %s", got)
	}
}

// A loose capability keeps an out-of-range extra member: Extra() reads it
// as +Inf (TS JSON.parse) and MarshalJSON writes null (JSON.stringify).
func TestR96integToolsLooseCapabilityExtraInfinity(t *testing.T) {
	var caps ServerCapabilities
	if err := unmarshalSafeJSON([]byte(`{"tools":{"x-n":1e400}}`), &caps); err != nil {
		t.Fatal(err)
	}
	if caps.Tools.Extra()["x-n"] != math.Inf(1) {
		t.Fatalf("extra = %#v, want +Inf", caps.Tools.Extra())
	}
	out, err := json.Marshal(caps.Tools)
	if err != nil || string(out) != `{"x-n":null}` {
		t.Fatalf("marshal = %s, %v", out, err)
	}
}
