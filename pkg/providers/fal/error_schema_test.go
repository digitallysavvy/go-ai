package fal

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

func TestParseFalErrorMessageFollowsTSSchema(t *testing.T) {
	for _, c := range []struct{ name, body, want string }{
		{"schema", `{"error":{"message":"bad","code":1}}`, "bad"},
		{"empty message kept", `{"error":{"message":"","code":1}}`, ""},
		{"missing code", `{"error":{"message":"bad"}}`, "Bad Request"},
		{"detail string", `{"detail":"x"}`, "Bad Request"},
		{"plain text", `oops`, "Bad Request"},
	} {
		if got := parseFalErrorMessage([]byte(c.body), "Bad Request"); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

func TestFalImageErrorMessageFollowsTSUnion(t *testing.T) {
	for _, c := range []struct{ name, body, want string }{
		{"validation", `{"detail":[{"loc":["body","prompt"],"msg":"required","type":"missing"},{"loc":["a"],"msg":"bad","type":"t"}]}`, "body.prompt: required\na: bad"},
		{"message", `{"message":"quota"}`, "quota"},
		{"validation missing type falls to message", `{"detail":[{"loc":["a"],"msg":"bad"}],"message":"m"}`, "m"},
		{"validation missing type without message", `{"detail":[{"loc":["a"],"msg":"bad"}]}`, "Bad Request"},
		{"non string loc", `{"detail":[{"loc":[1],"msg":"bad","type":"t"}]}`, "Bad Request"},
		{"error string is off schema", `{"error":"x"}`, "Bad Request"},
		{"plain text", `oops`, "Bad Request"},
	} {
		if got := falImageErrorMessage(400, []byte(c.body)); got != c.want {
			t.Errorf("%s: got %q, want %q", c.name, got, c.want)
		}
	}
}

// zod .nullish().default(x) keeps an explicit null: language is sent as null,
// version and batchSize (`?? undefined`) are omitted, and a null chunkLevel is
// falsy so the base 'word' stays.
func TestTranscriptionExplicitNullOptionsFollowTS(t *testing.T) {
	m := &TranscriptionModel{}
	body, err := m.buildRequestBody(&provider.TranscriptionOptions{ProviderOptions: map[string]interface{}{
		"fal": map[string]interface{}{"language": nil, "version": nil, "batchSize": nil, "chunkLevel": nil},
	}})
	if err != nil {
		t.Fatal(err)
	}
	if v, present := body["language"]; !present || v != nil {
		t.Errorf("language = %v (present %v), want explicit null", v, present)
	}
	if _, present := body["version"]; present {
		t.Error("version must be omitted")
	}
	if _, present := body["batch_size"]; present {
		t.Error("batch_size must be omitted")
	}
	if body["chunk_level"] != "word" {
		t.Errorf("chunk_level = %v, want word", body["chunk_level"])
	}
	// Absent keys still take the zod defaults.
	body, _ = m.buildRequestBody(&provider.TranscriptionOptions{ProviderOptions: map[string]interface{}{"fal": map[string]interface{}{}}})
	if body["language"] != "en" || body["version"] != "3" || body["batch_size"] != float64(64) || body["chunk_level"] != "segment" {
		t.Errorf("defaults = %v", body)
	}
}
