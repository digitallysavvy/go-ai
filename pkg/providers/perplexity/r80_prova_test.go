package perplexity

import (
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/internal/tsfixture"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TestR80provaTSCasesLanguage replays perplexity-language-model.test.ts
// extracted at e64ed82f27 with --asserts. The extractor cannot evaluate the
// test's prepareJsonResponse / prepareStream helpers, so each case's response
// was captured by running the TS test file with a stub model that records
// the response the test prepared before its call (review round 80).
func TestR80provaTSCasesLanguage(t *testing.T) {
	cases := tsfixture.LoadCases(t, "testdata/tscases/language_asserts.json.gz")
	if len(cases) != 25 {
		t.Fatalf("want 25 cases, got %d", len(cases))
	}
	var plain, headers []tsfixture.Case
	for _, c := range cases {
		if c.Test == "passes request and provider headers and exposes response headers" {
			headers = append(headers, c)
			continue
		}
		plain = append(plain, c)
	}
	run := func(cases []tsfixture.Case, key string, h map[string]string) {
		tsfixture.Run(t, cases, func(base, id string) provider.LanguageModel {
			if id == "" {
				id = "low"
			}
			m, err := New(Config{APIKey: key, BaseURL: base, Headers: h}).LanguageModel(id)
			if err != nil {
				t.Fatal(err)
			}
			return m
		}, nil)
	}
	run(plain, "test-token", nil)
	// new PerplexityLanguageModel('fast', {headers: () => ({authorization:
	// 'Bearer custom-key', 'custom-provider-header': 'provider-value'})})
	run(headers, "custom-key", map[string]string{"custom-provider-header": "provider-value"})
}

// The values the TS expectations compare against the raw Go stream: reasoning
// block ids "reasoning-<sequence_number>", a search result source's id
// String(result.id), and the stream error object (message and type).
func TestR80provaStreamIDsAndErrorObject(t *testing.T) {
	body := "data: {\"type\":\"response.reasoning.started\",\"sequence_number\":0,\"thought\":\"a\"}\n\n" +
		"data: {\"type\":\"response.reasoning.fetch_url_results\",\"sequence_number\":1,\"contents\":[{\"title\":\"F\",\"url\":\"https://example.com/source\",\"snippet\":\"s\"}]}\n\n" +
		"data: {\"type\":\"response.reasoning.search_results\",\"sequence_number\":2,\"results\":[{\"id\":7,\"title\":\"S\",\"url\":\"https://example.com/source\"}]}\n\n" +
		"data: {\"type\":\"response.reasoning.stopped\",\"sequence_number\":3}\n\n" +
		"data: {\"type\":\"response.failed\",\"sequence_number\":4,\"error\":{\"message\":\"Agent run failed\",\"type\":\"server_error\"}}\n\ndata: [DONE]\n\n"
	s := newPerplexityAgentStream(io.NopCloser(strings.NewReader(body)), nil, false, nil)
	var got []string
	var sources []string
	var streamErr error
	for {
		c, err := s.Next()
		if err != nil {
			break
		}
		switch c.Type {
		case provider.ChunkTypeReasoningStart, provider.ChunkTypeReasoningEnd:
			got = append(got, string(c.Type)+":"+c.ID)
		case provider.ChunkTypeSource:
			sources = append(sources, c.SourceContent.ID)
		case provider.ChunkTypeError:
			streamErr = c.Err
		}
	}
	if strings.Join(got, ",") != "reasoning-start:reasoning-0,reasoning-end:reasoning-0" {
		t.Fatalf("reasoning parts = %v", got)
	}
	if len(sources) != 1 || sources[0] != "7" {
		t.Fatalf("source ids = %v, want [7]", sources)
	}
	var pe *providererrors.StreamProviderError
	if !errors.As(streamErr, &pe) || pe.Message != "Agent run failed" {
		t.Fatalf("stream error = %#v", streamErr)
	}
	data, _ := pe.Data.(map[string]interface{})
	if data["message"] != "Agent run failed" || data["type"] != "server_error" {
		t.Fatalf("stream error data = %#v", pe.Data)
	}
}
