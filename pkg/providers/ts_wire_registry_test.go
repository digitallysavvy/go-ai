package providers_test

import (
	"encoding/json"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/cerebras"
	"github.com/digitallysavvy/go-ai/pkg/providers/deepseek"
	"github.com/digitallysavvy/go-ai/pkg/providers/gmicloud"
	"github.com/digitallysavvy/go-ai/pkg/providers/heygen"
	"github.com/digitallysavvy/go-ai/pkg/providers/huggingface"
	"github.com/digitallysavvy/go-ai/pkg/providers/klingai"
	"github.com/digitallysavvy/go-ai/pkg/providers/minimax"
	"github.com/digitallysavvy/go-ai/pkg/providers/moonshot"
	"github.com/digitallysavvy/go-ai/pkg/providers/openresponses"
	"github.com/digitallysavvy/go-ai/pkg/providers/quiverai"
	"github.com/digitallysavvy/go-ai/pkg/providers/typesafeai"
	"github.com/digitallysavvy/go-ai/pkg/providers/zai"
	"net/url"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/internal/tsfixture"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providers/alibaba"
	"github.com/digitallysavvy/go-ai/pkg/providers/assemblyai"
	"github.com/digitallysavvy/go-ai/pkg/providers/baseten"
	"github.com/digitallysavvy/go-ai/pkg/providers/bfl"
	"github.com/digitallysavvy/go-ai/pkg/providers/bytedance"
	"github.com/digitallysavvy/go-ai/pkg/providers/cartesia"
	"github.com/digitallysavvy/go-ai/pkg/providers/cohere"
	"github.com/digitallysavvy/go-ai/pkg/providers/deepgram"
	"github.com/digitallysavvy/go-ai/pkg/providers/deepinfra"
	"github.com/digitallysavvy/go-ai/pkg/providers/elevenlabs"
	"github.com/digitallysavvy/go-ai/pkg/providers/fireworks"
	"github.com/digitallysavvy/go-ai/pkg/providers/fishaudio"
	"github.com/digitallysavvy/go-ai/pkg/providers/gateway"
	"github.com/digitallysavvy/go-ai/pkg/providers/gladia"
	"github.com/digitallysavvy/go-ai/pkg/providers/groq"
	"github.com/digitallysavvy/go-ai/pkg/providers/hume"
	"github.com/digitallysavvy/go-ai/pkg/providers/luma"
	"github.com/digitallysavvy/go-ai/pkg/providers/mistral"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
	"github.com/digitallysavvy/go-ai/pkg/providers/perplexity"
	"github.com/digitallysavvy/go-ai/pkg/providers/prodia"
	"github.com/digitallysavvy/go-ai/pkg/providers/replicate"
	"github.com/digitallysavvy/go-ai/pkg/providers/revai"
	"github.com/digitallysavvy/go-ai/pkg/providers/together"
	"github.com/digitallysavvy/go-ai/pkg/providers/topaz"
	"github.com/digitallysavvy/go-ai/pkg/providers/voyage"
	"github.com/digitallysavvy/go-ai/pkg/providers/xai"
)

const wireKey = "test-api-key"

// wireAPIKey recovers the API key the TS test used from the recorded
// authorization / x-api-key style headers.
func wireAPIKey(m tsfixture.WireModel) string {
	h, _ := m.Config["headers"].(map[string]interface{})
	for k, v := range h {
		s, _ := v.(string)
		switch strings.ToLower(k) {
		case "authorization":
			if rest := strings.TrimPrefix(s, "Bearer "); rest != s {
				return rest
			}
		}
	}
	return wireKey
}

type pf = func(base string, m tsfixture.WireModel) (provider.Provider, error)

func simple(f func(base, key string, h map[string]string) provider.Provider) pf {
	return func(base string, m tsfixture.WireModel) (provider.Provider, error) {
		return f(base, wireAPIKey(m), wireHeaders(m)), nil
	}
}

// wireHeaders returns the provider-level headers the TS test configured
// (authorization and user-agent come from the API key and the SDK).
func wireHeaders(m tsfixture.WireModel) map[string]string {
	h, _ := m.Config["headers"].(map[string]interface{})
	out := map[string]string{}
	for k, v := range h {
		lk := strings.ToLower(k)
		if lk == "user-agent" {
			continue
		}
		if s, ok := v.(string); ok {
			out[k] = s
		}
	}
	return out
}

// wireSuites maps a TS package (testdata/tswire/<pkg>.jsonl.gz) to the Go
// provider replayed against it.
// bflTestModelRecording: recorded at TS b658e8503c, where the unknown model ID
// "test-model" used the legacy request format. TS #23410 sends unknown model IDs
// to the latest (Klein) API and renamed the TS test model to flux-2-pro, so these
// recordings no longer describe TS behavior for "test-model". The same cases are
// covered for the legacy models by pkg/providers/bfl tests.
const bflTestModelRecording = "recorded before TS #23410 (unknown model IDs default to the latest API); test-model is no longer a legacy model"

var wireSuites = map[string]tsfixture.WireSuite{
	"openai-compatible": {Skip: map[string]string{
		"~transformRequestBody": "the transform is code in the TS test; Go has Config.TransformRequestBody",
		"doEmbed > should not emit deprecated warning when camelCase provider name key is used": "TS embedding reads only the raw provider-name key; Go also honours the camelCase key",
		"~should handle error stream parts":                                                     "TS emits stream errors as parts; Go ends the stream with the same error from Next",
		"~should preserve structured error stream parts":                                        "TS emits stream errors as parts; Go ends the stream with the same error from Next",
	}, Language: func(p provider.Provider, cls, id string) (provider.LanguageModel, error) {
		op := p.(*openai.Provider)
		if strings.Contains(cls, "Completion") {
			return op.CompletionModel(id)
		}
		return op.ChatModel(id)
	}, Provider: func(b string, m tsfixture.WireModel) (provider.Provider, error) {
		name := m.Provider
		if i := indexByte(name, '.'); i > 0 {
			name = name[:i]
		}
		structured, _ := m.Config["supportsStructuredOutputs"].(bool)
		multipart, _ := m.Config["supportsMultiPartToolContent"].(bool)
		includeUsage, _ := m.Config["includeUsage"].(bool)
		return openai.New(openai.Config{APIKey: wireAPIKey(m), BaseURL: b, Name: name, OpenAICompatible: true, Headers: wireHeaders(m),
			SupportsStructuredOutputs: &structured, SupportsMultiPartToolContent: multipart, IncludeUsage: includeUsage}), nil
	}},
	"cohere": {RootBase: true, Skip: map[string]string{
		"~should validate cached token usage": "TS validates the usage schema and throws; Go reads usage leniently",
	}, Provider: simple(func(b, k string, h map[string]string) provider.Provider {
		return cohere.New(cohere.Config{APIKey: k, BaseURL: b, Headers: h})
	})},
	"mistral": {
		Skip: map[string]string{
			"doGenerate > should inject JSON schema when structured outputs are disabled": "the injected schema text keeps TS object key order, which a Go map cannot",
		},
		RootBase: true,
		Provider: simple(func(b, k string, h map[string]string) provider.Provider {
			return mistral.New(mistral.Config{APIKey: k, BaseURL: b, Headers: h})
		}),
		Language: func(p provider.Provider, cls, id string) (provider.LanguageModel, error) {
			if strings.Contains(cls, "Conversation") {
				return p.(*mistral.Provider).ConversationModel(id), nil
			}
			return p.LanguageModel(id)
		},
	},
	"voyage": {Provider: simple(func(b, k string, h map[string]string) provider.Provider {
		return voyage.New(voyage.Config{APIKey: k, BaseURL: b, Headers: h})
	})},
	"perplexity": {Provider: simple(func(b, k string, h map[string]string) provider.Provider {
		return perplexity.New(perplexity.Config{APIKey: k, BaseURL: b, Headers: h})
	})},
	"baseten": {Provider: simple(func(b, k string, h map[string]string) provider.Provider {
		return baseten.New(baseten.Config{APIKey: k, ModelURL: b, Headers: h})
	})},
	"togetherai": {GoAdds: "/v1", RootBase: true, Provider: simple(func(b, k string, h map[string]string) provider.Provider {
		return together.New(together.Config{APIKey: k, BaseURL: b, Headers: h})
	})},
	"deepinfra": {GoAdds: "/inference", GoPrefix: func(p string) string {
		if root, ok := strings.CutSuffix(p, "/inference"); ok {
			return root + "/openai"
		}
		return p + "/openai"
	}, Provider: simple(func(b, k string, h map[string]string) provider.Provider {
		return deepinfra.New(deepinfra.Config{APIKey: k, BaseURL: b, Headers: h})
	})},
	"fireworks": {GoAdds: "/inference/v1", GoPrefix: func(string) string { return "/inference" }, Provider: simple(func(b, k string, h map[string]string) provider.Provider {
		return fireworks.New(fireworks.Config{APIKey: k, BaseURL: b, Headers: h, ImagePollIntervalMs: 1, ImagePollTimeoutMs: 5000})
	})},
	"luma": {Provider: simple(func(b, k string, h map[string]string) provider.Provider {
		return luma.New(luma.Config{APIKey: k, BaseURL: b, Headers: h})
	})},
	"prodia": {Provider: simple(func(b, k string, h map[string]string) provider.Provider {
		return prodia.New(prodia.Config{APIKey: k, BaseURL: b, Headers: h})
	})},
	"replicate": {Provider: simple(func(b, k string, h map[string]string) provider.Provider {
		return replicate.New(replicate.Config{APIKey: k, BaseURL: b, Headers: h})
	})},
	"topaz": {Skip: map[string]string{
		"TopazImageModel > doGenerate > throws when polling exceeds the timeout": "the number of status polls before the 1ms timeout is timing dependent (TS polls twice, Go once); both then cancel the job and fail with the same message",
	}, Provider: simple(func(b, k string, h map[string]string) provider.Provider {
		return topaz.New(topaz.Config{APIKey: k, BaseURL: b, Headers: h, VideoPollIntervalMillis: 1, VideoPollTimeoutMillis: 5000})
	})},
	"xai": {ProviderTool: xaiProviderTool, BatchPrefix: "/v1", Skip: map[string]string{
		"xAI batch > uploads prepared JSONL requests and creates a file-backed batch": "provider headers are not captured in the batch recording config; JSONL line key order is semantically irrelevant (covered by xai batch unit tests)",
	}, Provider: simple(func(b, k string, h map[string]string) provider.Provider {
		return xai.New(xai.Config{APIKey: k, BaseURL: b, Headers: h})
	})},
	"black-forest-labs": {Skip: map[string]string{
		"BlackForestLabsImageModel > doGenerate > passes the correct parameters including aspect ratio and providerOptions": bflTestModelRecording,
		"BlackForestLabsImageModel > doGenerate > warns and derives aspect_ratio when size is provided":                     bflTestModelRecording,
		"BlackForestLabsImageModel > doGenerate > warns and ignores size when both size and aspectRatio are provided":       bflTestModelRecording,
	}, Provider: simple(func(b, k string, h map[string]string) provider.Provider {
		return bfl.New(bfl.Config{APIKey: k, BaseURL: b, Headers: h, PollIntervalMillis: 1, PollTimeoutMillis: 5000})
	})},
	"bytedance": {Provider: func(b string, m tsfixture.WireModel) (provider.Provider, error) {
		return bytedance.New(bytedance.Config{APIKey: wireAPIKey(m), BaseURL: b, Headers: wireHeaders(m)})
	}},
	"cartesia": {Skip: map[string]string{
		"~through streaming transcription instead of the batch endpoint": "streaming transcription uses a websocket, which the replay does not carry",
	}, Provider: simple(func(b, k string, h map[string]string) provider.Provider {
		return cartesia.New(cartesia.Config{APIKey: k, BaseURL: b, Headers: h})
	})},
	"deepgram": {Provider: simple(func(b, k string, h map[string]string) provider.Provider {
		return deepgram.New(deepgram.Config{APIKey: k, BaseURL: b, Headers: h})
	})},
	"elevenlabs": {Provider: simple(func(b, k string, h map[string]string) provider.Provider {
		return elevenlabs.New(elevenlabs.Config{APIKey: k, BaseURL: b, Headers: h})
	})},
	"fish-audio": {Provider: simple(func(b, k string, h map[string]string) provider.Provider {
		return fishaudio.New(fishaudio.Config{APIKey: k, BaseURL: b, Headers: h})
	})},
	"hume": {Provider: simple(func(b, k string, h map[string]string) provider.Provider {
		return hume.New(hume.Config{APIKey: k, BaseURL: b, Headers: h})
	})},
	"assemblyai": {GoPrefix: func(string) string { return "/v2" }, Provider: simple(func(b, k string, h map[string]string) provider.Provider {
		return assemblyai.New(assemblyai.Config{APIKey: k, BaseURL: b, Headers: h})
	})},
	"gladia": {GoPrefix: func(string) string { return "/v2" }, Provider: simple(func(b, k string, h map[string]string) provider.Provider {
		return gladia.New(gladia.Config{APIKey: k, BaseURL: b, Headers: h})
	})},
	"groq": {GoPrefix: func(string) string { return "/openai" }, Provider: simple(func(b, k string, h map[string]string) provider.Provider {
		return groq.New(groq.Config{APIKey: k, BaseURL: b, Headers: h})
	})},
	"revai": {Provider: simple(func(b, k string, h map[string]string) provider.Provider {
		return revai.New(revai.Config{APIKey: k, BaseURL: b, Headers: h})
	})},
	"alibaba": {Skip: map[string]string{
		"~injects the schema for other models": "the injected schema text keeps TS object key order, which a Go map cannot",
	}, Provider: simple(func(b, k string, h map[string]string) provider.Provider {
		return alibaba.New(alibaba.Config{APIKey: k, BaseURL: b, VideoBaseURL: b, EmbeddingBaseURL: b, Headers: h})
	})},
	"deepseek": {Skip: map[string]string{
		"~raw usage > continues validating known usage field":                                                                      "TS validates the usage schema and throws; Go reads usage leniently",
		"DeepSeekChatLanguageModel > doGenerate > tool call > json response format > should send correct request body with schema": "the injected schema text keeps TS object key order, which a Go map cannot",
	}, Provider: func(b string, m tsfixture.WireModel) (provider.Provider, error) {
		cfg := deepseek.Config{APIKey: wireAPIKey(m), BaseURL: b, Headers: wireHeaders(m)}
		if name, _, _ := strings.Cut(m.Provider, "."); name != "" && name != "deepseek" {
			cfg.Name, cfg.ProviderOptionsName = name, name
		}
		if v, ok := m.Config["supportsStructuredOutputs"].(bool); ok {
			cfg.SupportsStructuredOutputs = &v
		}
		return deepseek.New(cfg), nil
	}},
	"moonshotai": {Skip: map[string]string{
		"~should preserve a provider error envelope in stream errors": "TS emits this as an error part; Go ends the stream with the same error from Next",
	}, Provider: simple(func(b, k string, h map[string]string) provider.Provider {
		return moonshot.New(moonshot.Config{APIKey: k, BaseURL: b, Headers: h})
	})},
	// Open Responses takes the full endpoint URL as its base URL.
	"open-responses": {Skip: map[string]string{
		"~losslessly replay": "the TS test registers a hosted-tool extension in code, which the recording does not carry",
		"~extension":         "the TS test registers an extension in code, which the recording does not carry",
		"~multi-turn tool conversation > should send correct request body": "tool output JSON text keeps TS object key order, which a Go map cannot",
	}, Provider: func(b string, m tsfixture.WireModel) (provider.Provider, error) {
		endpoint := b
		if len(m.RequestURLs) > 0 {
			if u, err := url.Parse(m.RequestURLs[0]); err == nil {
				endpoint = b + u.Path
			}
		}
		return openresponses.New(openresponses.Config{Name: strings.SplitN(m.Provider, ".", 2)[0], APIKey: wireAPIKey(m), BaseURL: endpoint, Headers: wireHeaders(m)}), nil
	}},
	"huggingface": {Provider: simple(func(b, k string, h map[string]string) provider.Provider {
		return huggingface.New(huggingface.Config{APIKey: k, BaseURL: b, Headers: h})
	})},
	"cerebras": {Provider: simple(func(b, k string, h map[string]string) provider.Provider {
		return cerebras.New(cerebras.Config{APIKey: k, BaseURL: b, Headers: h})
	})},
	"gmicloud": {Provider: simple(func(b, k string, h map[string]string) provider.Provider {
		return gmicloud.New(gmicloud.Config{APIKey: k, BaseURL: b, Headers: h})
	})},
	"zai": {Provider: simple(func(b, k string, h map[string]string) provider.Provider {
		return zai.New(zai.Config{APIKey: k, BaseURL: b, Headers: h})
	})},
	"klingai": {Provider: func(b string, m tsfixture.WireModel) (provider.Provider, error) {
		return klingai.New(klingai.Config{APIKey: wireAPIKey(m), BaseURL: b, Headers: wireHeaders(m)})
	}},
	"minimax": {Provider: simple(func(b, k string, h map[string]string) provider.Provider {
		return minimax.New(minimax.Config{APIKey: k, BaseURL: b, VideoBaseURL: b, Headers: h})
	})},
	"heygen": {Provider: simple(func(b, k string, h map[string]string) provider.Provider {
		return heygen.New(heygen.Config{APIKey: k, BaseURL: b, Headers: h, PollIntervalMillis: 1, PollTimeoutMillis: 5000})
	})},
	"typesafe-ai": {Skip: map[string]string{
		"restores authentication from the environment when no headers are serialized": "depends on process environment variables set by the TS test",
	}, Provider: simple(func(b, k string, h map[string]string) provider.Provider {
		return typesafeai.New(typesafeai.Config{APIKey: k, BaseURL: b, Headers: h})
	})},
	"quiverai": {Provider: simple(func(b, k string, h map[string]string) provider.Provider {
		return quiverai.New(quiverai.Config{APIKey: k, BaseURL: b, Headers: h})
	})},
	"gateway": {
		// TS gateway language tests answer with non-V4 JSON; only the request is compared.
		Skip: map[string]string{
			"~o11y": "observability headers come from the Vercel environment; covered by gateway unit tests",
			"GatewayBatch > doStartBatch > should send the shared request model in the model header":    "ai-o11y-* headers come from the Vercel environment, which the replay does not set",
			"~GatewayBatch > doCancelBatch > should acknowledge cancellation without treating":          "ai-o11y-* headers come from the Vercel environment, which the replay does not set",
			"GatewayLanguageModel > Provider Options > should pass provider routing order for doStream": "TS callers omit includeRawChunks here; streamText always sends it (false), which Go sends",
			"GatewayLanguageModel > Provider Options > should pass providerTimeouts for doStream":       "TS callers omit includeRawChunks here; streamText always sends it (false), which Go sends",
		},
		Provider: func(b string, m tsfixture.WireModel) (provider.Provider, error) {
			return gateway.New(gateway.Config{APIKey: wireAPIKey(m), BaseURL: b, Headers: wireHeaders(m)})
		},
		Language: func(p provider.Provider, cls, id string) (provider.LanguageModel, error) { return p.LanguageModel(id) },
		ProviderTool: func(id, name string, args map[string]interface{}) (types.Tool, bool) {
			return types.Tool{Type: types.ToolTypeProviderDefined, ProviderID: id, Name: name, ProviderArgs: args}, true
		},
	},
}

func indexByte(s string, c byte) int {
	for i := 0; i < len(s); i++ {
		if s[i] == c {
			return i
		}
	}
	return -1
}

// xaiProviderTool maps a TS xai provider tool to the typed Go tool.
func xaiProviderTool(id, name string, args map[string]interface{}) (types.Tool, bool) {
	raw, _ := json.Marshal(args)
	decode := func(v interface{}) bool { return len(args) == 0 || json.Unmarshal(raw, v) == nil }
	var t types.Tool
	switch id {
	case "xai.web_search":
		var c xai.WebSearchConfig
		if !decode(&c) {
			return t, false
		}
		t = xai.WebSearch(c)
	case "xai.x_search":
		var c xai.XSearchConfig
		if !decode(&c) {
			return t, false
		}
		t = xai.XSearch(c)
	case "xai.file_search":
		var c xai.FileSearchConfig
		if !decode(&c) {
			return t, false
		}
		t = xai.FileSearch(c)
	case "xai.mcp":
		var c xai.MCPServerConfig
		if !decode(&c) {
			return t, false
		}
		t = xai.MCPServer(c)
	case "xai.code_execution":
		t = xai.CodeExecution()
	case "xai.view_image":
		t = xai.ViewImage()
	case "xai.view_x_video":
		t = xai.ViewXVideo()
	default:
		return t, false
	}
	// xai tools are matched by their fixed Name ("xai.web_search", ...), so the
	// TS name the caller chose is not applied.
	return t, true
}
