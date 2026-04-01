package ai

import "github.com/digitallysavvy/go-ai/pkg/provider/types"

// ExperimentalFilterActiveTools filters a slice of tools to only include those whose names
// are in activeTools.
//
// Behaviour:
//   - If tools is nil, returns nil regardless of activeTools.
//   - If activeTools is nil, returns tools unchanged (all tools are active).
//   - Otherwise returns a new slice containing only tools whose Name appears in
//     activeTools.
//
// This mirrors the TypeScript SDK's experimental_filterActiveTools utility from
// packages/ai/src/generate-text/filter-active-tool.ts.
func ExperimentalFilterActiveTools(tools []types.Tool, activeTools []string) []types.Tool {
	if tools == nil {
		return nil
	}
	if activeTools == nil {
		return tools
	}

	active := make(map[string]struct{}, len(activeTools))
	for _, name := range activeTools {
		active[name] = struct{}{}
	}

	filtered := make([]types.Tool, 0, len(tools))
	for _, t := range tools {
		if _, ok := active[t.Name]; ok {
			filtered = append(filtered, t)
		}
	}
	return filtered
}
