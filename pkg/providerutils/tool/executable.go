package tool

import "github.com/digitallysavvy/go-ai/pkg/provider/types"

// IsExecutableTool reports whether tool has a local Execute function.
func IsExecutableTool(tool *types.Tool) bool {
	return tool != nil && tool.Execute != nil
}

// RequireExecutableTool returns the tool and true when it has a local Execute function.
func RequireExecutableTool(tool *types.Tool) (*types.Tool, bool) {
	if !IsExecutableTool(tool) {
		return nil, false
	}
	return tool, true
}
