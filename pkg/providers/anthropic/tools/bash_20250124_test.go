package tools

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockBashSandbox struct {
	command string
	restart bool
	result  interface{}
	err     error
}

type mockExperimentalSandbox struct {
	command string
	result  interface{}
	err     error
}

func (m *mockBashSandbox) ExecuteCommand(ctx context.Context, command string, restart bool) (interface{}, error) {
	m.command = command
	m.restart = restart
	return m.result, m.err
}

func (m *mockExperimentalSandbox) Execute(ctx context.Context, command string, opts struct{}) (interface{}, error) {
	m.command = command
	return m.result, m.err
}

func TestBash20250124_Basic(t *testing.T) {
	tool := Bash20250124()

	assert.Equal(t, "anthropic.bash_20250124", tool.Name)
	assert.NotEmpty(t, tool.Description)
	assert.False(t, tool.ProviderExecuted)
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

func TestBash20250124_UsesRuntimeSandboxWhenPresent(t *testing.T) {
	tool := Bash20250124()
	sandbox := &mockBashSandbox{result: map[string]interface{}{"ok": true}}

	out, err := tool.Execute(t.Context(), map[string]interface{}{"command": "echo hi"}, types.ToolExecutionOptions{
		RuntimeContext: sandbox,
	})
	require.NoError(t, err)
	assert.Equal(t, "echo hi", sandbox.command)
	assert.False(t, sandbox.restart)
	assert.Equal(t, map[string]interface{}{"ok": true}, out)
}

func TestBash20250124_UsesExperimentalSandboxWhenPresent(t *testing.T) {
	tool := Bash20250124()
	sandbox := &mockExperimentalSandbox{result: map[string]interface{}{"stdout": "hi\n"}}

	out, err := tool.Execute(t.Context(), map[string]interface{}{"command": "echo hi"}, types.ToolExecutionOptions{
		ExperimentalSandbox: sandbox,
	})
	require.NoError(t, err)
	assert.Equal(t, "echo hi", sandbox.command)
	assert.Equal(t, map[string]interface{}{"stdout": "hi\n"}, out)
}

func TestBash20250124_NoSandbox(t *testing.T) {
	tool := Bash20250124()

	_, err := tool.Execute(t.Context(), map[string]interface{}{"command": "echo hi"}, types.ToolExecutionOptions{})
	require.EqualError(t, err, "Sandbox is not available")
}
