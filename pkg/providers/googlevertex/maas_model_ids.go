package googlevertex

// GoogleVertexMaasModelID identifies a Vertex MaaS partner/open model.
type GoogleVertexMaasModelID string

const (
	MaaSModelDeepSeekR1             GoogleVertexMaasModelID = "deepseek-ai/deepseek-r1-0528-maas"
	MaaSModelDeepSeekV31            GoogleVertexMaasModelID = "deepseek-ai/deepseek-v3.1-maas"
	MaaSModelDeepSeekV32            GoogleVertexMaasModelID = "deepseek-ai/deepseek-v3.2-maas"
	MaaSModelGPTOSS120B             GoogleVertexMaasModelID = "openai/gpt-oss-120b-maas"
	MaaSModelGPTOSS20B              GoogleVertexMaasModelID = "openai/gpt-oss-20b-maas"
	MaaSModelLlama4Maverick17B      GoogleVertexMaasModelID = "meta/llama-4-maverick-17b-128e-instruct-maas"
	MaaSModelLlama4Scout17B         GoogleVertexMaasModelID = "meta/llama-4-scout-17b-16e-instruct-maas"
	MaaSModelMiniMaxM2              GoogleVertexMaasModelID = "minimax/minimax-m2-maas"
	MaaSModelQwen3Coder480B         GoogleVertexMaasModelID = "qwen/qwen3-coder-480b-a35b-instruct-maas"
	MaaSModelQwen3Next80B           GoogleVertexMaasModelID = "qwen/qwen3-next-80b-a3b-instruct-maas"
	MaaSModelQwen3Next80BThink      GoogleVertexMaasModelID = "qwen/qwen3-next-80b-a3b-thinking-maas"
	MaaSModelKimiK2Thinking         GoogleVertexMaasModelID = "moonshotai/kimi-k2-thinking-maas"
	MaaSModelGrok420Reasoning       GoogleVertexMaasModelID = "xai/grok-4.20-reasoning"
	MaaSModelGrok420NonReasoning    GoogleVertexMaasModelID = "xai/grok-4.20-non-reasoning"
	MaaSModelGrok41FastReasoning    GoogleVertexMaasModelID = "xai/grok-4.1-fast-reasoning"
	MaaSModelGrok41FastNonReasoning GoogleVertexMaasModelID = "xai/grok-4.1-fast-non-reasoning"
)
