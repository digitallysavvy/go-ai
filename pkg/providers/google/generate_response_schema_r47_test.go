package google

import (
	"context"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/internal/geminischema"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS doGenerate parses with responseSchema, which has no `error` key (only the
// streaming chunkSchema does), so a stray `error` of any shape is stripped and
// the body still succeeds.
func TestDoGenerateIgnoresErrorKeyLikeResponseSchema(t *testing.T) {
	srv, _ := scriptedServer(t, scriptedReply{body: `{"error":"oops","candidates":[{"content":{"parts":[{"text":"hi"}],"role":"model"},"finishReason":"STOP"}]}`})
	res, err := longDecodeModel(srv).DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	if res.Text != "hi" {
		t.Fatalf("text = %q", res.Text)
	}
}

// responseSchema content is getContentSchema().nullish().or(z.object({}).strict()).
func TestGenerateResponseSchemaContentUnion(t *testing.T) {
	if msg := geminischema.GenerateValidationMessage(`{"error":5,"candidates":[{"content":{}}]}`); msg != "" {
		t.Fatalf("valid body rejected: %s", msg)
	}
	if msg := geminischema.ValidationMessage(`{"error":5}`); msg == "" {
		t.Fatal("chunkSchema must still check an in-stream error frame")
	}
	msg := geminischema.GenerateValidationMessage(`{"candidates":[{"content":{"parts":"x"}}]}`)
	for _, want := range []string{`"code": "invalid_union"`, `"code": "unrecognized_keys"`, `"Unrecognized key: \"parts\""`, `"expected": "array"`} {
		if !strings.Contains(msg, want) {
			t.Errorf("message lacks %s:\n%s", want, msg)
		}
	}
}
