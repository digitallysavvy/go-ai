package anthropic

import (
	"encoding/json"
	"testing"
)

// Ports amazon-bedrock-anthropic-fetch.test.ts's error-transform cases
// (ai@7.0.113).

func TestTransformErrorBody_FlatMessage(t *testing.T) {
	body := []byte(`{"message":"cache_control: Extra inputs are not permitted"}`)
	got := transformErrorBody(body)

	var parsed map[string]interface{}
	if err := json.Unmarshal(got, &parsed); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if parsed["type"] != "error" {
		t.Errorf("type = %v, want error", parsed["type"])
	}
	errObj, ok := parsed["error"].(map[string]interface{})
	if !ok {
		t.Fatalf("error = %#v, want a map", parsed["error"])
	}
	if errObj["type"] != "error" {
		t.Errorf("error.type = %v, want error", errObj["type"])
	}
	if errObj["message"] != "cache_control: Extra inputs are not permitted" {
		t.Errorf("error.message = %v", errObj["message"])
	}
}

func TestTransformErrorBody_ExtraFieldsIgnored(t *testing.T) {
	body := []byte(`{"message":"tools.0.custom.input_schema.type: Field required","someOtherField":"value"}`)
	got := transformErrorBody(body)

	var parsed struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(got, &parsed); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if parsed.Error.Message != "tools.0.custom.input_schema.type: Field required" {
		t.Errorf("error.message = %q", parsed.Error.Message)
	}
}

func TestTransformErrorBody_NoMessageFieldUsesRawText(t *testing.T) {
	body := []byte(`{"code":"ValidationException"}`)
	got := transformErrorBody(body)

	var parsed struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(got, &parsed); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if parsed.Error.Message != string(body) {
		t.Errorf("error.message = %q, want raw body %q", parsed.Error.Message, string(body))
	}
}

func TestTransformErrorBody_NonJSONUsesRawText(t *testing.T) {
	body := []byte("Internal Server Error")
	got := transformErrorBody(body)

	var parsed struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(got, &parsed); err != nil {
		t.Fatalf("output is not valid JSON: %v", err)
	}
	if parsed.Error.Message != "Internal Server Error" {
		t.Errorf("error.message = %q", parsed.Error.Message)
	}
}
