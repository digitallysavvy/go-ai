package anthropic

import (
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// ============================================================================
// P1-6: MCP Client tests
// ============================================================================

// MCP-T11: MCPServers option adds mcp-client-2025-04-04 beta header.
func TestMCPServersBetaHeader(t *testing.T) {
	tests := []struct {
		name       string
		mcpServers []MCPServerConfig
		wantHeader bool
	}{
		{
			name: "non-empty MCPServers adds beta header",
			mcpServers: []MCPServerConfig{
				{Type: "url", Name: "my-server", URL: "https://mcp.example.com/sse"},
			},
			wantHeader: true,
		},
		{
			name:       "nil MCPServers adds no header",
			mcpServers: nil,
			wantHeader: false,
		},
		{
			name:       "empty MCPServers slice adds no header",
			mcpServers: []MCPServerConfig{},
			wantHeader: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prov := New(Config{APIKey: "test-key"})
			model := NewLanguageModel(prov, ClaudeSonnet4_6, &ModelOptions{
				MCPServers: tt.mcpServers,
			})

			h := model.getBetaHeaders()
			hasHeader := strings.Contains(h, BetaHeaderMCPClient)
			if hasHeader != tt.wantHeader {
				t.Errorf("MCP beta header presence = %v, want %v (headers=%q)", hasHeader, tt.wantHeader, h)
			}
		})
	}
}

// MCP-T12: mcp_servers request body serializes correctly.
func TestMCPServersRequestBodyShape(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})

	t.Run("basic server without optional fields", func(t *testing.T) {
		model := NewLanguageModel(prov, ClaudeSonnet4_6, &ModelOptions{
			MCPServers: []MCPServerConfig{
				{Type: "url", Name: "server1", URL: "https://mcp.example.com/sse"},
			},
		})
		body := model.buildRequestBody(&provider.GenerateOptions{
			Prompt: types.Prompt{Text: "test"},
		}, false)

		servers, ok := body["mcp_servers"].([]map[string]interface{})
		if !ok {
			t.Fatalf("mcp_servers is not []map[string]interface{}, got %T", body["mcp_servers"])
		}
		if len(servers) != 1 {
			t.Fatalf("expected 1 server, got %d", len(servers))
		}
		srv := servers[0]
		if srv["type"] != "url" {
			t.Errorf("server type = %v, want url", srv["type"])
		}
		if srv["name"] != "server1" {
			t.Errorf("server name = %v, want server1", srv["name"])
		}
		if srv["url"] != "https://mcp.example.com/sse" {
			t.Errorf("server url = %v, want https://mcp.example.com/sse", srv["url"])
		}
		// Optional fields must be absent
		if _, has := srv["authorization_token"]; has {
			t.Error("authorization_token should be absent when not set")
		}
		if _, has := srv["tool_configuration"]; has {
			t.Error("tool_configuration should be absent when not set")
		}
	})

	t.Run("server with authorization token", func(t *testing.T) {
		model := NewLanguageModel(prov, ClaudeSonnet4_6, &ModelOptions{
			MCPServers: []MCPServerConfig{
				{
					Type:               "url",
					Name:               "secure-server",
					URL:                "https://mcp.example.com/sse",
					AuthorizationToken: "Bearer token123",
				},
			},
		})
		body := model.buildRequestBody(&provider.GenerateOptions{
			Prompt: types.Prompt{Text: "test"},
		}, false)

		servers := body["mcp_servers"].([]map[string]interface{})
		srv := servers[0]
		if srv["authorization_token"] != "Bearer token123" {
			t.Errorf("authorization_token = %v, want 'Bearer token123'", srv["authorization_token"])
		}
	})

	t.Run("server with tool configuration", func(t *testing.T) {
		enabled := true
		model := NewLanguageModel(prov, ClaudeSonnet4_6, &ModelOptions{
			MCPServers: []MCPServerConfig{
				{
					Type: "url",
					Name: "filtered-server",
					URL:  "https://mcp.example.com/sse",
					ToolConfiguration: &MCPToolConfiguration{
						AllowedTools: []string{"tool_a", "tool_b"},
						Enabled:      &enabled,
					},
				},
			},
		})
		body := model.buildRequestBody(&provider.GenerateOptions{
			Prompt: types.Prompt{Text: "test"},
		}, false)

		servers := body["mcp_servers"].([]map[string]interface{})
		srv := servers[0]
		tc, ok := srv["tool_configuration"].(map[string]interface{})
		if !ok {
			t.Fatalf("tool_configuration is not map[string]interface{}, got %T", srv["tool_configuration"])
		}
		allowedTools, ok := tc["allowed_tools"].([]string)
		if !ok {
			t.Fatalf("allowed_tools is not []string, got %T", tc["allowed_tools"])
		}
		if len(allowedTools) != 2 || allowedTools[0] != "tool_a" || allowedTools[1] != "tool_b" {
			t.Errorf("allowed_tools = %v, want [tool_a, tool_b]", allowedTools)
		}
		if tc["enabled"] != true {
			t.Errorf("enabled = %v, want true", tc["enabled"])
		}
	})

	t.Run("tool configuration with empty AllowedTools omits allowed_tools", func(t *testing.T) {
		enabled := false
		model := NewLanguageModel(prov, ClaudeSonnet4_6, &ModelOptions{
			MCPServers: []MCPServerConfig{
				{
					Type: "url",
					Name: "server",
					URL:  "https://mcp.example.com/sse",
					ToolConfiguration: &MCPToolConfiguration{
						Enabled: &enabled,
					},
				},
			},
		})
		body := model.buildRequestBody(&provider.GenerateOptions{
			Prompt: types.Prompt{Text: "test"},
		}, false)

		servers := body["mcp_servers"].([]map[string]interface{})
		srv := servers[0]
		tc := srv["tool_configuration"].(map[string]interface{})
		if _, has := tc["allowed_tools"]; has {
			t.Error("allowed_tools should be absent when AllowedTools slice is empty")
		}
		if tc["enabled"] != false {
			t.Errorf("enabled = %v, want false", tc["enabled"])
		}
	})

	t.Run("no MCPServers → no mcp_servers field in body", func(t *testing.T) {
		model := NewLanguageModel(prov, ClaudeSonnet4_6, nil)
		body := model.buildRequestBody(&provider.GenerateOptions{
			Prompt: types.Prompt{Text: "test"},
		}, false)

		if _, has := body["mcp_servers"]; has {
			t.Error("mcp_servers should be absent when MCPServers is empty")
		}
	})

	t.Run("mcp_servers serializes to JSON correctly", func(t *testing.T) {
		model := NewLanguageModel(prov, ClaudeSonnet4_6, &ModelOptions{
			MCPServers: []MCPServerConfig{
				{Type: "url", Name: "srv", URL: "https://mcp.example.com/sse"},
			},
		})
		body := model.buildRequestBody(&provider.GenerateOptions{
			Prompt: types.Prompt{Text: "test"},
		}, false)

		data, err := json.Marshal(body)
		if err != nil {
			t.Fatalf("json.Marshal failed: %v", err)
		}
		jsonStr := string(data)
		if !strings.Contains(jsonStr, `"mcp_servers"`) {
			t.Error("serialized JSON missing mcp_servers")
		}
		if !strings.Contains(jsonStr, `"type":"url"`) {
			t.Error("serialized JSON missing type:url")
		}
	})
}

// MCP-T13: mock mcp_tool_use response parsed into ToolCall.
func TestMCPToolUseResponseParsed(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(prov, ClaudeSonnet4_6, nil)

	// Simulate an API response with an mcp_tool_use content block
	response := anthropicResponse{
		ID:   "msg_mcp_1",
		Type: "message",
		Role: "assistant",
		Content: []anthropicContent{
			{
				Type: "text",
				Text: "Using MCP tool...",
			},
			{
				Type:  "mcp_tool_use",
				ID:    "mcp-call-001",
				Name:  "search_web",
				Input: map[string]interface{}{"query": "golang testing"},
			},
		},
		StopReason: "end_turn",
		Usage: anthropicUsage{
			InputTokens:  10,
			OutputTokens: 5,
		},
	}

	result := model.convertResponse(response)

	if len(result.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool call from mcp_tool_use, got %d", len(result.ToolCalls))
	}
	tc := result.ToolCalls[0]
	if tc.ID != "mcp-call-001" {
		t.Errorf("ToolCall.ID = %q, want mcp-call-001", tc.ID)
	}
	if tc.ToolName != "search_web" {
		t.Errorf("ToolCall.ToolName = %q, want search_web", tc.ToolName)
	}
	if tc.Arguments["query"] != "golang testing" {
		t.Errorf("ToolCall.Arguments[query] = %v, want golang testing", tc.Arguments["query"])
	}
}

// MCP-T13b: mcp_tool_use alongside regular tool_use in response.
func TestMCPToolUseAlongsideRegularToolUse(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(prov, ClaudeSonnet4_6, nil)

	response := anthropicResponse{
		ID:   "msg_2",
		Type: "message",
		Role: "assistant",
		Content: []anthropicContent{
			{
				Type:  "tool_use",
				ID:    "regular-001",
				Name:  "get_weather",
				Input: map[string]interface{}{"city": "NYC"},
			},
			{
				Type:  "mcp_tool_use",
				ID:    "mcp-001",
				Name:  "search",
				Input: map[string]interface{}{"q": "news"},
			},
		},
		StopReason: "tool_use",
		Usage:      anthropicUsage{InputTokens: 20, OutputTokens: 10},
	}

	result := model.convertResponse(response)
	if len(result.ToolCalls) != 2 {
		t.Fatalf("expected 2 tool calls (regular + mcp), got %d", len(result.ToolCalls))
	}
	if result.ToolCalls[0].ID != "regular-001" {
		t.Errorf("first tool call ID = %q, want regular-001", result.ToolCalls[0].ID)
	}
	if result.ToolCalls[1].ID != "mcp-001" {
		t.Errorf("second tool call ID = %q, want mcp-001", result.ToolCalls[1].ID)
	}
}

// MCP streaming: mcp_tool_use in content_block_start emits tool call immediately.
func TestMCPToolUseStreamingEmitsImmediately(t *testing.T) {
	sseData := "" +
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"mcp_tool_use\",\"id\":\"mcp-stream-001\",\"name\":\"web_search\",\"input\":{\"query\":\"go programming\"},\"server_name\":\"my-server\"}}\n\n" +
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"

	stream := newAnthropicStream(io.NopCloser(strings.NewReader(sseData)))

	// Should immediately emit a tool call chunk from content_block_start
	chunk, err := stream.Next()
	if err != nil {
		t.Fatalf("Next() error: %v", err)
	}
	if chunk.Type != provider.ChunkTypeToolCall {
		t.Fatalf("chunk.Type = %v, want ChunkTypeToolCall", chunk.Type)
	}
	if chunk.ToolCall == nil {
		t.Fatal("chunk.ToolCall is nil")
	}
	if chunk.ToolCall.ID != "mcp-stream-001" {
		t.Errorf("ToolCall.ID = %q, want mcp-stream-001", chunk.ToolCall.ID)
	}
	if chunk.ToolCall.ToolName != "web_search" {
		t.Errorf("ToolCall.ToolName = %q, want web_search", chunk.ToolCall.ToolName)
	}
	if chunk.ToolCall.Arguments["query"] != "go programming" {
		t.Errorf("ToolCall.Arguments[query] = %v, want go programming", chunk.ToolCall.Arguments["query"])
	}

	// Next: EOF
	_, err = stream.Next()
	if err != io.EOF {
		t.Errorf("expected io.EOF, got: %v", err)
	}
}

// MCP streaming: mcp_tool_result in content_block_start is a clean no-op.
func TestMCPToolResultStreamingNoOp(t *testing.T) {
	sseData := "" +
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"mcp_tool_result\",\"tool_use_id\":\"mcp-stream-001\",\"is_error\":false,\"content\":{\"results\":[]}}}\n\n" +
		"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}\n\n" +
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":1,\"delta\":{\"type\":\"text_delta\",\"text\":\"Done\"}}\n\n" +
		"event: message_stop\ndata: {\"type\":\"message_stop\"}\n\n"

	stream := newAnthropicStream(io.NopCloser(strings.NewReader(sseData)))

	// mcp_tool_result + content_block_stop: both are no-ops, should skip to next event
	chunk, err := stream.Next()
	if err != nil {
		t.Fatalf("Next() error: %v", err)
	}
	// Should get the text delta, not any tool result chunk
	if chunk.Type != provider.ChunkTypeText {
		t.Errorf("chunk.Type = %v, want ChunkTypeText (mcp_tool_result should be skipped)", chunk.Type)
	}
	if chunk.Text != "Done" {
		t.Errorf("chunk.Text = %q, want Done", chunk.Text)
	}
}

// MCP-T14: Integration test stub.
func TestMCPIntegration(t *testing.T) {
	t.Skip("Integration test: run manually with ANTHROPIC_API_KEY and a live MCP server")
}

// ============================================================================
// P1-7: Container & Skills tests
// ============================================================================

// ACT-T07: container with skills adds all three beta headers.
func TestContainerSkillsBetaHeaders(t *testing.T) {
	tests := []struct {
		name        string
		container   *ContainerConfig
		containerID string
		wantHeaders []string
		wantAbsent  []string
	}{
		{
			name: "container with skills adds all three headers",
			container: &ContainerConfig{
				Skills: []ContainerSkill{
					{Type: "anthropic", SkillID: "web_search"},
				},
			},
			wantHeaders: []string{BetaHeaderCodeExecution20250825, BetaHeaderSkills, BetaHeaderFilesAPI},
		},
		{
			name: "container with multiple skills adds headers",
			container: &ContainerConfig{
				Skills: []ContainerSkill{
					{Type: "anthropic", SkillID: "web_search"},
					{Type: "anthropic", SkillID: "browser"},
				},
			},
			wantHeaders: []string{BetaHeaderCodeExecution20250825, BetaHeaderSkills, BetaHeaderFilesAPI},
		},
		{
			name: "container without skills adds no headers",
			container: &ContainerConfig{
				ID: "container-abc123",
			},
			wantAbsent: []string{BetaHeaderCodeExecution20250825, BetaHeaderSkills, BetaHeaderFilesAPI},
		},
		{
			name:        "ContainerID string adds no skill beta headers",
			containerID: "container-abc123",
			wantAbsent:  []string{BetaHeaderCodeExecution20250825, BetaHeaderSkills, BetaHeaderFilesAPI},
		},
		{
			name:        "no container adds no headers",
			wantAbsent:  []string{BetaHeaderCodeExecution20250825, BetaHeaderSkills, BetaHeaderFilesAPI},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			prov := New(Config{APIKey: "test-key"})
			model := NewLanguageModel(prov, ClaudeSonnet4_6, &ModelOptions{
				Container:   tt.container,
				ContainerID: tt.containerID,
			})

			h := model.getBetaHeaders()
			for _, hdr := range tt.wantHeaders {
				if !strings.Contains(h, hdr) {
					t.Errorf("missing expected header %q in %q", hdr, h)
				}
			}
			for _, hdr := range tt.wantAbsent {
				if strings.Contains(h, hdr) {
					t.Errorf("unexpected header %q present in %q", hdr, h)
				}
			}
		})
	}
}

// ACT-T08: container without skills adds no beta headers (covered above but explicit test).
func TestContainerWithoutSkillsNoBetaHeaders(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(prov, ClaudeSonnet4_6, &ModelOptions{
		Container: &ContainerConfig{ID: "container-xyz"},
	})

	h := model.getBetaHeaders()
	for _, hdr := range []string{BetaHeaderCodeExecution20250825, BetaHeaderSkills, BetaHeaderFilesAPI} {
		if strings.Contains(h, hdr) {
			t.Errorf("unexpected header %q present when container has no skills", hdr)
		}
	}
}

// ACT-T09 / ACT-T13: container body field serializes correctly.
func TestContainerBodySerialization(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})

	t.Run("ContainerID sets container as plain string", func(t *testing.T) {
		model := NewLanguageModel(prov, ClaudeSonnet4_6, &ModelOptions{
			ContainerID: "container-abc123",
		})
		body := model.buildRequestBody(&provider.GenerateOptions{
			Prompt: types.Prompt{Text: "test"},
		}, false)

		container, ok := body["container"].(string)
		if !ok {
			t.Fatalf("container should be string, got %T", body["container"])
		}
		if container != "container-abc123" {
			t.Errorf("container = %q, want container-abc123", container)
		}
	})

	t.Run("Container with ID only serializes as plain string", func(t *testing.T) {
		// TS parity: when Container has no skills, send the ID as a plain string.
		model := NewLanguageModel(prov, ClaudeSonnet4_6, &ModelOptions{
			Container: &ContainerConfig{ID: "container-xyz"},
		})
		body := model.buildRequestBody(&provider.GenerateOptions{
			Prompt: types.Prompt{Text: "test"},
		}, false)

		container, ok := body["container"].(string)
		if !ok {
			t.Fatalf("container with no skills should be plain string, got %T", body["container"])
		}
		if container != "container-xyz" {
			t.Errorf("container = %q, want container-xyz", container)
		}
	})

	// ACT-T14: ContainerConfig with ID + skills serializes correctly.
	t.Run("Container with ID and skills serializes correctly", func(t *testing.T) {
		model := NewLanguageModel(prov, ClaudeSonnet4_6, &ModelOptions{
			Container: &ContainerConfig{
				ID: "container-abc",
				Skills: []ContainerSkill{
					{Type: "anthropic", SkillID: "web_search", Version: "1.0"},
					{Type: "custom", SkillID: "my_tool"},
				},
			},
		})
		body := model.buildRequestBody(&provider.GenerateOptions{
			Prompt: types.Prompt{Text: "test"},
		}, false)

		containerMap, ok := body["container"].(map[string]interface{})
		if !ok {
			t.Fatalf("container should be map, got %T", body["container"])
		}
		if containerMap["id"] != "container-abc" {
			t.Errorf("container.id = %v, want container-abc", containerMap["id"])
		}

		skills, ok := containerMap["skills"].([]map[string]interface{})
		if !ok {
			t.Fatalf("container.skills should be []map[string]interface{}, got %T", containerMap["skills"])
		}
		if len(skills) != 2 {
			t.Fatalf("expected 2 skills, got %d", len(skills))
		}

		// First skill with version
		if skills[0]["type"] != "anthropic" {
			t.Errorf("skill[0].type = %v, want anthropic", skills[0]["type"])
		}
		if skills[0]["skill_id"] != "web_search" {
			t.Errorf("skill[0].skill_id = %v, want web_search", skills[0]["skill_id"])
		}
		if skills[0]["version"] != "1.0" {
			t.Errorf("skill[0].version = %v, want 1.0", skills[0]["version"])
		}

		// Second skill without version
		if skills[1]["skill_id"] != "my_tool" {
			t.Errorf("skill[1].skill_id = %v, want my_tool", skills[1]["skill_id"])
		}
	})

	// ACT-T15: skill version field is omitted when empty.
	t.Run("skill version omitted when empty", func(t *testing.T) {
		model := NewLanguageModel(prov, ClaudeSonnet4_6, &ModelOptions{
			Container: &ContainerConfig{
				Skills: []ContainerSkill{
					{Type: "anthropic", SkillID: "web_search"},
				},
			},
		})
		body := model.buildRequestBody(&provider.GenerateOptions{
			Prompt: types.Prompt{Text: "test"},
		}, false)

		containerMap := body["container"].(map[string]interface{})
		skills := containerMap["skills"].([]map[string]interface{})
		if _, has := skills[0]["version"]; has {
			t.Error("skill version should be absent when empty string")
		}
	})

	t.Run("no container → no container field in body", func(t *testing.T) {
		model := NewLanguageModel(prov, ClaudeSonnet4_6, nil)
		body := model.buildRequestBody(&provider.GenerateOptions{
			Prompt: types.Prompt{Text: "test"},
		}, false)

		if _, has := body["container"]; has {
			t.Error("container should be absent when no container configured")
		}
	})

	t.Run("empty ContainerConfig (no ID, no skills) → no container field", func(t *testing.T) {
		model := NewLanguageModel(prov, ClaudeSonnet4_6, &ModelOptions{
			Container: &ContainerConfig{},
		})
		body := model.buildRequestBody(&provider.GenerateOptions{
			Prompt: types.Prompt{Text: "test"},
		}, false)

		if _, has := body["container"]; has {
			t.Error("container should be absent when ContainerConfig is empty")
		}
	})

	t.Run("ContainerID takes precedence over Container struct", func(t *testing.T) {
		model := NewLanguageModel(prov, ClaudeSonnet4_6, &ModelOptions{
			ContainerID: "string-id",
			Container:   &ContainerConfig{ID: "object-id"},
		})
		body := model.buildRequestBody(&provider.GenerateOptions{
			Prompt: types.Prompt{Text: "test"},
		}, false)

		container, ok := body["container"].(string)
		if !ok {
			t.Fatalf("ContainerID should take precedence, container should be string, got %T", body["container"])
		}
		if container != "string-id" {
			t.Errorf("container = %q, want string-id", container)
		}
	})
}

// TestContainerSkillsWarning: detectSkillsWarning emits a warning when container skills are
// configured but no code execution tool is present. Matches TypeScript SDK behavior.
func TestContainerSkillsWarning(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(prov, ClaudeSonnet4_6, &ModelOptions{
		Container: &ContainerConfig{
			ID:     "container-abc",
			Skills: []ContainerSkill{{Type: "anthropic", SkillID: "web_search"}},
		},
	})

	t.Run("warns when skills present without any code execution tool", func(t *testing.T) {
		w := model.detectSkillsWarning(&provider.GenerateOptions{
			Prompt: types.Prompt{Text: "test"},
		})
		if w == nil {
			t.Fatal("expected warning, got nil")
		}
		if w.Type != "other" {
			t.Errorf("warning.Type = %q, want 'other'", w.Type)
		}
		if w.Message != "code execution tool is required when using skills" {
			t.Errorf("warning.Message = %q", w.Message)
		}
	})

	t.Run("no warning when code-execution-20260120 tool present", func(t *testing.T) {
		w := model.detectSkillsWarning(&provider.GenerateOptions{
			Prompt: types.Prompt{Text: "test"},
			Tools:  []types.Tool{{Name: codeExecution20260120ToolName}},
		})
		if w != nil {
			t.Errorf("expected no warning when 20260120 code execution tool present, got %+v", w)
		}
	})

	t.Run("no warning when code-execution-20250825 tool present", func(t *testing.T) {
		w := model.detectSkillsWarning(&provider.GenerateOptions{
			Prompt: types.Prompt{Text: "test"},
			Tools:  []types.Tool{{Name: codeExecution20250825ToolName}},
		})
		if w != nil {
			t.Errorf("expected no warning when 20250825 code execution tool present, got %+v", w)
		}
	})

	t.Run("no warning when no container skills configured", func(t *testing.T) {
		noSkillsModel := NewLanguageModel(prov, ClaudeSonnet4_6, &ModelOptions{
			Container: &ContainerConfig{ID: "container-xyz"},
		})
		w := noSkillsModel.detectSkillsWarning(&provider.GenerateOptions{
			Prompt: types.Prompt{Text: "test"},
		})
		if w != nil {
			t.Errorf("expected no warning when no skills configured, got %+v", w)
		}
	})

	t.Run("no warning when no container", func(t *testing.T) {
		noContainerModel := NewLanguageModel(prov, ClaudeSonnet4_6, nil)
		w := noContainerModel.detectSkillsWarning(&provider.GenerateOptions{
			Prompt: types.Prompt{Text: "test"},
		})
		if w != nil {
			t.Errorf("expected no warning when no container, got %+v", w)
		}
	})
}

// ACT-T16: Integration test stub.
func TestContainerSkillsIntegration(t *testing.T) {
	t.Skip("Integration test: run manually with ANTHROPIC_API_KEY and container support")
}

// Regression: existing beta headers still work alongside the new ones.
func TestBetaHeadersNoRegressionWithNewOptions(t *testing.T) {
	prov := New(Config{APIKey: "test-key"})
	model := NewLanguageModel(prov, ClaudeOpus4_6, &ModelOptions{
		Speed:      SpeedFast,
		MCPServers: []MCPServerConfig{{Type: "url", Name: "srv", URL: "https://example.com"}},
		Container: &ContainerConfig{
			Skills: []ContainerSkill{{Type: "anthropic", SkillID: "web_search"}},
		},
	})

	h := model.getBetaHeaders()

	// All expected headers present
	for _, want := range []string{
		BetaHeaderFastMode,
		BetaHeaderMCPClient,
		BetaHeaderCodeExecution20250825,
		BetaHeaderSkills,
		BetaHeaderFilesAPI,
	} {
		if !strings.Contains(h, want) {
			t.Errorf("missing header %q in %q", want, h)
		}
	}
}
