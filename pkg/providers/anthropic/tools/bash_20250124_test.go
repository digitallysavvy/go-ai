package tools

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBash20250124_Basic(t *testing.T) {
	tool := Bash20250124()

	assert.Equal(t, "anthropic.bash_20250124", tool.Name)
	assert.NotEmpty(t, tool.Description)
	assert.True(t, tool.ProviderExecuted)
	assert.NotNil(t, tool.Parameters)
	assert.NotNil(t, tool.Execute)
}

func TestBash20250124_Schema(t *testing.T) {
	tool := Bash20250124()

	params, ok := tool.Parameters.(map[string]interface{})
	require.True(t, ok)

	// Verify required fields
	required, ok := params["required"].([]string)
	require.True(t, ok)
	assert.Contains(t, required, "command")

	// Verify properties
	properties, ok := params["properties"].(map[string]interface{})
	require.True(t, ok)

	// Check command property
	command, ok := properties["command"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "string", command["type"])

	// Check restart property
	restart, ok := properties["restart"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "boolean", restart["type"])
}

func TestBash20250124_Description(t *testing.T) {
	tool := Bash20250124()

	assert.Contains(t, tool.Description, "bash")
	assert.Contains(t, tool.Description, "shell")
	assert.Contains(t, tool.Description, "persistent")
}
