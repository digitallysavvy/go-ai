package ai

import (
	"context"
	"sort"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func resolveStepTools(ctx context.Context, tools []types.Tool, toolsContext map[string]interface{}, sandbox interface{}) []types.Tool {
	if len(tools) == 0 {
		return tools
	}
	resolved := make([]types.Tool, len(tools))
	copy(resolved, tools)
	for i := range resolved {
		if resolved[i].DescriptionFunc != nil {
			resolved[i].Description = resolved[i].DescriptionFunc(ctx, types.ToolDescriptionOptions{
				Context:             toolsContext[resolved[i].Name],
				ExperimentalSandbox: sandbox,
			})
			resolved[i].DescriptionFunc = nil
		}
	}
	return resolved
}

func orderStepTools(tools []types.Tool, toolOrder []string) []types.Tool {
	if toolOrder == nil {
		return tools
	}
	byName := make(map[string]types.Tool, len(tools))
	for _, tool := range tools {
		byName[tool.Name] = tool
	}

	out := make([]types.Tool, 0, len(tools))
	used := make(map[string]bool, len(tools))
	for _, name := range toolOrder {
		if used[name] {
			continue
		}
		if tool, ok := byName[name]; ok {
			out = append(out, tool)
			used[name] = true
		}
	}

	remaining := make([]types.Tool, 0, len(tools)-len(out))
	for _, tool := range tools {
		if !used[tool.Name] {
			remaining = append(remaining, tool)
		}
	}
	sort.SliceStable(remaining, func(i, j int) bool {
		return remaining[i].Name < remaining[j].Name
	})
	return append(out, remaining...)
}

func enrichToolCallMetadata(calls []types.ToolCall, tools []types.Tool) []types.ToolCall {
	if len(calls) == 0 || len(tools) == 0 {
		return calls
	}
	byName := make(map[string]types.Tool, len(tools))
	for _, tool := range tools {
		byName[tool.Name] = tool
	}
	out := make([]types.ToolCall, len(calls))
	copy(out, calls)
	for i := range out {
		if tool, ok := byName[out[i].ToolName]; ok {
			if out[i].Title == "" {
				out[i].Title = tool.Title
			}
			if out[i].ToolMetadata == nil && tool.Metadata != nil {
				out[i].ToolMetadata = cloneStringAnyMap(tool.Metadata)
			}
		}
	}
	return out
}

func cloneStringAnyMap(in map[string]interface{}) map[string]interface{} {
	if in == nil {
		return nil
	}
	out := make(map[string]interface{}, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
