package baseten

// Baseten Model APIs chat model IDs (TS BasetenChatModelId,
// baseten-model-catalog.ts). LanguageModel accepts any other string too.
const (
	ModelDeepSeekR10528             = "deepseek-ai/DeepSeek-R1-0528"
	ModelDeepSeekV30324             = "deepseek-ai/DeepSeek-V3-0324"
	ModelDeepSeekV31                = "deepseek-ai/DeepSeek-V3.1"
	ModelKimiK2Instruct0905         = "moonshotai/Kimi-K2-Instruct-0905"
	ModelKimiK2Thinking             = "moonshotai/Kimi-K2-Thinking"
	ModelQwen3235BA22BInstruct2507  = "Qwen/Qwen3-235B-A22B-Instruct-2507"
	ModelQwen3Coder480BA35BInstruct = "Qwen/Qwen3-Coder-480B-A35B-Instruct"
	ModelGPTOSS120B                 = "openai/gpt-oss-120b"
	ModelGLM46                      = "zai-org/GLM-4.6"
	ModelGLM47                      = "zai-org/GLM-4.7"
)

// Baseten Model APIs decision model IDs (TS BasetenModelIds['decision'],
// baseten-model-catalog.ts). DecisionModel accepts any other string too.
const (
	DecisionModelBisonLatest      = "decision-models/baseten-bison-latest"
	DecisionModelGroundhogLatest  = "decision-models/baseten-groundhog-latest"
	DecisionModelMammoth27BLatest = "decision-models/baseten-mammoth-27b-latest"
	DecisionModelElephant9BLatest = "decision-models/baseten-elephant-9b-latest"
	DecisionModelJoey2BLatest     = "decision-models/baseten-joey-2b-latest"
)
