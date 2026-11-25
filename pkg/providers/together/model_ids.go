package together

// Model ID constants mirror the TypeScript SDK model ID types at 0f3a85f92d (TS catalog
// refactors eaaead397d, a60ddb4a8f, c296443f12). Any model ID string is
// accepted; these constants only document the known IDs.

// Chat model IDs (TogetherAIChatModelId in togetherai/src/togetherai-chat-options.ts).
const (
	ModelMetaLlamaLlama3_3_70BInstructTurbo      = "meta-llama/Llama-3.3-70B-Instruct-Turbo"
	ModelMetaLlamaMetaLlama3_1_8BInstructTurbo   = "meta-llama/Meta-Llama-3.1-8B-Instruct-Turbo"
	ModelMetaLlamaMetaLlama3_1_70BInstructTurbo  = "meta-llama/Meta-Llama-3.1-70B-Instruct-Turbo"
	ModelMetaLlamaMetaLlama3_1_405BInstructTurbo = "meta-llama/Meta-Llama-3.1-405B-Instruct-Turbo"
	ModelMetaLlamaMetaLlama3_8BInstructTurbo     = "meta-llama/Meta-Llama-3-8B-Instruct-Turbo"
	ModelMetaLlamaMetaLlama3_70BInstructTurbo    = "meta-llama/Meta-Llama-3-70B-Instruct-Turbo"
	ModelMetaLlamaLlama3_2_3BInstructTurbo       = "meta-llama/Llama-3.2-3B-Instruct-Turbo"
	ModelMetaLlamaMetaLlama3_8BInstructLite      = "meta-llama/Meta-Llama-3-8B-Instruct-Lite"
	ModelMetaLlamaMetaLlama3_70BInstructLite     = "meta-llama/Meta-Llama-3-70B-Instruct-Lite"
	ModelMetaLlamaLlama3_8bChatHf                = "meta-llama/Llama-3-8b-chat-hf"
	ModelMetaLlamaLlama3_70bChatHf               = "meta-llama/Llama-3-70b-chat-hf"
	ModelNvidiaLlama3_1Nemotron70BInstructHF     = "nvidia/Llama-3.1-Nemotron-70B-Instruct-HF"
	ModelQwenQwen2_5Coder32BInstruct             = "Qwen/Qwen2.5-Coder-32B-Instruct"
	ModelQwenQwQ32BPreview                       = "Qwen/QwQ-32B-Preview"
	ModelMicrosoftWizardLM2_8x22B                = "microsoft/WizardLM-2-8x22B"
	ModelGoogleGemma2_27bIt                      = "google/gemma-2-27b-it"
	ModelGoogleGemma2_9bIt                       = "google/gemma-2-9b-it"
	ModelDatabricksDbrxInstruct                  = "databricks/dbrx-instruct"
	ModelDeepseekAiDeepseekLlm67bChat            = "deepseek-ai/deepseek-llm-67b-chat"
	ModelDeepseekAiDeepSeekV3                    = "deepseek-ai/DeepSeek-V3"
	ModelDeepseekAiDeepSeekV4Flash0731           = "deepseek-ai/DeepSeek-V4-Flash-0731"
	ModelGoogleGemma2bIt                         = "google/gemma-2b-it"
	ModelGrypheMythoMaxL2_13b                    = "Gryphe/MythoMax-L2-13b"
	ModelMetaLlamaLlama2_13bChatHf               = "meta-llama/Llama-2-13b-chat-hf"
	ModelMistralaiMistral7BInstructV0_1          = "mistralai/Mistral-7B-Instruct-v0.1"
	ModelMistralaiMistral7BInstructV0_2          = "mistralai/Mistral-7B-Instruct-v0.2"
	ModelMistralaiMistral7BInstructV0_3          = "mistralai/Mistral-7B-Instruct-v0.3"
	ModelMistralaiMixtral8x7BInstructV0_1        = "mistralai/Mixtral-8x7B-Instruct-v0.1"
	ModelMistralaiMixtral8x22BInstructV0_1       = "mistralai/Mixtral-8x22B-Instruct-v0.1"
	ModelNousResearchNousHermes2Mixtral8x7BDPO   = "NousResearch/Nous-Hermes-2-Mixtral-8x7B-DPO"
	ModelQwenQwen2_5_7BInstructTurbo             = "Qwen/Qwen2.5-7B-Instruct-Turbo"
	ModelQwenQwen2_5_72BInstructTurbo            = "Qwen/Qwen2.5-72B-Instruct-Turbo"
	ModelQwenQwen2_72BInstruct                   = "Qwen/Qwen2-72B-Instruct"
	ModelUpstageSOLAR10_7BInstructV1_0           = "upstage/SOLAR-10.7B-Instruct-v1.0"
)

// Completion model IDs (TogetherAICompletionModelId in togetherai/src/togetherai-completion-options.ts).
const (
	ModelMetaLlamaLlama2_70bHf           = "meta-llama/Llama-2-70b-hf"
	ModelMistralaiMistral7BV0_1          = "mistralai/Mistral-7B-v0.1"
	ModelMistralaiMixtral8x7BV0_1        = "mistralai/Mixtral-8x7B-v0.1"
	ModelMetaLlamaLlamaGuard7b           = "Meta-Llama/Llama-Guard-7b"
	ModelCodellamaCodeLlama34bInstructHf = "codellama/CodeLlama-34b-Instruct-hf"
)

// Embedding model IDs (TogetherAIEmbeddingModelId in togetherai/src/togetherai-embedding-options.ts).
const (
	ModelTogethercomputerM2Bert80M2kRetrieval     = "togethercomputer/m2-bert-80M-2k-retrieval"
	ModelTogethercomputerM2Bert80M32kRetrieval    = "togethercomputer/m2-bert-80M-32k-retrieval"
	ModelTogethercomputerM2Bert80M8kRetrieval     = "togethercomputer/m2-bert-80M-8k-retrieval"
	ModelWhereIsAIUAELargeV1                      = "WhereIsAI/UAE-Large-V1"
	ModelBAAIBgeLargeEnV1_5                       = "BAAI/bge-large-en-v1.5"
	ModelBAAIBgeBaseEnV1_5                        = "BAAI/bge-base-en-v1.5"
	ModelSentenceTransformersMsmarcoBertBaseDotV5 = "sentence-transformers/msmarco-bert-base-dot-v5"
	ModelBertBaseUncased                          = "bert-base-uncased"
)

// Image model IDs (TogetherAIImageModelId in togetherai/src/togetherai-image-settings.ts).
const (
	ModelStabilityaiStableDiffusionXlBase1_0 = "stabilityai/stable-diffusion-xl-base-1.0"
	ModelBlackForestLabsFLUX1Dev             = "black-forest-labs/FLUX.1-dev"
	ModelBlackForestLabsFLUX1DevLora         = "black-forest-labs/FLUX.1-dev-lora"
	ModelBlackForestLabsFLUX1Schnell         = "black-forest-labs/FLUX.1-schnell"
	ModelBlackForestLabsFLUX1Canny           = "black-forest-labs/FLUX.1-canny"
	ModelBlackForestLabsFLUX1Depth           = "black-forest-labs/FLUX.1-depth"
	ModelBlackForestLabsFLUX1Redux           = "black-forest-labs/FLUX.1-redux"
	ModelBlackForestLabsFLUX1_1Pro           = "black-forest-labs/FLUX.1.1-pro"
	ModelBlackForestLabsFLUX1Pro             = "black-forest-labs/FLUX.1-pro"
	ModelBlackForestLabsFLUX1SchnellFree     = "black-forest-labs/FLUX.1-schnell-Free"
	ModelBlackForestLabsFLUX1KontextPro      = "black-forest-labs/FLUX.1-kontext-pro"
	ModelBlackForestLabsFLUX1KontextMax      = "black-forest-labs/FLUX.1-kontext-max"
	ModelBlackForestLabsFLUX1KontextDev      = "black-forest-labs/FLUX.1-kontext-dev"
)

// Reranking model IDs (TogetherAIRerankingModelId in togetherai/src/reranking/togetherai-reranking-model-options.ts).
const (
	ModelSalesforceLlamaRankV1          = "Salesforce/Llama-Rank-v1"
	ModelMixedbreadAiMxbaiRerankLargeV2 = "mixedbread-ai/Mxbai-Rerank-Large-V2"
)
