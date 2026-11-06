package cohere

import (
	"encoding/base64"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// convert-to-cohere-chat-prompt.test.ts "should decode base64 document
// content" (TS 7a93864932): a string data payload is base64, bytes and text
// are taken as given.
func TestR85provaCohereDocumentBase64(t *testing.T) {
	for _, text := range []string{"This is file content", "Résumé: café, 日本語"} {
		for name, fd := range map[string]types.FileData{
			"bytes":  {Type: types.FileDataTypeData, Data: []byte(text)},
			"base64": {Type: types.FileDataTypeData, DataString: base64.StdEncoding.EncodeToString([]byte(text))},
		} {
			got, err := cohereDocumentText(types.FileContent{FileData: fd, MediaType: "text/plain", Filename: "test.txt"})
			if err != nil || got != text {
				t.Errorf("%s %q: got %q, %v", name, text, got, err)
			}
		}
	}
	if got, _ := cohereDocumentText(types.FileContent{FileData: types.FileData{Type: types.FileDataTypeText, Text: "SGVsbG8="}}); got != "SGVsbG8=" {
		t.Errorf("text = %q", got)
	}
	if _, err := cohereDocumentText(types.FileContent{FileData: types.FileData{Type: types.FileDataTypeData, DataString: "not base64!"}}); err == nil {
		t.Error("invalid base64: want an error (TS atob throws)")
	}
}
