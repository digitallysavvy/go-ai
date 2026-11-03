package gemini

import "testing"

// TS googleFailedResponseHandler parses response.text() (BOM dropped).
func TestR86provaGoogleHTTPErrorBOM(t *testing.T) {
	body := []byte("\xEF\xBB\xBF{\"error\":{\"code\":400,\"message\":\"boom\",\"status\":\"INVALID_ARGUMENT\"}}")
	pe := GoogleHTTPError("google", 400, nil, body, nil)
	if pe.Message != "boom" || pe.ErrorCode != "INVALID_ARGUMENT" || pe.ResponseBody != string(body[3:]) {
		t.Fatalf("%#v", pe)
	}
}
