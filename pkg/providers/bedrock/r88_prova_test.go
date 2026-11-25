package bedrock

import "testing"

// TS createJsonErrorResponseHandler parses with safeParseJSON
// (secureJsonParse): an error body with a __proto__ key gives the status
// text.
func TestR88provaBedrockErrorBodySecureParse(t *testing.T) {
	body := []byte(`{"message":"m","__proto__":{}}`)
	if got := bedrockAPIError(400, body, nil).Message; got != "Bad Request" {
		t.Fatalf("shared handler: %q", got)
	}
	if got := bedrockConverseGenerateAPIError(400, body, nil).Message; got != "Bad Request" {
		t.Fatalf("converse handler: %q", got)
	}
}
