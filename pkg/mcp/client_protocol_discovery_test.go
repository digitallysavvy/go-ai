package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// discoveryMockTransport is a Transport that also implements
// ProtocolVersionDiscoveryTransport, letting tests exercise the modern-era
// `server/discover` handshake (hash e6a9927).
type discoveryMockTransport struct {
	messages  chan *MCPMessage
	connected bool

	supportsDiscovery bool
	// discoverResponse, when set, is returned verbatim as the result of a
	// server/discover request. discoverError, when set, is returned as a
	// JSON-RPC error instead.
	discoverResponse json.RawMessage
	discoverError    *MCPError
	// discoverDelay simulates a slow/unresponsive server/discover method, to
	// exercise the 1s discovery timeout.
	discoverDelay time.Duration

	sentParams map[string]json.RawMessage // method -> last params
}

func newDiscoveryMockTransport() *discoveryMockTransport {
	return &discoveryMockTransport{
		messages:   make(chan *MCPMessage, 10),
		sentParams: map[string]json.RawMessage{},
	}
}

func (t *discoveryMockTransport) Connect(ctx context.Context) error { t.connected = true; return nil }
func (t *discoveryMockTransport) Close() error {
	t.connected = false
	if t.messages != nil {
		close(t.messages)
	}
	return nil
}
func (t *discoveryMockTransport) IsConnected() bool                      { return t.connected }
func (t *discoveryMockTransport) SetProtocolVersion(_ string)            {}
func (t *discoveryMockTransport) SupportsProtocolVersionDiscovery() bool { return t.supportsDiscovery }

func (t *discoveryMockTransport) Send(ctx context.Context, msg *MCPMessage) error {
	t.sentParams[msg.Method] = msg.Params

	switch msg.Method {
	case "server/discover":
		if t.discoverDelay > 0 {
			go func() {
				select {
				case <-time.After(t.discoverDelay):
				case <-ctx.Done():
					return
				}
				t.respondDiscover(msg.ID)
			}()
			return nil
		}
		t.respondDiscover(msg.ID)
	case "initialize":
		response := &MCPMessage{JSONRpc: "2.0", ID: msg.ID}
		result := InitializeResult{
			ProtocolVersion: LatestLegacyProtocolVersion,
			ServerInfo:      ServerInfo{Name: "legacy-server", Version: "1.0.0"},
		}
		data, _ := json.Marshal(result)
		response.Result = data
		t.messages <- response
	case "tools/list":
		response := &MCPMessage{JSONRpc: "2.0", ID: msg.ID}
		result := map[string]interface{}{
			"tools":      []interface{}{},
			"resultType": "success",
		}
		data, _ := json.Marshal(result)
		response.Result = data
		t.messages <- response
	case "prompts/list":
		// Deliberately omits resultType, to exercise the modern-era
		// "missing resultType" validation.
		response := &MCPMessage{JSONRpc: "2.0", ID: msg.ID}
		data, _ := json.Marshal(map[string]interface{}{"prompts": []interface{}{}})
		response.Result = data
		t.messages <- response
	case "completion/complete":
		response := &MCPMessage{JSONRpc: "2.0", ID: msg.ID}
		result := map[string]interface{}{
			"completion": map[string]interface{}{"values": []string{"foo", "bar"}},
			"resultType": "success",
		}
		data, _ := json.Marshal(result)
		response.Result = data
		t.messages <- response
	}
	return nil
}

func (t *discoveryMockTransport) respondDiscover(id interface{}) {
	response := &MCPMessage{JSONRpc: "2.0", ID: id}
	if t.discoverError != nil {
		response.Error = t.discoverError
	} else if t.discoverResponse != nil {
		response.Result = t.discoverResponse
	} else {
		result := DiscoverResult{
			SupportedVersions: []string{LatestProtocolVersion},
			Capabilities:      ServerCapabilities{Tools: &ToolsCapability{}, Completions: &CompletionsCapability{}},
			Instructions:      "modern era instructions",
			ResultType:        "success",
			Meta: map[string]interface{}{
				"io.modelcontextprotocol/serverInfo": map[string]interface{}{
					"name":    "modern-server",
					"version": "2.0.0",
				},
			},
		}
		data, _ := json.Marshal(result)
		response.Result = data
	}
	select {
	case t.messages <- response:
	default:
	}
}

func (t *discoveryMockTransport) Receive(ctx context.Context) (*MCPMessage, error) {
	select {
	case msg, ok := <-t.messages:
		if !ok {
			return nil, context.Canceled
		}
		return msg, nil
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// TestMCPClientProtocolDiscoverySucceeds mirrors TS MCPClient's
// server/discover success path (hash e6a9927): the client negotiates the
// modern protocol era, applies capabilities/instructions/serverInfo from the
// DiscoverResult, and never sends a legacy `initialize` request.
func TestMCPClientProtocolDiscoverySucceeds(t *testing.T) {
	transport := newDiscoveryMockTransport()
	transport.supportsDiscovery = true
	client := NewMCPClient(transport, MCPClientConfig{})

	if err := client.Connect(context.Background()); err != nil {
		t.Fatalf("Connect error: %v", err)
	}
	defer client.Close() //nolint:errcheck

	if client.protocolEra != "modern" || client.protocolVersion != LatestProtocolVersion {
		t.Fatalf("protocolEra=%q protocolVersion=%q, want modern/%s", client.protocolEra, client.protocolVersion, LatestProtocolVersion)
	}
	if client.ServerInfo().Name != "modern-server" || client.ServerInfo().Version != "2.0.0" {
		t.Fatalf("ServerInfo = %#v", client.ServerInfo())
	}
	if client.ServerInstructions() != "modern era instructions" {
		t.Fatalf("ServerInstructions = %q", client.ServerInstructions())
	}
	if client.ServerCapabilities().Completions == nil {
		t.Fatal("expected completions capability to be applied")
	}
	if _, sentInitialize := transport.sentParams["initialize"]; sentInitialize {
		t.Fatal("legacy initialize should not be sent when discovery succeeds")
	}
}

// TestMCPClientProtocolDiscoveryInjectsMeta mirrors TS's modern-era _meta
// injection (hash e6a9927): subsequent requests carry
// io.modelcontextprotocol/{protocolVersion,clientCapabilities,clientInfo}.
func TestMCPClientProtocolDiscoveryInjectsMeta(t *testing.T) {
	transport := newDiscoveryMockTransport()
	transport.supportsDiscovery = true
	client := NewMCPClient(transport, MCPClientConfig{ClientName: "test-client", ClientVersion: "9.9.9"})

	if err := client.Connect(context.Background()); err != nil {
		t.Fatalf("Connect error: %v", err)
	}
	defer client.Close() //nolint:errcheck

	if _, err := client.ListTools(context.Background()); err != nil {
		t.Fatalf("ListTools error: %v", err)
	}

	params, ok := transport.sentParams["tools/list"]
	if !ok {
		t.Fatal("expected a tools/list request to have been sent")
	}
	var decoded struct {
		Meta map[string]interface{} `json:"_meta"`
	}
	if err := json.Unmarshal(params, &decoded); err != nil {
		t.Fatalf("unmarshal params: %v", err)
	}
	if decoded.Meta["io.modelcontextprotocol/protocolVersion"] != LatestProtocolVersion {
		t.Fatalf("_meta.protocolVersion = %v", decoded.Meta["io.modelcontextprotocol/protocolVersion"])
	}
	clientInfo, ok := decoded.Meta["io.modelcontextprotocol/clientInfo"].(map[string]interface{})
	if !ok || clientInfo["name"] != "test-client" || clientInfo["version"] != "9.9.9" {
		t.Fatalf("_meta.clientInfo = %#v", decoded.Meta["io.modelcontextprotocol/clientInfo"])
	}
	if _, ok := decoded.Meta["io.modelcontextprotocol/clientCapabilities"]; !ok {
		t.Fatal("expected _meta.clientCapabilities to be present")
	}
}

// TestMCPClientProtocolDiscoveryFallsBackToLegacyOnPlainError mirrors TS's
// soft-fallback behavior: a server/discover failure that is NOT a
// ModernProtocolErrorCodes error falls back to the legacy initialize
// handshake rather than failing Connect.
func TestMCPClientProtocolDiscoveryFallsBackToLegacyOnPlainError(t *testing.T) {
	transport := newDiscoveryMockTransport()
	transport.supportsDiscovery = true
	transport.discoverError = &MCPError{Code: ErrorCodeMethodNotFound, Message: "Method not found"}
	client := NewMCPClient(transport, MCPClientConfig{})

	if err := client.Connect(context.Background()); err != nil {
		t.Fatalf("Connect error: %v, want fallback to legacy initialize to succeed", err)
	}
	defer client.Close() //nolint:errcheck

	if client.protocolEra != "legacy" || client.protocolVersion != LatestLegacyProtocolVersion {
		t.Fatalf("protocolEra=%q protocolVersion=%q, want legacy/%s", client.protocolEra, client.protocolVersion, LatestLegacyProtocolVersion)
	}
	if client.ServerInfo().Name != "legacy-server" {
		t.Fatalf("ServerInfo = %#v, want legacy initialize result applied", client.ServerInfo())
	}
}

// TestMCPClientProtocolDiscoveryHardFailsOnModernErrorCode mirrors TS: a
// server/discover error whose code is in ModernProtocolErrorCodes is a hard
// failure — Connect must not silently fall back to legacy.
func TestMCPClientProtocolDiscoveryHardFailsOnModernErrorCode(t *testing.T) {
	transport := newDiscoveryMockTransport()
	transport.supportsDiscovery = true
	transport.discoverError = &MCPError{Code: ModernProtocolErrorCodes[0], Message: "modern protocol required"}
	client := NewMCPClient(transport, MCPClientConfig{})

	err := client.Connect(context.Background())
	if err == nil || !strings.Contains(err.Error(), "modern protocol required") {
		t.Fatalf("Connect error = %v, want the modern-protocol error to propagate", err)
	}
	if _, sentInitialize := transport.sentParams["initialize"]; sentInitialize {
		t.Fatal("must not fall back to legacy initialize on a hard modern-protocol error")
	}
}

// TestMCPClientProtocolDiscoverySkippedWhenUnsupported verifies that a
// transport not implementing ProtocolVersionDiscoveryTransport (or reporting
// false) always uses the legacy handshake.
func TestMCPClientProtocolDiscoverySkippedWhenUnsupported(t *testing.T) {
	transport := newDiscoveryMockTransport()
	transport.supportsDiscovery = false
	client := NewMCPClient(transport, MCPClientConfig{})

	if err := client.Connect(context.Background()); err != nil {
		t.Fatalf("Connect error: %v", err)
	}
	defer client.Close() //nolint:errcheck

	if client.protocolEra != "legacy" {
		t.Fatalf("protocolEra = %q, want legacy", client.protocolEra)
	}
	if _, sentDiscover := transport.sentParams["server/discover"]; sentDiscover {
		t.Fatal("server/discover should not be sent when the transport does not support discovery")
	}
}

// TestMCPClientProtocolDiscoveryDisabledByConfig verifies
// MCPClientConfig.ProtocolVersionDiscovery=false skips discovery even when
// the transport supports it.
func TestMCPClientProtocolDiscoveryDisabledByConfig(t *testing.T) {
	transport := newDiscoveryMockTransport()
	transport.supportsDiscovery = true
	disabled := false
	client := NewMCPClient(transport, MCPClientConfig{ProtocolVersionDiscovery: &disabled})

	if err := client.Connect(context.Background()); err != nil {
		t.Fatalf("Connect error: %v", err)
	}
	defer client.Close() //nolint:errcheck

	if client.protocolEra != "legacy" {
		t.Fatalf("protocolEra = %q, want legacy", client.protocolEra)
	}
	if _, sentDiscover := transport.sentParams["server/discover"]; sentDiscover {
		t.Fatal("server/discover should not be sent when discovery is disabled by config")
	}
}

// TestMCPClientValidatesModernResultType mirrors TS's generic response
// handler requiring resultType in the modern era, and rejecting
// input_required.
func TestMCPClientValidatesModernResultType(t *testing.T) {
	transport := newDiscoveryMockTransport()
	transport.supportsDiscovery = true
	client := NewMCPClient(transport, MCPClientConfig{})
	if err := client.Connect(context.Background()); err != nil {
		t.Fatalf("Connect error: %v", err)
	}
	defer client.Close() //nolint:errcheck

	t.Run("missing resultType is rejected", func(t *testing.T) {
		var result map[string]interface{}
		err := client.call(context.Background(), "prompts/list", ListPromptsParams{}, &result)
		if err == nil || !strings.Contains(err.Error(), "missing resultType") {
			t.Fatalf("err = %v", err)
		}
	})
}

// TestMCPClientCompleteRequiresCapability mirrors TS assertCapability for
// completion/complete (hash 68a739a).
func TestMCPClientCompleteRequiresCapability(t *testing.T) {
	transport := newMockTransport()
	client := NewMCPClient(transport, MCPClientConfig{})
	if err := client.Connect(context.Background()); err != nil {
		t.Fatalf("Connect error: %v", err)
	}
	defer client.Close() //nolint:errcheck

	_, err := client.Complete(context.Background(), CompleteRequestParams{
		PromptRef: &PromptReference{Type: "ref/prompt", Name: "greeting"},
		Argument:  CompletionArgument{Name: "name", Value: "A"},
	})
	if err == nil || !strings.Contains(err.Error(), "does not support completions") {
		t.Fatalf("err = %v, want capability error", err)
	}
}

// TestMCPClientCompleteSucceeds exercises the happy path for Complete
// against a server that advertises the completions capability (discovered
// via server/discover in the modern era).
func TestMCPClientCompleteSucceeds(t *testing.T) {
	transport := newDiscoveryMockTransport()
	transport.supportsDiscovery = true
	client := NewMCPClient(transport, MCPClientConfig{})
	if err := client.Connect(context.Background()); err != nil {
		t.Fatalf("Connect error: %v", err)
	}
	defer client.Close() //nolint:errcheck

	result, err := client.Complete(context.Background(), CompleteRequestParams{
		PromptRef: &PromptReference{Type: "ref/prompt", Name: "greeting"},
		Argument:  CompletionArgument{Name: "name", Value: "A"},
	})
	if err != nil {
		t.Fatalf("Complete error: %v", err)
	}
	if len(result.Completion.Values) != 2 || result.Completion.Values[0] != "foo" {
		t.Fatalf("result = %#v", result)
	}

	params := transport.sentParams["completion/complete"]
	var decoded map[string]interface{}
	if err := json.Unmarshal(params, &decoded); err != nil {
		t.Fatalf("unmarshal params: %v", err)
	}
	ref, ok := decoded["ref"].(map[string]interface{})
	if !ok || ref["type"] != "ref/prompt" || ref["name"] != "greeting" {
		t.Fatalf("ref = %#v", decoded["ref"])
	}
}
