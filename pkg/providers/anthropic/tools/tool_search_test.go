package tools

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestToolSearchBm2520251119_Basic(t *testing.T) {
	tool := ToolSearchBm2520251119()

	assert.Equal(t, "anthropic.tool_search_bm25_20251119", tool.Name)
	assert.NotEmpty(t, tool.Description)
	assert.True(t, tool.ProviderExecuted)
	assert.NotNil(t, tool.Parameters)
	assert.NotNil(t, tool.Execute)
}

func TestToolSearchBm2520251119_Schema(t *testing.T) {
	tool := ToolSearchBm2520251119()

	params, ok := tool.Parameters.(map[string]interface{})
	require.True(t, ok)

	// Verify required fields
	required, ok := params["required"].([]string)
	require.True(t, ok)
	assert.Contains(t, required, "query")

	// Verify properties
	properties, ok := params["properties"].(map[string]interface{})
	require.True(t, ok)

	// Check query property
	query, ok := properties["query"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "string", query["type"])

	// Check limit property
	limit, ok := properties["limit"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "integer", limit["type"])
}

func TestToolSearchBm2520251119_Description(t *testing.T) {
	tool := ToolSearchBm2520251119()

	assert.Contains(t, tool.Description, "BM25")
	assert.Contains(t, tool.Description, "natural language")
	assert.Contains(t, tool.Description, "on-demand")
	assert.Contains(t, tool.Description, "deferLoading")
}

func TestToolSearchRegex20251119_Basic(t *testing.T) {
	tool := ToolSearchRegex20251119()

	assert.Equal(t, "anthropic.tool_search_regex_20251119", tool.Name)
	assert.NotEmpty(t, tool.Description)
	assert.True(t, tool.ProviderExecuted)
	assert.NotNil(t, tool.Parameters)
	assert.NotNil(t, tool.Execute)
}

func TestToolSearchRegex20251119_Schema(t *testing.T) {
	tool := ToolSearchRegex20251119()

	params, ok := tool.Parameters.(map[string]interface{})
	require.True(t, ok)

	// Verify required fields
	required, ok := params["required"].([]string)
	require.True(t, ok)
	assert.Contains(t, required, "pattern")

	// Verify properties
	properties, ok := params["properties"].(map[string]interface{})
	require.True(t, ok)

	// Check pattern property
	pattern, ok := properties["pattern"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "string", pattern["type"])
	// maxLength can be either int or float64 depending on how it's serialized
	maxLength := pattern["maxLength"]
	assert.NotNil(t, maxLength)
	// Check the value is 200 regardless of type
	switch v := maxLength.(type) {
	case int:
		assert.Equal(t, 200, v)
	case float64:
		assert.Equal(t, float64(200), v)
	default:
		t.Errorf("maxLength should be int or float64, got %T", v)
	}

	// Check limit property
	limit, ok := properties["limit"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "integer", limit["type"])
}

func TestToolSearchRegex20251119_Description(t *testing.T) {
	tool := ToolSearchRegex20251119()

	assert.Contains(t, tool.Description, "regex")
	assert.Contains(t, tool.Description, "Python")
	assert.Contains(t, tool.Description, "re.search()")
	assert.Contains(t, tool.Description, "200 characters")
	assert.Contains(t, tool.Description, "deferLoading")
}

func TestToolSearchRegex20251119_PatternExamples(t *testing.T) {
	tool := ToolSearchRegex20251119()

	params, ok := tool.Parameters.(map[string]interface{})
	require.True(t, ok)

	properties, ok := params["properties"].(map[string]interface{})
	require.True(t, ok)

	pattern, ok := properties["pattern"].(map[string]interface{})
	require.True(t, ok)

	description, ok := pattern["description"].(string)
	require.True(t, ok)

	// Verify examples are in the description
	assert.Contains(t, description, "weather")
	assert.Contains(t, description, "get_.*_data")
	assert.Contains(t, description, "(?i)slack")
}
