package fx

import (
	"encoding/base64"
	"strings"
	"testing"
)

// TS builds the brokered ChatGPT token payload with JSON.stringify, which does
// not HTML-escape the account id.
func TestCreateChatGPTSandboxAccessTokenDoesNotHTMLEscape(t *testing.T) {
	token := createChatGPTSandboxAccessToken("cred", "a&b<c>")
	parts := strings.Split(token, ".")
	payload, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		t.Fatal(err)
	}
	if got, want := string(payload), `{"https://api.openai.com/auth":{"chatgpt_account_id":"a&b<c>"}}`; got != want {
		t.Fatalf("payload = %s, want %s", got, want)
	}
}
