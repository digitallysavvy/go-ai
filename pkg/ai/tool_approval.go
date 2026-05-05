package ai

import (
	"context"
	"fmt"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

type ToolApprovalStatus = types.ToolApprovalStatus
type ToolApprovalResult = types.ToolApprovalResult
type ToolApprovalFunc = types.ToolApprovalFunc
type ToolApprovalValue = types.ToolApprovalValue
type ToolApprovalConfig = types.ToolApprovalConfig

const (
	ToolApprovalStatusNotApplicable = types.ToolApprovalStatusNotApplicable
	ToolApprovalStatusApproved      = types.ToolApprovalStatusApproved
	ToolApprovalStatusDenied        = types.ToolApprovalStatusDenied
	ToolApprovalStatusUserApproval  = types.ToolApprovalStatusUserApproval
)

func effectiveRuntimeContext(runtimeCtx, experimentalCtx interface{}) interface{} {
	if runtimeCtx != nil {
		return runtimeCtx
	}
	return experimentalCtx
}

func normalizeToolApprovalResult(status interface{}) types.ToolApprovalResult {
	switch v := status.(type) {
	case nil:
		return types.ToolApprovalResult{Status: types.ToolApprovalStatusNotApplicable}
	case types.ToolApprovalResult:
		return normalizeToolApprovalResult(&v)
	case *types.ToolApprovalResult:
		if v == nil {
			return types.ToolApprovalResult{Status: types.ToolApprovalStatusNotApplicable}
		}
		if v.Status == "" {
			v.Status = types.ToolApprovalStatusNotApplicable
		}
		return *v
	case types.ToolApprovalStatus:
		return types.ToolApprovalResult{Status: v}
	case string:
		return types.ToolApprovalResult{Status: types.ToolApprovalStatus(v)}
	default:
		return types.ToolApprovalResult{Status: types.ToolApprovalStatusNotApplicable}
	}
}

func normalizeToolApprovalStatus(status types.ToolApprovalStatus) types.ToolApprovalResult {
	if status == "" {
		status = types.ToolApprovalStatusNotApplicable
	}
	return types.ToolApprovalResult{Status: status}
}

func resolveToolApproval(
	ctx context.Context,
	call types.ToolCall,
	tools []types.Tool,
	messages []types.Message,
	runtimeCtx interface{},
	toolsCtx map[string]interface{},
	toolApproval interface{},
) types.ToolApprovalResult {
	if toolApproval != nil {
		switch v := toolApproval.(type) {
		case types.ToolApprovalFunc:
			return normalizeToolApprovalResult(v(call, tools, messages, runtimeCtx, toolsCtx))
		case map[string]interface{}:
			if value, ok := v[call.ToolName]; ok {
				if fn, ok := value.(types.ToolApprovalFunc); ok {
					return normalizeToolApprovalResult(fn(call, tools, messages, runtimeCtx, toolsCtx))
				}
				if status, ok := value.(types.ToolApprovalStatus); ok {
					return normalizeToolApprovalResult(status)
				}
				if status, ok := value.(string); ok {
					return normalizeToolApprovalResult(types.ToolApprovalStatus(status))
				}
			}
		case map[string]types.ToolApprovalValue:
			if value, ok := v[call.ToolName]; ok {
				switch x := value.(type) {
				case types.ToolApprovalFunc:
					return normalizeToolApprovalResult(x(call, tools, messages, runtimeCtx, toolsCtx))
				case types.ToolApprovalStatus:
					return normalizeToolApprovalResult(x)
				case string:
					return normalizeToolApprovalResult(types.ToolApprovalStatus(x))
				}
			}
		}
	}

	var tool *types.Tool
	for i := range tools {
		if tools[i].Name == call.ToolName {
			tool = &tools[i]
			break
		}
	}
	if tool == nil {
		return types.ToolApprovalResult{Status: types.ToolApprovalStatusNotApplicable}
	}

	switch v := tool.NeedsApproval.(type) {
	case nil:
		return types.ToolApprovalResult{Status: types.ToolApprovalStatusNotApplicable}
	case bool:
		if v {
			return types.ToolApprovalResult{Status: types.ToolApprovalStatusUserApproval}
		}
		return types.ToolApprovalResult{Status: types.ToolApprovalStatusNotApplicable}
	case types.NeedsApprovalFunc:
		ctxValue := toolsCtx[tool.Name]
		if tool.ContextSchema != nil && ctxValue != nil {
			if err := tool.ContextSchema.Validator().Validate(ctxValue); err != nil {
				return types.ToolApprovalResult{Status: types.ToolApprovalStatusDenied, Reason: strPtr(err.Error())}
			}
		}
		if v(ctx, call.Arguments) {
			return types.ToolApprovalResult{Status: types.ToolApprovalStatusUserApproval}
		}
		return types.ToolApprovalResult{Status: types.ToolApprovalStatusNotApplicable}
	case types.ToolApprovalStatus:
		return normalizeToolApprovalStatus(v)
	case string:
		return normalizeToolApprovalStatus(types.ToolApprovalStatus(v))
	}

	return types.ToolApprovalResult{Status: types.ToolApprovalStatusNotApplicable}
}

func validateToolContextFor(tool *types.Tool, toolName string, ctxValue interface{}) (interface{}, error) {
	if tool == nil || tool.ContextSchema == nil {
		return ctxValue, nil
	}
	if err := tool.ContextSchema.Validator().Validate(ctxValue); err != nil {
		return nil, fmt.Errorf("invalid tool context for %s: %w", toolName, err)
	}
	return ctxValue, nil
}

func strPtr(s string) *string {
	return &s
}

func mergeProviderMetadataMaps(primary, fallback map[string]interface{}) map[string]interface{} {
	if len(primary) == 0 && len(fallback) == 0 {
		return nil
	}
	merged := make(map[string]interface{}, len(primary)+len(fallback))
	for k, v := range fallback {
		merged[k] = v
	}
	for k, v := range primary {
		merged[k] = v
	}
	return merged
}
