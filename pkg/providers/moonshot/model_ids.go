package moonshot

// Moonshot AI chat model IDs. Mirrors MoonshotAIChatModelId in
// packages/moonshotai/src/moonshotai-chat-options.ts, plus IDs the Go SDK
// already exposed. Any other model ID string is accepted as well.
const (
	ModelMoonshotV1Auto              = "moonshot-v1-auto"
	ModelMoonshotV18k                = "moonshot-v1-8k"
	ModelMoonshotV132k               = "moonshot-v1-32k"
	ModelMoonshotV1128k              = "moonshot-v1-128k"
	ModelMoonshotV18kVisionPreview   = "moonshot-v1-8k-vision-preview"
	ModelMoonshotV132kVisionPreview  = "moonshot-v1-32k-vision-preview"
	ModelMoonshotV1128kVisionPreview = "moonshot-v1-128k-vision-preview"
	ModelKimiK2                      = "kimi-k2"
	ModelKimiK20905                  = "kimi-k2-0905"
	ModelKimiK2Thinking              = "kimi-k2-thinking"
	ModelKimiK2ThinkingTurbo         = "kimi-k2-thinking-turbo"
	ModelKimiK2Turbo                 = "kimi-k2-turbo"
	ModelKimiK25                     = "kimi-k2.5"
	ModelKimiK26                     = "kimi-k2.6"
	ModelKimiK27Code                 = "kimi-k2.7-code"
	ModelKimiK27CodeHighspeed        = "kimi-k2.7-code-highspeed"
	ModelKimiK3                      = "kimi-k3"
	defaultLanguageModelID           = ModelMoonshotV132k
)
