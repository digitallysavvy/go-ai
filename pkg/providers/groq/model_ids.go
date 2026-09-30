package groq

// Chat model IDs mirror ai/packages/groq/src/groq-chat-language-model-options.ts
// (GroqChatModelId), captured 2026-09-30. Groq's chat model catalog is
// open-ended (the TS type also allows arbitrary strings via `string & {}`),
// so these constants are convenience values, not an exhaustive allow-list.
const (
	// production models
	ModelGemma29BIt          = "gemma2-9b-it"
	ModelLlama318BInstant    = "llama-3.1-8b-instant"
	ModelLlama3370BVersatile = "llama-3.3-70b-versatile"
	ModelMetaLlamaGuard412B  = "meta-llama/llama-guard-4-12b"
	ModelOpenAIGptOss120B    = "openai/gpt-oss-120b"
	ModelOpenAIGptOss20B     = "openai/gpt-oss-20b"

	// preview models (selection)
	ModelDeepseekR1DistillLlama70B         = "deepseek-r1-distill-llama-70b"
	ModelMetaLlama4Maverick17B128EInstruct = "meta-llama/llama-4-maverick-17b-128e-instruct"
	ModelMetaLlama4Scout17B16EInstruct     = "meta-llama/llama-4-scout-17b-16e-instruct"
	ModelMetaLlamaPromptGuard222M          = "meta-llama/llama-prompt-guard-2-22m"
	ModelMetaLlamaPromptGuard286M          = "meta-llama/llama-prompt-guard-2-86m"
	ModelMoonshotAIKimiK2Instruct0905      = "moonshotai/kimi-k2-instruct-0905"
	ModelQwenQwen3627B                     = "qwen/qwen3.6-27b"
	ModelLlamaGuard38B                     = "llama-guard-3-8b"
	ModelLlama370B8192                     = "llama3-70b-8192"
	ModelLlama38B8192                      = "llama3-8b-8192"
	ModelMixtral8X7B32768                  = "mixtral-8x7b-32768"
	ModelQwenQwq32B                        = "qwen-qwq-32b"
	ModelQwen2532B                         = "qwen-2.5-32b"
	ModelDeepseekR1DistillQwen32B          = "deepseek-r1-distill-qwen-32b"
)

// Transcription model IDs mirror
// ai/packages/groq/src/groq-transcription-model-options.ts
// (GroqTranscriptionModelId), captured 2026-09-30. Also open-ended via
// `string & {}` in TS.
const (
	ModelWhisperLargeV3Turbo = "whisper-large-v3-turbo"
	ModelWhisperLargeV3      = "whisper-large-v3"
)
