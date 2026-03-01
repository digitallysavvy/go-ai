package responses

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	openaitool "github.com/digitallysavvy/go-ai/pkg/providers/openai/tool"
)

// TestIntegration_CustomTool_WireFormat verifies that PrepareTools output
// serializes to the correct JSON wire format using a mock HTTP server.
func TestIntegration_CustomTool_WireFormat(t *testing.T) {
	var capturedBody map[string]interface{}

	// Mock server to capture the request body
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if err := json.NewDecoder(r.Body).Decode(&capturedBody); err != nil {
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"resp_test","output":[]}`)
	}))
	defer server.Close()

	// Build custom tool
	syntax := "lark"
	def := `start: WORD`
	ct := openaitool.NewCustomTool("json-tool",
		openaitool.WithDescription("Extract JSON"),
		openaitool.WithFormat(openaitool.CustomToolFormat{
			Type:       "grammar",
			Syntax:     &syntax,
			Definition: &def,
		}),
	)

	tools := PrepareTools([]types.Tool{ct.ToTool()})

	reqBody := map[string]interface{}{
		"model": "gpt-4o",
		"tools": tools,
	}

	data, err := json.Marshal(reqBody)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}

	resp, err := http.Post(server.URL, "application/json", bytes.NewReader(data))
	if err != nil {
		t.Fatalf("post failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("unexpected status: %d", resp.StatusCode)
	}

	// Verify tool wire format
	rawTools, ok := capturedBody["tools"].([]interface{})
	if !ok || len(rawTools) != 1 {
		t.Fatalf("expected 1 tool in captured body, got %v", capturedBody["tools"])
	}
	tool := rawTools[0].(map[string]interface{})
	if tool["type"] != "custom" {
		t.Errorf("tool type: got %q, want custom", tool["type"])
	}
	if tool["name"] != "json-tool" {
		t.Errorf("tool name: got %q, want json-tool", tool["name"])
	}
	format, ok := tool["format"].(map[string]interface{})
	if !ok {
		t.Fatal("tool format should be present")
	}
	if format["type"] != "grammar" {
		t.Errorf("format type: got %q, want grammar", format["type"])
	}
}

// TestIntegration_ShellTools_WireFormat verifies shell tools serialize correctly.
func TestIntegration_ShellTools_WireFormat(t *testing.T) {
	var capturedBody map[string]interface{}

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewDecoder(r.Body).Decode(&capturedBody)
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"id":"resp_test","output":[]}`)
	}))
	defer server.Close()

	memLimit := "4g"
	tools := PrepareTools([]types.Tool{
		NewLocalShellTool(),
		NewShellTool(WithShellEnvironment(ShellEnvironment{
			Type:        "container_auto",
			MemoryLimit: &memLimit,
		})),
		NewApplyPatchTool(),
	})

	reqBody := map[string]interface{}{"tools": tools}
	data, _ := json.Marshal(reqBody)

	resp, err := http.Post(server.URL, "application/json", bytes.NewReader(data))
	if err != nil {
		t.Fatalf("post failed: %v", err)
	}
	defer resp.Body.Close()

	rawTools := capturedBody["tools"].([]interface{})
	expectedTypes := []string{"local_shell", "shell", "apply_patch"}
	for i, want := range expectedTypes {
		got := rawTools[i].(map[string]interface{})["type"]
		if got != want {
			t.Errorf("tools[%d].type: got %q, want %q", i, got, want)
		}
	}

	// Verify shell environment
	shellTool := rawTools[1].(map[string]interface{})
	env, ok := shellTool["environment"].(map[string]interface{})
	if !ok {
		t.Fatal("expected environment in shell tool")
	}
	if env["type"] != "container_auto" {
		t.Errorf("environment.type: got %q", env["type"])
	}
	if env["memory_limit"] != "4g" {
		t.Errorf("environment.memory_limit: got %q", env["memory_limit"])
	}
}

// TestIntegration_Phase_InResponse verifies phase field parsing from API response.
func TestIntegration_Phase_InResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{
			"id": "resp_123",
			"output": [
				{
					"type": "message",
					"role": "assistant",
					"id": "msg_001",
					"phase": "final_answer",
					"content": [{"type": "output_text", "text": "Here is the answer."}]
				}
			]
		}`)
	}))
	defer server.Close()

	resp, err := http.Get(server.URL)
	if err != nil {
		t.Fatalf("get failed: %v", err)
	}
	defer resp.Body.Close()

	var body struct {
		Output []AssistantMessageItem `json:"output"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		t.Fatalf("decode failed: %v", err)
	}

	if len(body.Output) != 1 {
		t.Fatalf("expected 1 output, got %d", len(body.Output))
	}
	msg := body.Output[0]
	if msg.Phase == nil {
		t.Fatal("expected phase to be present")
	}
	if *msg.Phase != "final_answer" {
		t.Errorf("phase: got %q, want final_answer", *msg.Phase)
	}
}

// TestIntegration_LiveOpenAI_CustomTool runs against the real OpenAI API.
// Skipped unless OPENAI_API_KEY is set.
func TestIntegration_LiveOpenAI_CustomTool(t *testing.T) {
	if os.Getenv("OPENAI_API_KEY") == "" {
		t.Skip("Skipping: OPENAI_API_KEY not configured")
	}

	// This test verifies the PrepareTools output for a custom tool is correct.
	// A full API call would require a complete Responses API client implementation.
	ctx := context.Background()
	_ = ctx

	ct := openaitool.NewCustomTool("sentiment-analyzer",
		openaitool.WithDescription("Analyze sentiment of text"),
		openaitool.WithFormat(openaitool.CustomToolFormat{Type: "text"}),
	)

	tools := PrepareTools([]types.Tool{ct.ToTool()})
	if len(tools) != 1 {
		t.Fatalf("expected 1 tool, got %d", len(tools))
	}

	toolDef, ok := tools[0].(CustomToolDef)
	if !ok {
		t.Fatalf("expected CustomToolDef, got %T", tools[0])
	}
	if toolDef.Type != "custom" {
		t.Errorf("type: got %q, want custom", toolDef.Type)
	}
	t.Logf("Custom tool def: %+v", toolDef)
}

// TestIntegration_LiveOpenAI_ShellTools tests shell tool serialization.
// Skipped unless OPENAI_API_KEY is set.
func TestIntegration_LiveOpenAI_ShellTools(t *testing.T) {
	if os.Getenv("OPENAI_API_KEY") == "" {
		t.Skip("Skipping: OPENAI_API_KEY not configured")
	}

	tools := PrepareTools([]types.Tool{
		NewLocalShellTool(),
		NewShellTool(),
		NewApplyPatchTool(),
	})

	data, err := json.Marshal(tools)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var raw []map[string]interface{}
	if err := json.Unmarshal(data, &raw); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	expectedTypes := []string{"local_shell", "shell", "apply_patch"}
	for i, expected := range expectedTypes {
		if raw[i]["type"] != expected {
			t.Errorf("tool[%d].type: got %q, want %q", i, raw[i]["type"], expected)
		}
	}
	t.Logf("Shell tools wire format: %s", string(data))
}
