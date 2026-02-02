package tools

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestCodeExecution20250825_Basic(t *testing.T) {
	tool := CodeExecution20250825()

	assert.Equal(t, "anthropic.code_execution_20250825", tool.Name)
	assert.NotEmpty(t, tool.Description)
	assert.True(t, tool.ProviderExecuted)
	assert.NotNil(t, tool.Parameters)
	assert.NotNil(t, tool.Execute)
}

func TestCodeExecution20250825_Schema(t *testing.T) {
	tool := CodeExecution20250825()

	params, ok := tool.Parameters.(map[string]interface{})
	require.True(t, ok)

	// Should be a oneOf discriminated union
	oneOf, ok := params["oneOf"].([]interface{})
	require.True(t, ok)
	assert.Len(t, oneOf, 3) // programmatic, bash, text_editor

	// Check discriminator
	discriminator, ok := params["discriminator"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "type", discriminator["propertyName"])
}

func TestCodeExecution20250825_ProgrammaticToolCallSchema(t *testing.T) {
	tool := CodeExecution20250825()

	params, ok := tool.Parameters.(map[string]interface{})
	require.True(t, ok)

	oneOf, ok := params["oneOf"].([]interface{})
	require.True(t, ok)

	// First schema should be programmatic-tool-call
	programmatic, ok := oneOf[0].(map[string]interface{})
	require.True(t, ok)

	properties, ok := programmatic["properties"].(map[string]interface{})
	require.True(t, ok)

	// Check type property
	typeField, ok := properties["type"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "programmatic-tool-call", typeField["const"])

	// Check code property
	code, ok := properties["code"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "string", code["type"])
}

func TestCodeExecution20250825_BashSchema(t *testing.T) {
	tool := CodeExecution20250825()

	params, ok := tool.Parameters.(map[string]interface{})
	require.True(t, ok)

	oneOf, ok := params["oneOf"].([]interface{})
	require.True(t, ok)

	// Second schema should be bash_code_execution
	bash, ok := oneOf[1].(map[string]interface{})
	require.True(t, ok)

	properties, ok := bash["properties"].(map[string]interface{})
	require.True(t, ok)

	// Check type property
	typeField, ok := properties["type"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "bash_code_execution", typeField["const"])

	// Check command property
	command, ok := properties["command"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "string", command["type"])
}

func TestCodeExecution20250825_TextEditorSchema(t *testing.T) {
	tool := CodeExecution20250825()

	params, ok := tool.Parameters.(map[string]interface{})
	require.True(t, ok)

	oneOf, ok := params["oneOf"].([]interface{})
	require.True(t, ok)

	// Third schema should be text_editor_code_execution
	textEditor, ok := oneOf[2].(map[string]interface{})
	require.True(t, ok)

	// Should have its own oneOf for commands
	textEditorOneOf, ok := textEditor["oneOf"].([]interface{})
	require.True(t, ok)
	assert.Len(t, textEditorOneOf, 3) // view, create, str_replace
}

func TestCodeExecution20250825_Description(t *testing.T) {
	tool := CodeExecution20250825()

	// Check for key features in description
	assert.Contains(t, tool.Description, "Python")
	assert.Contains(t, tool.Description, "Bash")
	assert.Contains(t, tool.Description, "sandboxed")
	assert.Contains(t, tool.Description, "programmatic-tool-call")
	assert.Contains(t, tool.Description, "bash_code_execution")
	assert.Contains(t, tool.Description, "text_editor_code_execution")
}
