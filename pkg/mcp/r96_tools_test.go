package mcp

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// TS sends JSON.stringify(message): `<`, `>`, `&` and U+2028 stay literal.
// encoding/json escaped them (<, &,  ) in the params and again
// in the POST body.
func TestR96toolsRequestBodyIsJSONStringify(t *testing.T) {
	args := map[string]interface{}{"q": "<a&b> "}
	msg, err := CreateRequest(1, "tools/call", CallToolParams{Name: "t", Arguments: args})
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"name":"t","arguments":{"q":"<a&b>` + " " + `"}}`; string(msg.Params) != want {
		t.Fatalf("params = %s, want %s", msg.Params, want)
	}

	var mu sync.Mutex
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		b, _ := io.ReadAll(r.Body)
		mu.Lock()
		body = string(b)
		mu.Unlock()
		w.WriteHeader(http.StatusAccepted)
	}))
	defer srv.Close()
	transport := NewHTTPTransport(HTTPTransportConfig{URL: srv.URL})
	if err := transport.Connect(t.Context()); err != nil {
		t.Fatal(err)
	}
	defer transport.Close() //nolint:errcheck
	notification, _ := CreateNotification("notifications/x", map[string]interface{}{"v": "<&>"})
	if err := transport.Send(t.Context(), notification); err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	if want := `{"method":"notifications/x","params":{"v":"<&>"},"jsonrpc":"2.0"}`; body != want {
		t.Fatalf("body = %s, want %s", body, want)
	}
	if strings.Contains(body, `\u00`) {
		t.Fatalf("body escapes HTML: %s", body)
	}
}

// Loose types re-serialize the server's extra keys as JSON.stringify does.
func TestR96toolsLooseExtraKeysKeepHTMLLiteral(t *testing.T) {
	var tools ListToolsResult
	if err := unmarshalSafeJSON([]byte(`{"tools":[{"name":"t","x-note":"<b>","inputSchema":{"type":"object"}}]}`), &tools); err != nil {
		t.Fatal(err)
	}
	out, err := marshalLoose(struct {
		Name string `json:"name"`
	}{"t"}, tools.Tools[0].Extra)
	if err != nil {
		t.Fatal(err)
	}
	if want := `{"name":"t","x-note":"<b>"}`; string(out) != want {
		t.Fatalf("loose = %s, want %s", out, want)
	}
}

// The undeclared keys of a loose capability are kept as the server sent
// them: a member Go cannot re-encode (TS JSON.parse reads 1e400 as
// Infinity) no longer drops every extra key.
func TestR96toolsLooseCapabilityExtrasKeptRaw(t *testing.T) {
	var caps ServerCapabilities
	if err := unmarshalSafeJSON([]byte(`{"tools":{"listChanged":true,"x-a":"<b>","x-n":1e400}}`), &caps); err != nil {
		t.Fatal(err)
	}
	if caps.Tools == nil || caps.Tools.extraJSON != `{"x-a":"<b>","x-n":1e400}` {
		t.Fatalf("tools extras = %#v", caps.Tools)
	}
}

func TestR96toolsLooseCapabilityExtraKeepsDecodableMembers(t *testing.T) {
	var caps ServerCapabilities
	if err := unmarshalSafeJSON([]byte(`{"tools":{"x-a":"<b>","x-n":1e400}}`), &caps); err != nil {
		t.Fatal(err)
	}
	if got := caps.Tools.Extra(); got["x-a"] != "<b>" {
		t.Fatalf("extra = %#v, want x-a kept", got)
	}
}

// TS z.number().int() takes the number's value: 1.0 and -3.2e4 are
// integers, a fraction, a string or a number beyond the safe range is not.
func TestR96toolsEventTerminationCodeIsAnyIntegerNumber(t *testing.T) {
	for _, tc := range []struct {
		code string
		want int
		ok   bool
	}{
		{"1.0", 1, true}, {"-3.2e4", -32000, true}, {"7", 7, true},
		{"1.5", 0, false}, {`"1"`, 0, false}, {"null", 0, false}, {"9007199254740992", 0, false},
	} {
		raw := map[string]json.RawMessage{
			"type":  json.RawMessage(`"terminated"`),
			"error": json.RawMessage(`{"code":` + tc.code + `,"message":"m"}`),
		}
		_, term, err := parseMCPEventControl(raw)
		if (err == nil) != tc.ok || (tc.ok && term.Error.Code != tc.want) {
			t.Errorf("code %s: term = %+v, err = %v", tc.code, term, err)
		}
	}
}

// TS JSONRPCErrorSchema error.code is z.number().int(): a quoted number is
// not a number (a json.Number target accepted it).
func TestR96toolsJSONRPCErrorCodeStringRejected(t *testing.T) {
	if _, err := parseMCPMessage([]byte(`{"jsonrpc":"2.0","id":1,"error":{"code":"-32000","message":"m"}}`)); err == nil {
		t.Fatal("quoted error.code accepted")
	}
	if msg, err := parseMCPMessage([]byte(`{"jsonrpc":"2.0","id":1,"error":{"code":-3.2e4,"message":"m"}}`)); err != nil || msg.Error.Code != -32000 {
		t.Fatalf("msg = %+v, err = %v", msg, err)
	}
}

// TS splitMCPAppTools reads a non-array visibility as the raw value the
// {...uiMeta} spread keeps: a string answers with String.includes, another
// value has no includes method (TypeError).
func TestR96toolsSplitMCPAppToolsRawVisibility(t *testing.T) {
	tool := func(name string, vis interface{}) MCPTool {
		return MCPTool{Name: name, Meta: map[string]interface{}{"ui": map[string]interface{}{"resourceUri": "ui://x", "visibility": vis}}}
	}
	model, app, err := SplitMCPAppTools(ListToolsResult{Tools: []MCPTool{tool("a", "app"), tool("m", "model,app"), tool("n", nil)}})
	if err != nil {
		t.Fatal(err)
	}
	names := func(r ListToolsResult) (out []string) {
		for _, t := range r.Tools {
			out = append(out, t.Name)
		}
		return out
	}
	if got := names(model); len(got) != 2 || got[0] != "m" || got[1] != "n" {
		t.Fatalf("model-visible = %v, want [m n]", got)
	}
	if got := names(app); len(got) != 2 || got[0] != "a" || got[1] != "m" {
		t.Fatalf("app-visible = %v, want [a m]", got)
	}
	if _, _, err := SplitMCPAppTools(ListToolsResult{Tools: []MCPTool{tool("b", true)}}); err == nil || err.Error() != "visibility.includes is not a function" {
		t.Fatalf("err = %v", err)
	}
}
