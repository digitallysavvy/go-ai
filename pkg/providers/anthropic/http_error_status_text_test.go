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

// TS anthropicErrorDataSchema requires {type:'error', error:{type, message}};
// any other JSON body gives the status text.
func TestAnthropicHTTPErrorSchemaIsStrict(t *testing.T) {
	for name, body := range map[string]string{
		"no top-level type":    `{"error":{"type":"x","message":"boom"}}`,
		"wrong top-level type": `{"type":"oops","error":{"type":"x","message":"boom"}}`,
		"missing message":      `{"type":"error","error":{"type":"overloaded_error"}}`,
		"vertex shape":         `{"error":{"code":400,"message":"boom","status":"INVALID_ARGUMENT"}}`,
	} {
		err := anthropicHTTPError("anthropic", 400, nil, []byte(body), nil)
		if err.Message != "Bad Request" {
			t.Errorf("%s: message %q, want status text", name, err.Message)
		}
	}
}
