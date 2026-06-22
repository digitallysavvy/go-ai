package ai

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func TestToolApprovalSignatureVerifiesAndRejectsTampering(t *testing.T) {
	secret := []byte("test-secret-key-for-hmac-signing")
	input := map[string]interface{}{"path": "/tmp/cache", "mode": "delete"}

	signature, err := SignToolApproval(secret, "approval-1", "call-1", "deleteFile", input)
	if err != nil {
		t.Fatalf("SignToolApproval error = %v", err)
	}
	if signature == "" || strings.ContainsAny(signature, "+/=") {
		t.Fatalf("signature = %q, want non-empty base64url without padding", signature)
	}

	valid, err := VerifyToolApprovalSignature(secret, signature, "approval-1", "call-1", "deleteFile", map[string]interface{}{"mode": "delete", "path": "/tmp/cache"})
	if err != nil || !valid {
		t.Fatalf("VerifyToolApprovalSignature valid = %v, err = %v", valid, err)
	}

	cases := []struct {
		name       string
		approvalID string
		toolCallID string
		toolName   string
		input      interface{}
		secret     []byte
	}{
		{name: "approval id", approvalID: "tampered", toolCallID: "call-1", toolName: "deleteFile", input: input, secret: secret},
		{name: "tool call id", approvalID: "approval-1", toolCallID: "tampered", toolName: "deleteFile", input: input, secret: secret},
		{name: "tool name", approvalID: "approval-1", toolCallID: "call-1", toolName: "readFile", input: input, secret: secret},
		{name: "input", approvalID: "approval-1", toolCallID: "call-1", toolName: "deleteFile", input: map[string]interface{}{"path": "/app/.env"}, secret: secret},
		{name: "secret", approvalID: "approval-1", toolCallID: "call-1", toolName: "deleteFile", input: input, secret: []byte("different")},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			valid, err := VerifyToolApprovalSignature(tc.secret, signature, tc.approvalID, tc.toolCallID, tc.toolName, tc.input)
			if err != nil {
				t.Fatalf("VerifyToolApprovalSignature error = %v", err)
			}
			if valid {
				t.Fatal("expected tampered approval to be rejected")
			}
		})
	}
}

func TestVerifyToolApprovalSignatureReturnsErrorForMalformedSignature(t *testing.T) {
	valid, err := VerifyToolApprovalSignature(
		[]byte("secret"),
		"not base64url!",
		"approval-1",
		"call-1",
		"deleteFile",
		map[string]interface{}{"path": "/tmp/cache"},
	)
	if err == nil {
		t.Fatal("expected malformed base64url signature to return an error like TS atob rejection")
	}
	if valid {
		t.Fatal("malformed signature verified")
	}
}

func TestInvalidToolApprovalSignatureErrorMatchesTSMessageAndHelper(t *testing.T) {
	err := &InvalidToolApprovalSignatureError{
		ApprovalID: "approval-1",
		ToolCallID: "call-1",
		Reason:     "missing signature",
	}
	want := `Tool approval signature verification failed for approval "approval-1" (tool call "call-1"): missing signature`
	if err.Error() != want {
		t.Fatalf("Error() = %q, want %q", err.Error(), want)
	}
	if !IsInvalidToolApprovalSignatureError(err) {
		t.Fatal("IsInvalidToolApprovalSignatureError returned false for direct error")
	}
	if !IsInvalidToolApprovalSignatureError(fmt.Errorf("wrapped: %w", err)) {
		t.Fatal("IsInvalidToolApprovalSignatureError returned false for wrapped error")
	}
	if IsInvalidToolApprovalSignatureError(fmt.Errorf("other")) {
		t.Fatal("IsInvalidToolApprovalSignatureError returned true for unrelated error")
	}
}

func TestToolResultsToContentPartsSignsApprovalRequest(t *testing.T) {
	parts := toolResultsToContentParts([]types.ToolResult{{
		ToolCallID:     "call-1",
		ToolName:       "deleteFile",
		Input:          map[string]interface{}{"path": "/tmp/cache"},
		ApprovalStatus: types.ToolApprovalStatusUserApproval,
	}}, []byte("secret"))
	if len(parts) != 1 {
		t.Fatalf("parts len = %d, want 1", len(parts))
	}
	req, ok := parts[0].(types.ToolApprovalRequestContent)
	if !ok {
		t.Fatalf("part = %T, want ToolApprovalRequestContent", parts[0])
	}
	if req.Signature == "" {
		t.Fatal("expected signed approval request")
	}
	valid, err := VerifyToolApprovalSignature([]byte("secret"), req.Signature, req.ApprovalID, req.ToolCallID, "deleteFile", map[string]interface{}{"path": "/tmp/cache"})
	if err != nil || !valid {
		t.Fatalf("signature valid = %v, err = %v", valid, err)
	}
	data, err := json.Marshal(req)
	if err != nil {
		t.Fatalf("marshal request: %v", err)
	}
	if !strings.Contains(string(data), `"signature"`) {
		t.Fatalf("marshaled approval request missing signature: %s", data)
	}
}

func TestToolResultsToContentPartsSignsWithConfiguredEmptySecret(t *testing.T) {
	parts := toolResultsToContentParts([]types.ToolResult{{
		ToolCallID:     "call-1",
		ApprovalID:     "approval-1",
		ToolName:       "deleteFile",
		Input:          map[string]interface{}{"path": "/tmp/cache"},
		ApprovalStatus: types.ToolApprovalStatusUserApproval,
	}}, []byte{})
	req, ok := parts[0].(types.ToolApprovalRequestContent)
	if !ok {
		t.Fatalf("part = %T, want ToolApprovalRequestContent", parts[0])
	}
	if req.Signature == "" {
		t.Fatal("expected empty-but-configured secret to sign approval request")
	}
	valid, err := VerifyToolApprovalSignature([]byte{}, req.Signature, "approval-1", "call-1", "deleteFile", map[string]interface{}{"path": "/tmp/cache"})
	if err != nil || !valid {
		t.Fatalf("signature valid = %v, err = %v", valid, err)
	}

	parts = toolResultsToContentParts([]types.ToolResult{{
		ToolCallID:     "call-1",
		ApprovalID:     "approval-1",
		ToolName:       "deleteFile",
		Input:          map[string]interface{}{"path": "/tmp/cache"},
		ApprovalStatus: types.ToolApprovalStatusUserApproval,
	}}, nil)
	req, ok = parts[0].(types.ToolApprovalRequestContent)
	if !ok {
		t.Fatalf("part = %T, want ToolApprovalRequestContent", parts[0])
	}
	if req.Signature != "" {
		t.Fatalf("nil secret signed approval request: %+v", req)
	}
}
