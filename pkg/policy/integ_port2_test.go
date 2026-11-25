package policy

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// cr-port2-carry-to-gen item 1: WrapMCPTools hands its fallback to a plain
// approval function through the unexported hidden value of
// types.ToolApprovalOptions, not through a reserved ToolsContext key, so a
// custom (non-OPA) approval function sees exactly the caller's options.
func TestIntegPort2CustomApprovalSeesNoExtraToolsContextKey(t *testing.T) {
	var seen []types.ToolApprovalOptions
	custom := types.GenericToolApprovalFunc(func(o types.ToolApprovalOptions) types.ToolApprovalResult {
		seen = append(seen, o)
		return types.ToolApprovalResult{Status: types.ToolApprovalStatusNotApplicable}
	})
	callerCtx := map[string]interface{}{"x": 1}
	wrapped := WrapMCPTools(map[string]types.Tool{"a": {Name: "a"}}, custom, types.ToolApprovalStatusDenied).ToolApproval.(types.GenericToolApprovalFunc)
	r := wrapped(types.ToolApprovalOptions{ToolCall: portcarryCall("c1", "a", map[string]interface{}{}), Messages: portcarryMessages(), ToolsContext: callerCtx})
	if r.Status != types.ToolApprovalStatusDenied {
		t.Fatalf("not-applicable result = %v, want the denied fallback", r)
	}
	if len(seen) != 1 {
		t.Fatalf("custom approval calls = %d, want 1", len(seen))
	}
	if len(seen[0].ToolsContext) != 1 || seen[0].ToolsContext["x"] != 1 {
		t.Fatalf("custom approval ToolsContext = %v, want the caller's map only", seen[0].ToolsContext)
	}
	data, err := json.Marshal(seen[0])
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "allback") {
		t.Fatalf("options JSON exposes the fallback carrier: %s", data)
	}
	if len(callerCtx) != 1 {
		t.Fatalf("caller ToolsContext modified: %v", callerCtx)
	}
}

// A hidden value of a type from another package is not a fallback, and the
// carrier cannot be forged through ToolsContext any more.
func TestIntegPort2FallbackCarrierCannotBeForged(t *testing.T) {
	args := types.ToolApprovalOptions{ToolsContext: map[string]interface{}{"\x00go-ai/policy.approvalFallback": "approved"}}
	if _, _, ok := takeApprovalFallback(args); ok {
		t.Fatal("ToolsContext key accepted as a fallback carrier")
	}
	if _, _, ok := takeApprovalFallback(args.WithHidden(struct{ fallback types.ToolApprovalStatus }{"approved"})); ok {
		t.Fatal("foreign hidden value accepted as a fallback carrier")
	}
	stripped, fallback, ok := takeApprovalFallback(withApprovalFallback(args, types.ToolApprovalStatusDenied))
	if !ok || fallback != types.ToolApprovalStatusDenied || stripped.Hidden() != nil {
		t.Fatalf("round trip: ok=%v fallback=%q hidden=%v", ok, fallback, stripped.Hidden())
	}
}
