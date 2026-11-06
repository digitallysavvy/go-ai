package pplxschema

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/internal/zodlite"
)

// zod v4 (4.4.3) issue lists for perplexityOutputItemSchema failures, checked
// with node: every failing item is an invalid_union over all five members.
func TestOutputItemUnionIssues(t *testing.T) {
	head := `Type validation failed: Value: `
	for _, tc := range []struct{ frame, want string }{
		{`{"item":{"type":"search_results","results":[{"title":"x"}]}}`,
			`[{"code":"invalid_union","errors":[[{"code":"invalid_value","values":["message"],"path":["type"],"message":"Invalid input: expected \"message\""}],[{"expected":"string","code":"invalid_type","path":["results",0,"url"],"message":"Invalid input: expected string, received undefined"}],[{"code":"invalid_value","values":["fetch_url_results"],"path":["type"],"message":"Invalid input: expected \"fetch_url_results\""}],[{"code":"invalid_value","values":["function_call"],"path":["type"],"message":"Invalid input: expected \"function_call\""}],[{"code":"custom","path":["type"],"message":"Invalid input"}]],"path":["item"],"message":"Invalid input"}]`},
		{`{"item":5}`,
			`[{"code":"invalid_union","errors":[[{"expected":"object","code":"invalid_type","path":[],"message":"Invalid input: expected object, received number"}],[{"expected":"object","code":"invalid_type","path":[],"message":"Invalid input: expected object, received number"}],[{"expected":"object","code":"invalid_type","path":[],"message":"Invalid input: expected object, received number"}],[{"expected":"object","code":"invalid_type","path":[],"message":"Invalid input: expected object, received number"}],[{"expected":"object","code":"invalid_type","path":[],"message":"Invalid input: expected object, received number"}]],"path":["item"],"message":"Invalid input"}]`},
		{`{"item":{"type":1}}`,
			`[{"code":"invalid_union","errors":[[{"code":"invalid_value","values":["message"],"path":["type"],"message":"Invalid input: expected \"message\""}],[{"code":"invalid_value","values":["search_results"],"path":["type"],"message":"Invalid input: expected \"search_results\""}],[{"code":"invalid_value","values":["fetch_url_results"],"path":["type"],"message":"Invalid input: expected \"fetch_url_results\""}],[{"code":"invalid_value","values":["function_call"],"path":["type"],"message":"Invalid input: expected \"function_call\""}],[{"expected":"string","code":"invalid_type","path":["type"],"message":"Invalid input: expected string, received number"}]],"path":["item"],"message":"Invalid input"}]`},
	} {
		frame := `{"type":"x",` + tc.frame[1:]
		msg := zodlite.ValidationMessage(Chunk, frame)
		if msg == "" || msg[:len(head)] != head {
			t.Fatalf("%s accepted: %q", frame, msg)
		}
		if got := zodlite.CompactJSON(zodlite.IssueText(msg)); got != tc.want {
			t.Errorf("%s\n got %s\nwant %s", frame, got, tc.want)
		}
	}
	for _, ok := range []string{`{"type":"x","item":{"type":"finance_results","results":5}}`, `{"type":"x","item":{"type":"message","content":[{"type":"output_text","text":"hi"}]}}`} {
		if msg := zodlite.ValidationMessage(Chunk, ok); msg != "" {
			t.Errorf("%s rejected: %s", ok, msg)
		}
	}
}
