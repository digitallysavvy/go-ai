package gemini

import (
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS only treats a part as text when `'text' in part && part.text != null`.
// A part that carries just a thoughtSignature (no text key, or a null text)
// leaves the previous content untouched; an empty-string text still attaches
// its signature to the previous content.
func TestR48_GenerateSignatureOnlyPartDoesNotAttachUnlessTextKeyPresent(t *testing.T) {
	m := makeTestModel("gemini-2.5-pro")
	for body, wantMeta := range map[string]bool{
		`{"candidates":[{"content":{"parts":[{"text":"a"},{"thoughtSignature":"s"}]},"finishReason":"STOP"}]}`:             false,
		`{"candidates":[{"content":{"parts":[{"text":"a"},{"text":null,"thoughtSignature":"s"}]},"finishReason":"STOP"}]}`: false,
		`{"candidates":[{"content":{"parts":[{"text":"a"},{"text":"","thoughtSignature":"s"}]},"finishReason":"STOP"}]}`:   true,
	} {
		var resp Response
		if err := json.Unmarshal([]byte(body), &resp); err != nil {
			t.Fatal(err)
		}
		result := m.convertResponse(resp, nil)
		text := result.Content[0].(types.TextContent)
		if got := len(text.ProviderMetadata) > 0; got != wantMeta {
			t.Errorf("%s: metadata attached = %v, want %v", body, got, wantMeta)
		}
	}
}

func TestR48_StreamSignatureOnlyPartEmitsNoEmptyTextDelta(t *testing.T) {
	count := func(second string) int {
		sse := "data: " + `{"candidates":[{"content":{"parts":[{"text":"a"}]}}]}` + "\n\n" +
			"data: " + `{"candidates":[{"content":{"parts":[` + second + `]},"finishReason":"STOP"}]}` + "\n\n"
		s := newTestStream(io.NopCloser(strings.NewReader(sse)))
		defer s.Close() //nolint:errcheck
		n := 0
		for {
			c, err := s.Next()
			if err != nil {
				return n
			}
			if c.Type == provider.ChunkTypeText && c.Text == "" && len(c.ProviderMetadata) > 0 {
				n++
			}
		}
	}
	if got := count(`{"thoughtSignature":"s"}`); got != 0 {
		t.Fatalf("signature-only part emitted %d metadata-only deltas", got)
	}
	if got := count(`{"text":"","thoughtSignature":"s"}`); got != 1 {
		t.Fatalf("empty text part emitted %d metadata-only deltas, want 1", got)
	}
}

// TS emits a tool call only for `'functionCall' in part && functionCall.name
// != null`; a call without a name produces nothing.
func TestR48_GenerateFunctionCallWithoutNameIsDropped(t *testing.T) {
	m := makeTestModel("gemini-2.5-pro")
	var resp Response
	body := `{"candidates":[{"content":{"parts":[{"functionCall":{"args":{"a":1}}},{"functionCall":{"name":"f","args":{}}}]},"finishReason":"STOP"}]}`
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatal(err)
	}
	result := m.convertResponse(resp, nil)
	if len(result.ToolCalls) != 1 || result.ToolCalls[0].ToolName != "f" || len(result.Content) != 1 {
		t.Fatalf("tool calls = %+v, content = %d", result.ToolCalls, len(result.Content))
	}
}

// With a JSON response tool TS skips every non-thought text part, including an
// empty one that carries a thoughtSignature (no attach to earlier content).
func TestR48_GenerateJSONToolSkipsEmptySignatureTextPart(t *testing.T) {
	m := makeTestModel("gemini-2.5-pro")
	var resp Response
	body := `{"candidates":[{"content":{"parts":[{"functionCall":{"name":"json","args":{"x":1}}},{"text":"","thoughtSignature":"s"}]},"finishReason":"STOP"}]}`
	if err := json.Unmarshal([]byte(body), &resp); err != nil {
		t.Fatal(err)
	}
	result := m.convertResponseFull(resp, nil, "json", nil)
	text, ok := result.Content[0].(types.TextContent)
	if !ok || len(text.ProviderMetadata) != 0 {
		t.Fatalf("content[0] = %#v", result.Content[0])
	}
}

// TS initialPromptTokenCount is `usage?.promptTokenCount`: a usage object
// without a promptTokenCount contributes none (not a zero).
func TestR48_InitialPromptTokensNeedsANumericCount(t *testing.T) {
	var with, without UsageMetadata
	if err := json.Unmarshal([]byte(`{"promptTokenCount":7}`), &with); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(`{"candidatesTokenCount":3}`), &without); err != nil {
		t.Fatal(err)
	}
	if got := initialPromptTokens(&with); got == nil || *got != 7 {
		t.Fatalf("with = %v", got)
	}
	if initialPromptTokens(&without) != nil || initialPromptTokens(nil) != nil {
		t.Fatal("expected nil without a promptTokenCount")
	}
}

func TestR48_StreamContinuationWithoutPromptCountOmitsInitialPromptTokenCount(t *testing.T) {
	s := newTestStream(sseStream(`{"candidates":[{"content":{"parts":[{"text":"hi"}]},"finishReason":"CONTINUATION","continuationToken":"tok"}],"usageMetadata":{"candidatesTokenCount":3}}`))
	s.continueFn = func(token string) (io.ReadCloser, error) {
		return sseStream(`{"candidates":[{"content":{"parts":[{"text":" more"}]},"finishReason":"STOP"}],"usageMetadata":{"promptTokenCount":9,"candidatesTokenCount":2}}`), nil
	}
	chunks := drainChunks(t, s)
	last := chunks[len(chunks)-1]
	if last.Type != provider.ChunkTypeFinish || strings.Contains(string(last.ProviderMetadata), "initialPromptTokenCount") {
		t.Fatalf("finish metadata = %s", last.ProviderMetadata)
	}
}
