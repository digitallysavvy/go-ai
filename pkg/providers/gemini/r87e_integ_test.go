package gemini

import (
	"encoding/json"
	"io"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS google-language-model.ts: `finishMessage: candidate?.finishMessage ??
// null` and `candidate?.continuationToken != null` keep a present "" in
// generate; the stream keeps the last present value (`!= null`), and a
// CONTINUATION with a "" token still continues (cr-r87e-provb-to-prova 2).
func TestR87eintegFinishMessageAndContinuationTokenEmpty(t *testing.T) {
	var resp Response
	if err := json.Unmarshal([]byte(`{"candidates":[{"content":{"parts":[{"text":"hi"}]},"finishReason":"MAX_TOKENS","finishMessage":"","continuationToken":""}]}`), &resp); err != nil {
		t.Fatal(err)
	}
	m := NewLanguageModel(Config{MetadataKey: "google"}, "gemini-test")
	meta := m.convertResponseFull(resp, nil, "", nil).ProviderMetadata["google"].(map[string]json.RawMessage)
	if string(meta["finishMessage"]) != `""` {
		t.Fatalf("generate finishMessage = %s", meta["finishMessage"])
	}
	if ct, ok := meta["continuationToken"]; !ok || string(ct) != `""` {
		t.Fatalf("generate continuationToken = %s", ct)
	}

	s := newTestStream(sseStream(`{"candidates":[{"content":{"parts":[{"text":"a"}]},"finishMessage":"x"}]}`,
		`{"candidates":[{"content":{"parts":[{"text":"b"}]},"finishReason":"MAX_TOKENS","finishMessage":"","continuationToken":""}]}`))
	chunks := drainChunks(t, s)
	last := chunks[len(chunks)-1]
	if last.Type != provider.ChunkTypeFinish ||
		!strings.Contains(string(last.ProviderMetadata), `"finishMessage":""`) ||
		!strings.Contains(string(last.ProviderMetadata), `"continuationToken":""`) {
		t.Fatalf("stream finish metadata = %s", last.ProviderMetadata)
	}

	s = newTestStream(sseStream(`{"candidates":[{"content":{"parts":[{"text":"a"}]},"finishReason":"CONTINUATION","continuationToken":""}]}`))
	var gotToken *string
	s.continueFn = func(token string) (io.ReadCloser, error) {
		gotToken = &token
		return sseStream(`{"candidates":[{"content":{"parts":[{"text":"b"}]},"finishReason":"STOP"}]}`), nil
	}
	drainChunks(t, s)
	if gotToken == nil || *gotToken != "" {
		t.Fatalf("continuation with an empty token: continueFn token = %v", gotToken)
	}
}
