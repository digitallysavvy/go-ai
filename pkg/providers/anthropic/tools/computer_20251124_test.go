package tools

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestComputer20251124_Basic(t *testing.T) {
	tool := Computer20251124(Computer20251124Args{
		DisplayWidthPx:  1920,
		DisplayHeightPx: 1080,
		EnableZoom:      false,
	})

	assert.Equal(t, "anthropic.computer_20251124", tool.Name)
	assert.NotEmpty(t, tool.Description)
	assert.True(t, tool.ProviderExecuted)
	assert.NotNil(t, tool.Parameters)
	assert.NotNil(t, tool.Execute)
}

func TestComputer20251124_WithZoom(t *testing.T) {
	tool := Computer20251124(Computer20251124Args{
		DisplayWidthPx:  1920,
		DisplayHeightPx: 1080,
		EnableZoom:      true,
	})

	assert.Contains(t, tool.Description, "zoom")

	// Verify schema includes zoom action
	params, ok := tool.Parameters.(map[string]interface{})
	require.True(t, ok)

	properties, ok := params["properties"].(map[string]interface{})
	require.True(t, ok)

	action, ok := properties["action"].(map[string]interface{})
	require.True(t, ok)

	actionEnum, ok := action["enum"].([]string)
	require.True(t, ok)

	assert.Contains(t, actionEnum, "zoom")
}

func TestComputer20251124_WithoutZoom(t *testing.T) {
	tool := Computer20251124(Computer20251124Args{
		DisplayWidthPx:  1920,
		DisplayHeightPx: 1080,
		EnableZoom:      false,
	})

	// Verify schema does NOT include zoom action
	params, ok := tool.Parameters.(map[string]interface{})
	require.True(t, ok)

	properties, ok := params["properties"].(map[string]interface{})
	require.True(t, ok)

	action, ok := properties["action"].(map[string]interface{})
	require.True(t, ok)

	actionEnum, ok := action["enum"].([]string)
	require.True(t, ok)

	assert.NotContains(t, actionEnum, "zoom")
}

func TestComputer20251124_WithDisplayNumber(t *testing.T) {
	displayNum := 1
	tool := Computer20251124(Computer20251124Args{
		DisplayWidthPx:  1920,
		DisplayHeightPx: 1080,
		DisplayNumber:   &displayNum,
		EnableZoom:      false,
	})

	assert.Contains(t, tool.Description, "display 1")
}

func TestComputer20251124_SchemaValidation(t *testing.T) {
	tool := Computer20251124(Computer20251124Args{
		DisplayWidthPx:  1920,
		DisplayHeightPx: 1080,
	})

	params, ok := tool.Parameters.(map[string]interface{})
	require.True(t, ok)

	// Verify required fields
	required, ok := params["required"].([]string)
	require.True(t, ok)
	assert.Contains(t, required, "action")

	// Verify all expected properties exist
	properties, ok := params["properties"].(map[string]interface{})
	require.True(t, ok)

	expectedProps := []string{
		"action",
		"coordinate",
		"duration",
		"scroll_amount",
		"scroll_direction",
		"start_coordinate",
		"text",
	}

	for _, prop := range expectedProps {
		assert.Contains(t, properties, prop)
	}
}

func TestComputer20251124_AllActions(t *testing.T) {
	tool := Computer20251124(Computer20251124Args{
		DisplayWidthPx:  1920,
		DisplayHeightPx: 1080,
		EnableZoom:      true,
	})

	params, ok := tool.Parameters.(map[string]interface{})
	require.True(t, ok)

	properties, ok := params["properties"].(map[string]interface{})
	require.True(t, ok)

	action, ok := properties["action"].(map[string]interface{})
	require.True(t, ok)

	actionEnum, ok := action["enum"].([]string)
	require.True(t, ok)

	expectedActions := []string{
		"key", "hold_key", "type", "cursor_position",
		"mouse_move", "left_mouse_down", "left_mouse_up",
		"left_click", "left_click_drag", "right_click",
		"middle_click", "double_click", "triple_click",
		"scroll", "wait", "screenshot", "zoom",
	}

	assert.ElementsMatch(t, expectedActions, actionEnum)
}
