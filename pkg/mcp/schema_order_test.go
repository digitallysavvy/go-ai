package mcp

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	toolutil "github.com/digitallysavvy/go-ai/pkg/providerutils/tool"
	"github.com/digitallysavvy/go-ai/pkg/schema"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// An MCP server's tool schema reaches the provider request in the key order the
// server sent, like TS (JS objects keep insertion order).
func TestMCPToolSchemaKeepsServerKeyOrder(t *testing.T) {
	const wire = `{"name":"t","inputSchema":{"type":"object","required":["zeta","alpha"],"properties":{"zeta":{"type":"string","description":"z"},"alpha":{"type":"integer","minimum":1}}}}`
	var tool MCPTool
	if err := json.Unmarshal([]byte(wire), &tool); err != nil {
		t.Fatal(err)
	}
	converter := NewMCPToolConverter(NewMCPClient(newMockTransport(), MCPClientConfig{}))
	tools, err := converter.ToolsFromDefinitions([]MCPTool{tool}, nil)
	if err != nil {
		t.Fatal(err)
	}
	// What core sends to a provider.
	resolved := toolutil.ResolveToolSchemas(tools)
	got, err := json.Marshal(resolved[0].Parameters)
	if err != nil {
		t.Fatal(err)
	}
	want := `{"type":"object","required":["zeta","alpha"],"properties":{"zeta":{"type":"string","description":"z"},"alpha":{"type":"integer","minimum":1}},"additionalProperties":false}`
	if string(got) != want {
		t.Fatalf("schema = %s\nwant     %s", got, want)
	}
	// And through a converter that builds the wire body.
	body, _ := json.Marshal(toolutil.ToOpenAIFormat(resolved))
	if !strings.Contains(string(body), `"required":["zeta","alpha"],"properties":{"zeta"`) {
		t.Fatalf("openai tool body lost order: %s", body)
	}
	// The schema is still readable as a map and validates.
	m, ok := schema.ParametersMap(tools[0].Parameters)
	if !ok || m["additionalProperties"] != false {
		t.Fatalf("ParametersMap = %#v, %v", m, ok)
	}
}

// TS spreads {...inputSchema, properties: inputSchema.properties ?? {},
// additionalProperties: false}: missing keys are appended in that order.
func TestMCPToolSchemaAppendsMissingKeysLikeTS(t *testing.T) {
	var tool MCPTool
	if err := json.Unmarshal([]byte(`{"name":"t","inputSchema":{"type":"object","description":"d"}}`), &tool); err != nil {
		t.Fatal(err)
	}
	converter := NewMCPToolConverter(NewMCPClient(newMockTransport(), MCPClientConfig{}))
	tools, err := converter.ToolsFromDefinitions([]MCPTool{tool}, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := json.Marshal(tools[0].Parameters)
	want := `{"type":"object","description":"d","properties":{},"additionalProperties":false}`
	if string(got) != want {
		t.Fatalf("schema = %s\nwant     %s", got, want)
	}
}

// Through ai.GenerateText: the provider receives the MCP schema in server order.
func TestMCPToolSchemaOrderReachesProviderViaGenerateText(t *testing.T) {
	var tool MCPTool
	if err := json.Unmarshal([]byte(`{"name":"t","inputSchema":{"type":"object","properties":{"zeta":{"type":"string"},"alpha":{"type":"string"}}}}`), &tool); err != nil {
		t.Fatal(err)
	}
	converter := NewMCPToolConverter(NewMCPClient(newMockTransport(), MCPClientConfig{}))
	tools, err := converter.ToolsFromDefinitions([]MCPTool{tool}, nil)
	if err != nil {
		t.Fatal(err)
	}
	model := &testutil.MockLanguageModel{ToolSupport: true}
	if _, err := ai.GenerateText(context.Background(), ai.GenerateTextOptions{Model: model, Prompt: "hi", Tools: tools}); err != nil {
		t.Fatal(err)
	}
	sent := model.GenerateCalls[0].Tools
	got, _ := json.Marshal(toolutil.ToOpenAIFormat(sent))
	if !strings.Contains(string(got), `"properties":{"zeta":{"type":"string"},"alpha":{"type":"string"}}`) {
		t.Fatalf("provider tool body lost order: %s", got)
	}
}

// MCP tools (a Hinted schema) fingerprint like the same schema as a plain map;
// FingerprintTools is the MCP rug-pull check.
func TestMCPToolFingerprintMatchesPlainSchema(t *testing.T) {
	var tool MCPTool
	if err := json.Unmarshal([]byte(`{"name":"t","description":"d","inputSchema":{"type":"object","properties":{"b":{"type":"string"},"a":{"type":"string"}}}}`), &tool); err != nil {
		t.Fatal(err)
	}
	converter := NewMCPToolConverter(NewMCPClient(newMockTransport(), MCPClientConfig{}))
	tools, err := converter.ToolsFromDefinitions([]MCPTool{tool}, nil)
	if err != nil {
		t.Fatal(err)
	}
	got, err := ai.FingerprintTools(tools)
	if err != nil {
		t.Fatalf("FingerprintTools: %v", err)
	}
	plain := tools[0]
	m, _ := schema.ParametersMap(tools[0].Parameters)
	plain.Parameters = m
	want, err := ai.FingerprintTools([]types.Tool{plain})
	if err != nil {
		t.Fatal(err)
	}
	if got["t"] == "" || got["t"] != want["t"] {
		t.Fatalf("fingerprint = %q, want %q", got["t"], want["t"])
	}
}
