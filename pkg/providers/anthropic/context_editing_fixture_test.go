package anthropic

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// loadFixture reads a JSON fixture file from testdata/context_editing/.
func loadFixture(t *testing.T, name string) []byte {
	t.Helper()
	path := filepath.Join("testdata", "context_editing", name)
	data, err := os.ReadFile(path)
	require.NoError(t, err, "failed to read fixture %s", name)
	return data
}

// newFixtureServer creates an httptest.Server that responds with a fixture body.
// It also captures the last request body so tests can inspect the outgoing request.
func newFixtureServer(t *testing.T, fixture []byte) (*httptest.Server, *map[string]interface{}) {
	t.Helper()
	var lastReqBody map[string]interface{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, json.NewDecoder(r.Body).Decode(&lastReqBody))
		w.Header().Set("Content-Type", "application/json")
		w.Write(fixture)
	}))
	return srv, &lastReqBody
}

// TestContextEditing_ClearToolUses_Fixture verifies that when the API returns a
// clear_tool_uses applied edit, it is correctly parsed and surfaced in the result.
func TestContextEditing_ClearToolUses_Fixture(t *testing.T) {
	fixture := loadFixture(t, "clear_tool_uses_response.json")
	srv, reqBody := newFixtureServer(t, fixture)
	defer srv.Close()

	p := New(Config{APIKey: "test-key", BaseURL: srv.URL})
	model, err := p.LanguageModelWithOptions(ClaudeSonnet4_5, &ModelOptions{
		ContextManagement: &ContextManagement{
			Edits: []ContextManagementEdit{
				NewClearToolUsesEdit().
					WithInputTokensTrigger(10000).
					WithKeepToolUses(3),
			},
		},
	})
	require.NoError(t, err)

	result, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt:    types.Prompt{Text: "What did the previous search find?"},
		MaxTokens: intPtr(100),
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	// Verify the request included context_management config
	assert.Contains(t, *reqBody, "context_management",
		"request body should contain context_management field")

	// Verify the response parsed the applied edit
	require.NotNil(t, result.ContextManagement,
		"result.ContextManagement should be set")
	cmr, ok := result.ContextManagement.(*ContextManagementResponse)
	require.True(t, ok, "ContextManagement should be *ContextManagementResponse")
	require.Len(t, cmr.AppliedEdits, 1)

	edit, ok := cmr.AppliedEdits[0].(*AppliedClearToolUsesEdit)
	require.True(t, ok, "applied edit should be *AppliedClearToolUsesEdit")
	assert.Equal(t, "clear_tool_uses_20250919", edit.Type)
	assert.Equal(t, 12, edit.ClearedToolUses)
	assert.Equal(t, 4200, edit.ClearedInputTokens)

	// Verify text was extracted
	assert.Equal(t, "Based on the search results, here is the information you requested.", result.Text)
}

// TestContextEditing_ClearThinking_Fixture verifies that when the API returns a
// clear_thinking applied edit, it is correctly parsed.
func TestContextEditing_ClearThinking_Fixture(t *testing.T) {
	fixture := loadFixture(t, "clear_thinking_response.json")
	srv, _ := newFixtureServer(t, fixture)
	defer srv.Close()

	p := New(Config{APIKey: "test-key", BaseURL: srv.URL})
	model, err := p.LanguageModelWithOptions(ClaudeOpus4_5, &ModelOptions{
		ContextManagement: &ContextManagement{
			Edits: []ContextManagementEdit{
				NewClearThinkingEdit().WithKeepRecentTurns(2),
			},
		},
	})
	require.NoError(t, err)

	result, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt:    types.Prompt{Text: "Continue the analysis."},
		MaxTokens: intPtr(100),
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	require.NotNil(t, result.ContextManagement)
	cmr, ok := result.ContextManagement.(*ContextManagementResponse)
	require.True(t, ok)
	require.Len(t, cmr.AppliedEdits, 1)

	edit, ok := cmr.AppliedEdits[0].(*AppliedClearThinkingEdit)
	require.True(t, ok, "applied edit should be *AppliedClearThinkingEdit")
	assert.Equal(t, "clear_thinking_20251015", edit.Type)
	assert.Equal(t, 4, edit.ClearedThinkingTurns)
	assert.Equal(t, 9800, edit.ClearedInputTokens)
}

// TestContextEditing_Compact_Fixture verifies that compact applied edits
// are parsed and that compaction iterations are summed correctly.
func TestContextEditing_Compact_Fixture(t *testing.T) {
	fixture := loadFixture(t, "compact_response.json")
	srv, _ := newFixtureServer(t, fixture)
	defer srv.Close()

	p := New(Config{APIKey: "test-key", BaseURL: srv.URL})
	model, err := p.LanguageModelWithOptions(ClaudeSonnet4_5, &ModelOptions{
		ContextManagement: &ContextManagement{
			Edits: []ContextManagementEdit{
				NewCompactEdit().
					WithTrigger(50000).
					WithInstructions("Preserve key decisions."),
			},
		},
	})
	require.NoError(t, err)

	result, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt:    types.Prompt{Text: "What have we decided so far?"},
		MaxTokens: intPtr(100),
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	// Verify compact edit was parsed
	require.NotNil(t, result.ContextManagement)
	cmr, ok := result.ContextManagement.(*ContextManagementResponse)
	require.True(t, ok)
	require.Len(t, cmr.AppliedEdits, 1)

	edit, ok := cmr.AppliedEdits[0].(*AppliedCompactEdit)
	require.True(t, ok, "applied edit should be *AppliedCompactEdit")
	assert.Equal(t, "compact_20260112", edit.Type)

	// Verify usage iterations are summed: compaction (52000 in, 800 out) + message (3000 in, 25 out)
	require.NotNil(t, result.Usage.InputTokens)
	require.NotNil(t, result.Usage.OutputTokens)
	assert.Equal(t, int64(55000), *result.Usage.InputTokens,
		"input tokens should sum compaction + message iterations: 52000+3000=55000")
	assert.Equal(t, int64(825), *result.Usage.OutputTokens,
		"output tokens should sum compaction + message iterations: 800+25=825")
}

// TestContextEditing_CompactionDelta_Fixture verifies that the "compaction delta"
// (the token savings from compaction) can be derived from the usage iterations.
// Before compaction: 95000 input tokens. After compaction summary: 2100 input tokens.
// Delta = 95000 - 2100 = 92900 tokens freed.
func TestContextEditing_CompactionDelta_Fixture(t *testing.T) {
	fixture := loadFixture(t, "compact_with_delta_response.json")
	srv, _ := newFixtureServer(t, fixture)
	defer srv.Close()

	p := New(Config{APIKey: "test-key", BaseURL: srv.URL})
	model, err := p.LanguageModelWithOptions(ClaudeSonnet4_5, &ModelOptions{
		ContextManagement: &ContextManagement{
			Edits: []ContextManagementEdit{
				NewCompactEdit().WithTrigger(80000),
			},
		},
	})
	require.NoError(t, err)

	result, err := model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt:    types.Prompt{Text: "Continue."},
		MaxTokens: intPtr(50),
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	// Compact was applied
	require.NotNil(t, result.ContextManagement)
	cmr, ok := result.ContextManagement.(*ContextManagementResponse)
	require.True(t, ok)
	require.Len(t, cmr.AppliedEdits, 1)
	_, isCompact := cmr.AppliedEdits[0].(*AppliedCompactEdit)
	require.True(t, isCompact)

	// Verify total usage sums both iterations:
	// compaction: 95000 input + 1200 output
	// message:    2100 input  + 18 output
	require.NotNil(t, result.Usage.InputTokens)
	require.NotNil(t, result.Usage.OutputTokens)
	assert.Equal(t, int64(97100), *result.Usage.InputTokens,
		"should sum compaction (95000) + message (2100) iterations")
	assert.Equal(t, int64(1218), *result.Usage.OutputTokens,
		"should sum compaction (1200) + message (18) iterations")

	// Derive the compaction delta from raw iterations in the response
	// The compaction delta is the difference between the compaction input tokens
	// (what was fed into the compaction LLM call) and the message input tokens
	// (the compacted context passed to the actual message generation).
	// A higher delta means more tokens were freed by compaction.
	if result.Usage.Raw != nil {
		if iterations, ok := result.Usage.Raw["iterations"].([]interface{}); ok && len(iterations) == 2 {
			var compactionInputTokens, messageInputTokens float64

			for _, iter := range iterations {
				iterMap, ok := iter.(map[string]interface{})
				if !ok {
					continue
				}
				iterType, _ := iterMap["type"].(string)
				inputTokens, _ := iterMap["input_tokens"].(float64)
				switch iterType {
				case "compaction":
					compactionInputTokens = inputTokens
				case "message":
					messageInputTokens = inputTokens
				}
			}

			compactionDelta := compactionInputTokens - messageInputTokens
			assert.Equal(t, float64(92900), compactionDelta,
				"compaction delta should be 95000 (pre-compaction) - 2100 (post-compaction) = 92900")
		}
	}
}

// TestContextEditing_BetaHeaders_Fixture verifies that the correct beta headers
// are sent for each context management edit type.
func TestContextEditing_BetaHeaders_Fixture(t *testing.T) {
	tests := []struct {
		name         string
		edits        []ContextManagementEdit
		expectedBeta string
	}{
		{
			name:         "clear_tool_uses requires context-management header",
			edits:        []ContextManagementEdit{NewClearToolUsesEdit()},
			expectedBeta: BetaHeaderContextManagement,
		},
		{
			name:         "clear_thinking requires context-management header",
			edits:        []ContextManagementEdit{NewClearThinkingEdit()},
			expectedBeta: BetaHeaderContextManagement,
		},
		{
			name:         "compact requires both context-management and compact headers",
			edits:        []ContextManagementEdit{NewCompactEdit()},
			expectedBeta: BetaHeaderContextManagement + "," + BetaHeaderCompact,
		},
		{
			name: "combined edits use union of required headers",
			edits: []ContextManagementEdit{
				NewClearToolUsesEdit(),
				NewCompactEdit(),
			},
			expectedBeta: BetaHeaderContextManagement + "," + BetaHeaderCompact,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var capturedBeta string
			fixture := loadFixture(t, "compact_response.json")
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				capturedBeta = r.Header.Get("anthropic-beta")
				w.Header().Set("Content-Type", "application/json")
				w.Write(fixture)
			}))
			defer srv.Close()

			p := New(Config{APIKey: "test-key", BaseURL: srv.URL})
			model, err := p.LanguageModelWithOptions(ClaudeSonnet4_5, &ModelOptions{
				ContextManagement: &ContextManagement{Edits: tt.edits},
			})
			require.NoError(t, err)

			_, err = model.DoGenerate(context.Background(), &provider.GenerateOptions{
				Prompt:    types.Prompt{Text: "Test"},
				MaxTokens: intPtr(10),
			})
			require.NoError(t, err)

			assert.Equal(t, tt.expectedBeta, capturedBeta,
				"incorrect anthropic-beta header for %s", tt.name)
		})
	}
}

// TestContextEditing_RequestBody_Fixture verifies that the context_management
// config is correctly serialized into the outgoing request body.
func TestContextEditing_RequestBody_Fixture(t *testing.T) {
	var capturedBody map[string]interface{}
	fixture := loadFixture(t, "clear_tool_uses_response.json")
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.NoError(t, json.NewDecoder(r.Body).Decode(&capturedBody))
		w.Header().Set("Content-Type", "application/json")
		w.Write(fixture)
	}))
	defer srv.Close()

	p := New(Config{APIKey: "test-key", BaseURL: srv.URL})
	model, err := p.LanguageModelWithOptions(ClaudeSonnet4_5, &ModelOptions{
		ContextManagement: &ContextManagement{
			Edits: []ContextManagementEdit{
				NewClearToolUsesEdit().
					WithInputTokensTrigger(10000).
					WithKeepToolUses(3).
					WithExcludeTools("search", "calculator"),
			},
		},
	})
	require.NoError(t, err)

	_, err = model.DoGenerate(context.Background(), &provider.GenerateOptions{
		Prompt:    types.Prompt{Text: "Test"},
		MaxTokens: intPtr(10),
	})
	require.NoError(t, err)

	// Verify context_management was included in the request
	cm, ok := capturedBody["context_management"]
	require.True(t, ok, "request body must include context_management")
	require.NotNil(t, cm)

	// Marshal back to JSON to verify structure
	cmJSON, err := json.Marshal(cm)
	require.NoError(t, err)

	var cmParsed map[string]interface{}
	require.NoError(t, json.Unmarshal(cmJSON, &cmParsed))

	edits, ok := cmParsed["edits"].([]interface{})
	require.True(t, ok, "context_management.edits should be an array")
	require.Len(t, edits, 1)

	edit, ok := edits[0].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "clear_tool_uses_20250919", edit["type"])
}
