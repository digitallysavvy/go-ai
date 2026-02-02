package tools

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTextEditor20250728_Basic(t *testing.T) {
	tool := TextEditor20250728(TextEditor20250728Args{})

	assert.Equal(t, "anthropic.text_editor_20250728", tool.Name)
	assert.NotEmpty(t, tool.Description)
	assert.True(t, tool.ProviderExecuted)
	assert.NotNil(t, tool.Parameters)
	assert.NotNil(t, tool.Execute)
}

func TestTextEditor20250728_WithMaxCharacters(t *testing.T) {
	maxChars := 100000
	tool := TextEditor20250728(TextEditor20250728Args{
		MaxCharacters: &maxChars,
	})

	assert.Contains(t, tool.Description, "100000")
	assert.Contains(t, tool.Description, "characters")
}

func TestTextEditor20250728_Schema(t *testing.T) {
	tool := TextEditor20250728(TextEditor20250728Args{})

	params, ok := tool.Parameters.(map[string]interface{})
	require.True(t, ok)

	// Verify required fields
	required, ok := params["required"].([]string)
	require.True(t, ok)
	assert.Contains(t, required, "command")
	assert.Contains(t, required, "path")

	// Verify properties
	properties, ok := params["properties"].(map[string]interface{})
	require.True(t, ok)

	// Check command property
	command, ok := properties["command"].(map[string]interface{})
	require.True(t, ok)
	commandEnum, ok := command["enum"].([]string)
	require.True(t, ok)
	assert.ElementsMatch(t, []string{"view", "create", "str_replace", "insert"}, commandEnum)

	// Check all expected properties exist
	expectedProps := []string{
		"command", "path", "file_text", "insert_line",
		"new_str", "insert_text", "old_str", "view_range",
	}

	for _, prop := range expectedProps {
		assert.Contains(t, properties, prop)
	}
}

func TestTextEditor20250728_NoUndoEdit(t *testing.T) {
	tool := TextEditor20250728(TextEditor20250728Args{})

	params, ok := tool.Parameters.(map[string]interface{})
	require.True(t, ok)

	properties, ok := params["properties"].(map[string]interface{})
	require.True(t, ok)

	command, ok := properties["command"].(map[string]interface{})
	require.True(t, ok)

	commandEnum, ok := command["enum"].([]string)
	require.True(t, ok)

	// Verify undo_edit is NOT in the enum
	assert.NotContains(t, commandEnum, "undo_edit")

	// Verify description mentions this
	assert.Contains(t, tool.Description, "undo_edit")
	assert.Contains(t, tool.Description, "not supported")
}
