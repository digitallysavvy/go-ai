package zai

// Language model ID constants for Z.AI chat models, from the official
// OpenAPI 1.0.0 specification (https://docs.z.ai/openapi.json, 2026-08-26).
// Mirrors packages/zai/src/zai-chat-options.ts (ZaiChatModelId) at
// ai@7.0.113. Any model ID string is accepted; these constants exist for
// convenience and IDE support.
const (
	ModelGLM53                    = "glm-5.3"
	ModelGLM52                    = "glm-5.2"
	ModelGLM51                    = "glm-5.1"
	ModelGLM5Turbo                = "glm-5-turbo"
	ModelGLM5                     = "glm-5"
	ModelGLM47                    = "glm-4.7"
	ModelGLM47Flash               = "glm-4.7-flash"
	ModelGLM47FlashX              = "glm-4.7-flashx"
	ModelGLM46                    = "glm-4.6"
	ModelGLM45                    = "glm-4.5"
	ModelGLM45Air                 = "glm-4.5-air"
	ModelGLM45X                   = "glm-4.5-x"
	ModelGLM45AirX                = "glm-4.5-airx"
	ModelGLM45Flash               = "glm-4.5-flash"
	ModelGLM432B0414128K          = "glm-4-32b-0414-128k"
	ModelGLM53Flash               = "glm-5.3-flash"
	ModelGLM5VTurbo               = "glm-5v-turbo"
	ModelGLM46V                   = "glm-4.6v"
	ModelGLM46VFlash              = "glm-4.6v-flash"
	ModelGLM46VFlashX             = "glm-4.6v-flashx"
	ModelGLM45V                   = "glm-4.5v"
	ModelAutoGLMPhoneMultilingual = "autoglm-phone-multilingual"
)
