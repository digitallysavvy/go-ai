package policy

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// DefaultOPAInput is the default input passed to OPA tool-call rules.
type DefaultOPAInput struct {
	Tool           map[string]string `json:"tool"`
	Args           any               `json:"args,omitempty"`
	Messages       []types.Message   `json:"messages"`
	RuntimeContext any               `json:"runtimeContext,omitempty"`
}

// OPAPolicyOptions configures OPAPolicy.
type OPAPolicyOptions struct {
	Path    string
	ToInput func(types.ToolApprovalOptions) any
}

// OptionalOPAPolicyOptions configures OptionalOPAPolicy.
type OptionalOPAPolicyOptions struct {
	OPAPolicyOptions
}

// OPAPolicy constructs a generic tool approval function backed by OPA.
func OPAPolicy(client PolicyClient, opts OPAPolicyOptions) types.GenericToolApprovalFunc {
	return func(approval types.ToolApprovalOptions) types.ToolApprovalResult {
		if client == nil {
			return denied(fmt.Errorf("policy client is nil"))
		}
		result, err := client.Evaluate(context.Background(), opts.Path, opaInput(approval, opts.ToInput))
		if err != nil {
			return denied(err)
		}
		decision := NormalizeOPADecision(result)
		return approvalFromDecision(decision)
	}
}

// OptionalOPAPolicy returns nil when no client is supplied, matching the TS
// helper's default allow-all behavior when policy is not configured.
func OptionalOPAPolicy(client PolicyClient, opts OptionalOPAPolicyOptions) types.ToolApprovalConfig {
	if client == nil {
		return nil
	}
	return OPAPolicy(client, opts.OPAPolicyOptions)
}

func opaInput(approval types.ToolApprovalOptions, mapper func(types.ToolApprovalOptions) any) any {
	if mapper != nil {
		if input := mapper(approval); input != nil {
			return input
		}
	}
	messages := approval.Messages
	if messages == nil {
		messages = []types.Message{}
	}
	return DefaultOPAInput{
		Tool:           map[string]string{"name": approval.ToolCall.ToolName},
		Args:           approval.ToolCall.Arguments,
		Messages:       messages,
		RuntimeContext: approval.RuntimeContext,
	}
}

func denied(err error) types.ToolApprovalResult {
	reason := "policy evaluation failed: " + errorMessage(err)
	return types.ToolApprovalResult{Status: types.ToolApprovalStatusDenied, Reason: &reason}
}

func errorMessage(cause any) string {
	switch v := cause.(type) {
	case nil:
		return "<nil>"
	case error:
		return v.Error()
	case string:
		return v
	default:
		if data, err := json.Marshal(v); err == nil {
			return string(data)
		}
		return fmt.Sprint(v)
	}
}
