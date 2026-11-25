package errors

import "testing"

// TS createGatewayErrorFromResponse validates error.param with zod
// (modelNotFoundParamSchema / forbiddenParamSchema), whose object keys match
// exactly: a "ModelId" or "RULEID" key is unknown, so modelId / ruleId stay
// undefined.
func TestR97provgErrorParamKeysMatchExactly(t *testing.T) {
	body := []byte(`{"error":{"message":"m","type":"model_not_found","param":{"ModelId":"x"}}}`)
	err := CreateGatewayErrorFromResponse(body, 404, "Gateway request failed", nil, "")
	mnf, ok := err.(*GatewayModelNotFoundError)
	if !ok {
		t.Fatalf("got %T, want *GatewayModelNotFoundError", err)
	}
	if mnf.ModelID != "" {
		t.Fatalf("ModelID = %q, want empty (TS modelId undefined)", mnf.ModelID)
	}

	body = []byte(`{"error":{"message":"m","type":"forbidden","param":{"RULEID":"r1"}}}`)
	err = CreateGatewayErrorFromResponse(body, 403, "Gateway request failed", nil, "")
	fe, ok := err.(*GatewayForbiddenError)
	if !ok {
		t.Fatalf("got %T, want *GatewayForbiddenError", err)
	}
	if fe.RuleID != "" {
		t.Fatalf("RuleID = %q, want empty (TS ruleId undefined)", fe.RuleID)
	}

	// The exact keys still fill the fields.
	body = []byte(`{"error":{"message":"m","type":"model_not_found","param":{"modelId":"x"}}}`)
	if got := CreateGatewayErrorFromResponse(body, 404, "", nil, "").(*GatewayModelNotFoundError).ModelID; got != "x" {
		t.Fatalf("ModelID = %q, want x", got)
	}
	body = []byte(`{"error":{"message":"m","type":"forbidden","param":{"ruleId":"r1"}}}`)
	if got := CreateGatewayErrorFromResponse(body, 403, "", nil, "").(*GatewayForbiddenError).RuleID; got != "r1" {
		t.Fatalf("RuleID = %q, want r1", got)
	}
}
