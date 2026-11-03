package tsfixture_test

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/internal/tsfixture"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providers/fireworks"
	"github.com/digitallysavvy/go-ai/pkg/providers/gmicloud"
	"github.com/digitallysavvy/go-ai/pkg/providers/ollama"
	"github.com/digitallysavvy/go-ai/pkg/providers/together"
	"github.com/digitallysavvy/go-ai/pkg/providers/zai"
)

// r82provbWireHeaders returns the provider-level headers a TS test set
// (authorization and user-agent come from the API key and the SDK).
func r82provbWireHeaders(m tsfixture.WireModel) map[string]string {
	h, _ := m.Config["headers"].(map[string]interface{})
	out := map[string]string{}
	for k, v := range h {
		if lk := strings.ToLower(k); lk == "user-agent" || lk == "authorization" {
			continue
		}
		if s, ok := v.(string); ok {
			out[k] = s
		}
	}
	return out
}

// r82provbIncludeUsage adds stream_options.include_usage to every recorded
// streaming chat request: the TS provider (togetherai, fireworks, gmicloud)
// builds the chat class with includeUsage: true.
func r82provbIncludeUsage(w *tsfixture.WireTest) {
	for i, h := range w.HTTP {
		var body map[string]interface{}
		if json.Unmarshal(h.Body(), &body) != nil || body["stream"] != true || body["stream_options"] != nil {
			continue
		}
		body["stream_options"] = map[string]interface{}{"include_usage": true}
		b, _ := json.Marshal(body)
		w.HTTP[i].BodyB64 = base64.StdEncoding.EncodeToString(b)
	}
}

// r82provbDropRawKeyWarning removes the deprecated-key warning TS reports
// because the class tests name the provider "test-provider" and pass the
// hyphenated raw key; with the Go provider's own (unhyphenated) key a TS
// caller gets none.
func r82provbDropRawKeyWarning(w *tsfixture.WireTest) {
	keep := func(v interface{}) interface{} {
		ws, ok := v.([]interface{})
		if !ok {
			return v
		}
		var out []interface{}
		for _, x := range ws {
			if m, ok := x.(map[string]interface{}); ok && m["message"] == "Use 'testProvider' instead." {
				continue
			}
			out = append(out, x)
		}
		if out == nil {
			out = []interface{}{}
		}
		return out
	}
	for i := range w.Calls {
		if r, ok := w.Calls[i].Result.(map[string]interface{}); ok {
			if _, has := r["warnings"]; has {
				r["warnings"] = keep(r["warnings"])
			}
		}
		for _, p := range w.Calls[i].Parts {
			if m, ok := p.(map[string]interface{}); ok && m["type"] == "stream-start" {
				m["warnings"] = keep(m["warnings"])
			}
		}
	}
}

// r82provbOllama adjusts what TS sent to what the Go-only ollama provider
// sends since v0.5.2: an explicit "stream": false and no API key.
func r82provbOllama(w *tsfixture.WireTest) {
	for i, h := range w.HTTP {
		delete(w.HTTP[i].Headers, "authorization")
		var body map[string]interface{}
		if json.Unmarshal(h.Body(), &body) != nil || body["stream"] != nil {
			continue
		}
		body["stream"] = false
		b, _ := json.Marshal(body)
		w.HTTP[i].BodyB64 = base64.StdEncoding.EncodeToString(b)
	}
	for i := range w.Calls {
		r, _ := w.Calls[i].Result.(map[string]interface{})
		req, _ := r["request"].(map[string]interface{})
		switch body := req["body"].(type) {
		case map[string]interface{}:
			if body["stream"] == nil {
				body["stream"] = false
			}
		case string: // doGenerate reports the JSON text it sent
			var m map[string]interface{}
			if json.Unmarshal([]byte(body), &m) == nil && m["stream"] == nil {
				m["stream"] = false
				b, _ := json.Marshal(m)
				req["body"] = string(b)
			}
		}
	}
}

func r82provbWireKey(m tsfixture.WireModel) string {
	h, _ := m.Config["headers"].(map[string]interface{})
	for k, v := range h {
		if s, _ := v.(string); strings.EqualFold(k, "authorization") && strings.HasPrefix(s, "Bearer ") {
			return strings.TrimPrefix(s, "Bearer ")
		}
	}
	return "test-api-key"
}

// TestR82provbCompatChatWireThroughProviders replays the TS
// OpenAICompatibleChatLanguageModel tests (testdata/tswire/openai-compatible)
// through every Go provider built on the shared OpenAI-compatible chat
// stream (streaming.OpenAICompatStream): their TS providers are thin
// createOpenAICompatible-style wrappers of that class.
func TestR82provbCompatChatWireThroughProviders(t *testing.T) {
	file := "../../providers/testdata/tswire/openai-compatible.jsonl.gz"
	if _, err := os.Stat(file); err != nil {
		t.Skip("no recording")
	}
	// classConfig returns a reason to skip a test whose class settings differ
	// from the ones the TS provider passes (want: setting -> value; a
	// missing setting is the class default false).
	classConfig := func(want map[string]bool) func(m tsfixture.WireModel) string {
		return func(m tsfixture.WireModel) string {
			if m.Cls != "OpenAICompatibleChatLanguageModel" {
				return "only the chat model is shared with this provider"
			}
			for _, k := range []string{"supportsStructuredOutputs", "supportsMultiPartToolContent", "includeUsage"} {
				got, _ := m.Config[k].(bool)
				if _, set := m.Config[k]; set && got != want[k] {
					return "the TS test sets " + k + " on the class; the TS provider sets it differently"
				}
			}
			return ""
		}
	}
	common := map[string]string{
		"~camelCase provider options": "the TS test provider name test-provider is hyphenated (raw versus camelCase keys); the Go provider's key has no hyphen",
		"~transformRequestBody":       "the transform is code in the TS test",
		"~metadata extraction":        "metadataExtractor is a setting of the TS class that the TS provider does not expose (openai.Config.MetadataExtractor covers the class)",
		"doGenerate > should generate distinct IDs for parallel tool calls with empty IDs": "TS generateId is random; distinct Go ids are checked in pkg/providerutils/streaming",
	}
	type mk func(base, key string, h map[string]string) provider.Provider
	for _, p := range []struct {
		name, key string
		suite     tsfixture.WireSuite
		config    map[string]bool
		build     mk
	}{
		{"together", "togetherai", tsfixture.WireSuite{GoAdds: "/v1", RootBase: true, PrepareTest: r82provbIncludeUsage, Skip: map[string]string{
			// getModelStructuredOutputSupport(modelId) is false for the
			// test model ids.
			"~response format > should use json_schema":                                             "TS togetherai enables structured outputs per model id; not for this test model",
			"~response format > should set name & description":                                      "TS togetherai enables structured outputs per model id; not for this test model",
			"~response format > should send strict: false":                                          "TS togetherai enables structured outputs per model id; not for this test model",
			"~response format > should forward json response format as \"json_object\" and include": "TS togetherai enables structured outputs per model id; not for this test model",
		}}, map[string]bool{"includeUsage": true}, func(b, k string, h map[string]string) provider.Provider {
			return together.New(together.Config{APIKey: k, BaseURL: b, Headers: h})
		}},
		{"fireworks", "fireworks", tsfixture.WireSuite{GoAdds: "/inference", GoPrefix: func(string) string { return "/inference" }, PrepareTest: r82provbIncludeUsage, Skip: map[string]string{
			"doGenerate > response format > should pass top-level max reasoning as reasoning_effort": "TS fireworks transformRequestBody maps max to high with a compatibility warning",
		}}, map[string]bool{"includeUsage": true, "supportsStructuredOutputs": true}, func(b, k string, h map[string]string) provider.Provider {
			return fireworks.New(fireworks.Config{APIKey: k, BaseURL: b, Headers: h})
		}},
		{"gmicloud", "gmicloud", tsfixture.WireSuite{PrepareTest: r82provbIncludeUsage}, map[string]bool{"includeUsage": true}, func(b, k string, h map[string]string) provider.Provider {
			return gmicloud.New(gmicloud.Config{APIKey: k, BaseURL: b, Headers: h})
		}},
		{"zai", "zai", tsfixture.WireSuite{Skip: map[string]string{
			"doGenerate > should pass tools and toolChoice":                                       "TS ZaiChatLanguageModel drops a tool choice other than auto with an unsupported warning",
			"doGenerate > response format > should not duplicate reasoningEffort in request body": "TS ZaiChatLanguageModel parses providerOptions.zai with a strict schema, which drops unknown keys",
			"doGenerate > response format > should not duplicate textVerbosity in request body":   "TS ZaiChatLanguageModel parses providerOptions.zai with a strict schema, which drops unknown keys",
			"~should include provider-specific options":                                           "TS ZaiChatLanguageModel parses providerOptions.zai with a strict schema, which drops unknown keys",
		}}, nil, func(b, k string, h map[string]string) provider.Provider {
			return zai.New(zai.Config{APIKey: k, BaseURL: b, Headers: h})
		}},
		{"ollama", "ollama", tsfixture.WireSuite{RootBase: true, PrepareTest: r82provbOllama, Skip: map[string]string{
			"doGenerate > response format > should forward json response format as \"json_object\" and omit schema when structuredOutputs are disabled": "the Go-only ollama provider always uses structured outputs (json_schema) since v0.5.2",
		}}, map[string]bool{"supportsStructuredOutputs": true}, func(b, k string, h map[string]string) provider.Provider {
			return ollama.New(ollama.Config{BaseURL: b, Headers: h})
		}},
	} {
		p := p
		t.Run(p.name, func(t *testing.T) {
			s := p.suite
			s.LanguageResults = true
			s.SkipModel = classConfig(p.config)
			if s.Skip == nil {
				s.Skip = map[string]string{}
			}
			for k, v := range common {
				s.Skip[k] = v
			}
			prep := s.PrepareTest
			s.PrepareTest = func(w *tsfixture.WireTest) {
				r82provbDropRawKeyWarning(w)
				if prep != nil {
					prep(w)
				}
			}
			s.MetadataKeyAlias = map[string]string{"test-provider": p.key, "testProvider": p.key}
			s.ProviderOptionsKeyAlias = map[string]string{"test-provider": p.key, "testProvider": p.key}
			s.Provider = func(base string, m tsfixture.WireModel) (provider.Provider, error) {
				return p.build(base, r82provbWireKey(m), r82provbWireHeaders(m)), nil
			}
			tsfixture.RunWire(t, file, s)
		})
	}
}
