package bedrock

import "testing"

// TS createJsonErrorResponseHandler parses response.text() (BOM dropped).
func TestR86provaBedrockErrorBodyBOM(t *testing.T) {
	body := []byte("\xEF\xBB\xBF{\"message\":\"boom\",\"type\":\"ValidationException\"}")
	if pe := bedrockAPIError(400, body, nil); pe.Message != "ValidationException: boom" || pe.ResponseBody != string(body[3:]) {
		t.Fatalf("shared: %#v", pe)
	}
	if pe := bedrockConverseGenerateAPIError(400, body, nil); pe.Message != "boom" || pe.ResponseBody != string(body[3:]) {
		t.Fatalf("converse: %#v", pe)
	}
}
