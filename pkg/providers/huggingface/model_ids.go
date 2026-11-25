package huggingface

// Model ID constants mirror the TypeScript SDK model ID types at 0f3a85f92d (TS catalog
// refactors eaaead397d, a60ddb4a8f, c296443f12). Any model ID string is
// accepted; these constants only document the known IDs.

// Responses model IDs (HuggingFaceResponsesModelId in huggingface/src/responses/huggingface-responses-settings.ts).
const (
	ModelMetaLlamaLlama3_1_8BInstruct              = "meta-llama/Llama-3.1-8B-Instruct"
	ModelMetaLlamaLlama3_1_70BInstruct             = "meta-llama/Llama-3.1-70B-Instruct"
	ModelMetaLlamaLlama3_1_405BInstruct            = "meta-llama/Llama-3.1-405B-Instruct"
	ModelMetaLlamaLlama3_3_70BInstruct             = "meta-llama/Llama-3.3-70B-Instruct"
	ModelMetaLlamaMetaLlama3_8BInstruct            = "meta-llama/Meta-Llama-3-8B-Instruct"
	ModelMetaLlamaMetaLlama3_70BInstruct           = "meta-llama/Meta-Llama-3-70B-Instruct"
	ModelMetaLlamaLlama3_2_3BInstruct              = "meta-llama/Llama-3.2-3B-Instruct"
	ModelMetaLlamaLlama4Maverick17B128EInstruct    = "meta-llama/Llama-4-Maverick-17B-128E-Instruct"
	ModelMetaLlamaLlama4Maverick17B128EInstructFP8 = "meta-llama/Llama-4-Maverick-17B-128E-Instruct-FP8"
	ModelMetaLlamaLlamaGuard4_12B                  = "meta-llama/Llama-Guard-4-12B"
	ModelDeepseekAiDeepSeekV3_1                    = "deepseek-ai/DeepSeek-V3.1"
	ModelDeepseekAiDeepSeekV3_0324                 = "deepseek-ai/DeepSeek-V3-0324"
	ModelDeepseekAiDeepSeekR1                      = "deepseek-ai/DeepSeek-R1"
	ModelDeepseekAiDeepSeekR1_0528                 = "deepseek-ai/DeepSeek-R1-0528"
	ModelDeepseekAiDeepSeekR1DistillQwen1_5B       = "deepseek-ai/DeepSeek-R1-Distill-Qwen-1.5B"
	ModelDeepseekAiDeepSeekR1DistillQwen7B         = "deepseek-ai/DeepSeek-R1-Distill-Qwen-7B"
	ModelDeepseekAiDeepSeekR1DistillQwen14B        = "deepseek-ai/DeepSeek-R1-Distill-Qwen-14B"
	ModelDeepseekAiDeepSeekR1DistillQwen32B        = "deepseek-ai/DeepSeek-R1-Distill-Qwen-32B"
	ModelDeepseekAiDeepSeekR1DistillLlama8B        = "deepseek-ai/DeepSeek-R1-Distill-Llama-8B"
	ModelDeepseekAiDeepSeekR1DistillLlama70B       = "deepseek-ai/DeepSeek-R1-Distill-Llama-70B"
	ModelDeepseekAiDeepSeekProverV2_671B           = "deepseek-ai/DeepSeek-Prover-V2-671B"
	ModelQwenQwen3_32B                             = "Qwen/Qwen3-32B"
	ModelQwenQwen3_14B                             = "Qwen/Qwen3-14B"
	ModelQwenQwen3_8B                              = "Qwen/Qwen3-8B"
	ModelQwenQwen3_4B                              = "Qwen/Qwen3-4B"
	ModelQwenQwen3Coder480BA35BInstruct            = "Qwen/Qwen3-Coder-480B-A35B-Instruct"
	ModelQwenQwen3Coder480BA35BInstructFP8         = "Qwen/Qwen3-Coder-480B-A35B-Instruct-FP8"
	ModelQwenQwen3_30BA3B                          = "Qwen/Qwen3-30B-A3B"
	ModelQwenQwen2_5VL7BInstruct                   = "Qwen/Qwen2.5-VL-7B-Instruct"
	ModelQwenQwen2_5_7BInstruct                    = "Qwen/Qwen2.5-7B-Instruct"
	ModelQwenQwen2_5Coder7BInstruct                = "Qwen/Qwen2.5-Coder-7B-Instruct"
	ModelQwenQwen2_5Coder32BInstruct               = "Qwen/Qwen2.5-Coder-32B-Instruct"
	ModelGoogleGemma2_9bIt                         = "google/gemma-2-9b-it"
	ModelGoogleGemma3_27bIt                        = "google/gemma-3-27b-it"
	ModelMoonshotaiKimiK2Instruct                  = "moonshotai/Kimi-K2-Instruct"
)
