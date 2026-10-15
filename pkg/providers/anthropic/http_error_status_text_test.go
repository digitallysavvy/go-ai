package anthropic

import "testing"

// TS createJsonErrorResponseHandler: an empty or unparseable error body gives
// the response status text; the body stays on ResponseBody.
func TestAnthropicHTTPErrorUnparseableBodyUsesStatusText(t *testing.T) {
	err := anthropicHTTPError("anthropic", 502, nil, []byte("<html>bad gateway</html>"), nil)
	if err.Message != "Bad Gateway" || err.ResponseBody != "<html>bad gateway</html>" {
		t.Fatalf("message %q body %q", err.Message, err.ResponseBody)
	}
	ok := anthropicHTTPError("anthropic", 400, nil, []byte(`{"type":"error","error":{"type":"invalid_request_error","message":"bad"}}`), nil)
	if ok.Message != "bad" {
		t.Fatalf("message %q", ok.Message)
	}
}
