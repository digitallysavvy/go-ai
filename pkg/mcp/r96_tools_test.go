package mcp

import (
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
