package ai

import (
	"context"
	"fmt"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
)

type ToolApprovalStatus = types.ToolApprovalStatus
type ToolApprovalResult = types.ToolApprovalResult

// ToolApprovalFunc re-exports the deprecated types.ToolApprovalFunc so
// ai.ToolApprovalFunc remains valid for existing callers of this package's
// own (equally deprecated, for the same reason) exported alias.
type ToolApprovalFunc = types.ToolApprovalFunc //nolint:staticcheck
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

// resolveToolApproval decides whether a tool call needs approval. It fails
// closed: a configuration value or status it doesn't recognize is an error,
// which reaches the model as a tool error instead of running the tool.
func resolveToolApproval(
	ctx context.Context,
	call types.ToolCall,
	tools []types.Tool,
	messages []types.Message,
	runtimeCtx interface{},
	toolsCtx map[string]interface{},
	toolApproval interface{},
) (types.ToolApprovalResult, error) {
	result, err := resolveToolApprovalValue(ctx, call, tools, messages, runtimeCtx, toolsCtx, toolApproval)
	if err != nil {
		return types.ToolApprovalResult{}, err
	}
	switch result.Status {
	case types.ToolApprovalStatusNotApplicable, types.ToolApprovalStatusApproved,
		types.ToolApprovalStatusDenied, types.ToolApprovalStatusUserApproval:
		return result, nil
	}
	return types.ToolApprovalResult{}, fmt.Errorf("tool %q: unknown tool approval status %q (want %q, %q, %q or %q)",
		call.ToolName, result.Status, types.ToolApprovalStatusNotApplicable, types.ToolApprovalStatusApproved,
		types.ToolApprovalStatusDenied, types.ToolApprovalStatusUserApproval)
}

// namedApprovalFunc converts an unnamed function literal to the named
// approval function type with the same signature. Without it, a value such
// as ToolApproval: func(ctx context.Context, ...) bool {...} would match no
// case in the type switches below. Other values are returned unchanged.
func namedApprovalFunc(v interface{}) interface{} {
	switch fn := v.(type) {
	case func(context.Context, map[string]interface{}, types.ToolNeedsApprovalOptions) bool:
		return types.ToolNeedsApprovalFunc(fn)
	case func(context.Context, map[string]interface{}) bool:
		return types.NeedsApprovalFunc(fn) //nolint:staticcheck // legacy function type still accepted for backward compatibility
	case func(types.ToolApprovalOptions) types.ToolApprovalResult:
		return types.GenericToolApprovalFunc(fn)
	case func(map[string]interface{}, types.SingleToolApprovalOptions) types.ToolApprovalResult:
		return types.SingleToolApprovalFunc(fn)
	case func(types.ToolCall, []types.Tool, []types.Message, interface{}, map[string]interface{}) types.ToolApprovalResult:
		return types.ToolApprovalFunc(fn) //nolint:staticcheck // legacy function type still accepted for backward compatibility
	}
	return v
}

func resolveToolApprovalValue(
	ctx context.Context,
	call types.ToolCall,
	tools []types.Tool,
	messages []types.Message,
	runtimeCtx interface{},
	toolsCtx map[string]interface{},
	toolApproval interface{},
) (types.ToolApprovalResult, error) {
	tool := findToolForCall(call, tools)
	if toolApproval != nil {
		switch v := namedApprovalFunc(toolApproval).(type) {
		case types.GenericToolApprovalFunc:
			return normalizeToolApprovalResult(v(types.ToolApprovalOptions{
				ToolCall:       call,
				Tools:          tools,
				ToolsContext:   toolsCtx,
				RuntimeContext: runtimeCtx,
				Messages:       messages,
			})), nil
		case types.ToolApprovalFunc: //nolint:staticcheck // legacy function type still accepted for backward compatibility
			return normalizeToolApprovalResult(v(call, tools, messages, runtimeCtx, toolsCtx)), nil
		case map[string]interface{}:
			if value, ok := v[call.ToolName]; ok {
				return normalizePerToolApprovalValue(call, tool, tools, messages, value, runtimeCtx, toolsCtx)
			}
		case map[string]types.ToolApprovalValue:
			if value, ok := v[call.ToolName]; ok {
				return normalizePerToolApprovalValue(call, tool, tools, messages, value, runtimeCtx, toolsCtx)
			}
		default:
			return types.ToolApprovalResult{}, fmt.Errorf("unsupported ToolApproval option of type %T: use a GenericToolApprovalFunc or a map of tool name to approval value", toolApproval)
		}
	}

	if tool == nil {
		return types.ToolApprovalResult{Status: types.ToolApprovalStatusNotApplicable}, nil
	}

	setting := tool.ToolApproval
	if setting == nil {
		setting = tool.NeedsApproval
	}
	switch v := namedApprovalFunc(setting).(type) {
	case nil:
		return types.ToolApprovalResult{Status: types.ToolApprovalStatusNotApplicable}, nil
	case bool:
		if v {
			return types.ToolApprovalResult{Status: types.ToolApprovalStatusUserApproval}, nil
		}
		return types.ToolApprovalResult{Status: types.ToolApprovalStatusNotApplicable}, nil
	case types.ToolNeedsApprovalFunc:
		toolCtx, err := validateToolContextFor(tool, call.ToolName, toolsCtx[tool.Name])
		if err != nil {
			return types.ToolApprovalResult{}, err
		}
		if v(ctx, call.Arguments, types.ToolNeedsApprovalOptions{
			ToolCallID: call.ID,
			Messages:   messages,
			Context:    toolCtx,
		}) {
			return types.ToolApprovalResult{Status: types.ToolApprovalStatusUserApproval}, nil
		}
		return types.ToolApprovalResult{Status: types.ToolApprovalStatusNotApplicable}, nil
	case types.NeedsApprovalFunc: //nolint:staticcheck // legacy function type still accepted for backward compatibility
		if _, err := validateToolContextFor(tool, call.ToolName, toolsCtx[tool.Name]); err != nil {
			return types.ToolApprovalResult{}, err
		}
		if v(ctx, call.Arguments) {
			return types.ToolApprovalResult{Status: types.ToolApprovalStatusUserApproval}, nil
		}
		return types.ToolApprovalResult{Status: types.ToolApprovalStatusNotApplicable}, nil
	case types.ToolApprovalStatus:
		return normalizeToolApprovalStatus(v), nil
	case string:
		return normalizeToolApprovalStatus(types.ToolApprovalStatus(v)), nil
	}

	return types.ToolApprovalResult{}, fmt.Errorf("tool %q: unsupported ToolApproval value of type %T: use a bool, a ToolApprovalStatus or a ToolNeedsApprovalFunc", call.ToolName, setting)
}

func findToolForCall(call types.ToolCall, tools []types.Tool) *types.Tool {
	for i := range tools {
		if tools[i].Name == call.ToolName {
			return &tools[i]
		}
	}
	return nil
}

func normalizePerToolApprovalValue(
	call types.ToolCall,
	tool *types.Tool,
	tools []types.Tool,
	messages []types.Message,
	value interface{},
	runtimeCtx interface{},
	toolsCtx map[string]interface{},
) (types.ToolApprovalResult, error) {
	switch fn := namedApprovalFunc(value).(type) {
	case types.SingleToolApprovalFunc:
		toolCtx, err := validateToolContextFor(tool, call.ToolName, toolsCtx[call.ToolName])
		if err != nil {
			return types.ToolApprovalResult{}, err
		}
		return normalizeToolApprovalResult(fn(call.Arguments, types.SingleToolApprovalOptions{
			ToolContext:    toolCtx,
			RuntimeContext: runtimeCtx,
			ToolCallID:     call.ID,
			Messages:       messages,
		})), nil
	case types.GenericToolApprovalFunc:
		return normalizeToolApprovalResult(fn(types.ToolApprovalOptions{
			ToolCall:       call,
			Tools:          tools,
			ToolsContext:   toolsCtx,
			RuntimeContext: runtimeCtx,
			Messages:       messages,
		})), nil
	case types.ToolApprovalFunc: //nolint:staticcheck // legacy function type still accepted for backward compatibility
		return normalizeToolApprovalResult(fn(call, tools, messages, runtimeCtx, toolsCtx)), nil
	case types.ToolApprovalStatus:
		return normalizeToolApprovalResult(fn), nil
	case string:
		return normalizeToolApprovalResult(types.ToolApprovalStatus(fn)), nil
	case nil, types.ToolApprovalResult, *types.ToolApprovalResult:
		return normalizeToolApprovalResult(value), nil
	default:
		return types.ToolApprovalResult{}, fmt.Errorf("tool %q: unsupported tool approval value of type %T", call.ToolName, value)
	}
}

func validateToolContextFor(tool *types.Tool, toolName string, ctxValue interface{}) (interface{}, error) {
	if tool == nil || tool.ContextSchema == nil {
		return ctxValue, nil
	}
	normalized := schema.ApplyDefaults(ctxValue, tool.ContextSchema)
	if err := tool.ContextSchema.Validator().Validate(normalized); err != nil {
		return nil, providererrors.NewValidationErrorWithContext(
			normalized,
			fmt.Sprintf("invalid tool context for %s: %v", toolName, err),
			err,
			&providererrors.ValidationContext{Field: "tool context", EntityName: toolName},
		)
	}
	return normalized, nil
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
