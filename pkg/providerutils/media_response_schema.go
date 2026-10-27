package providerutils

import (
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"net/url"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/internal/zodlite"
)

// This file ports the zod response schemas the TS embedding, reranking,
// image and transcription models apply to every successful body through
// createJsonResponseHandler. A body that does not match is an APICallError
// "Invalid JSON response" whose cause is the TypeValidationError.

// MediaResponseSchema names a TS response schema.
type MediaResponseSchema string

// Embedding, reranking, image and transcription response schemas.
const (
	// SchemaOpenAIEmbedding is openaiTextEmbeddingResponseSchema (also mistral).
	SchemaOpenAIEmbedding MediaResponseSchema = "openai-embedding"
	// SchemaCompatEmbedding is the openai-compatible embedding schema.
	SchemaCompatEmbedding MediaResponseSchema = "compat-embedding"
	// SchemaVoyageEmbedding is voyageEmbeddingResponseSchema.
	SchemaVoyageEmbedding MediaResponseSchema = "voyage-embedding"
	// SchemaPerplexityEmbedding is perplexityEmbeddingResponseSchema.
	SchemaPerplexityEmbedding MediaResponseSchema = "perplexity-embedding"
	// SchemaAlibabaEmbedding is alibabaTextEmbeddingResponseSchema.
	SchemaAlibabaEmbedding MediaResponseSchema = "alibaba-embedding"
	// SchemaCohereRerank is cohereRerankingResponseSchema.
	SchemaCohereRerank MediaResponseSchema = "cohere-rerank"
	// SchemaVoyageRerank is voyageRerankingResponseSchema.
	SchemaVoyageRerank MediaResponseSchema = "voyage-rerank"
	// SchemaTogetherRerank is togetheraiRerankingResponseSchema.
	SchemaTogetherRerank MediaResponseSchema = "together-rerank"

	SchemaOpenAIImage    MediaResponseSchema = "openai-image"
	SchemaCompatImage    MediaResponseSchema = "compat-image"
	SchemaTogetherImage  MediaResponseSchema = "together-image"
	SchemaDeepInfraImage MediaResponseSchema = "deepinfra-image"
	SchemaDeepInfraEdit  MediaResponseSchema = "deepinfra-edit"
	SchemaXAIImage       MediaResponseSchema = "xai-image"
	SchemaByteDanceImage MediaResponseSchema = "bytedance-image"
	SchemaQuiverSVG      MediaResponseSchema = "quiverai-svg"
	SchemaTopazSubmit    MediaResponseSchema = "topaz-submit"
	// SchemaProdiaJob is prodiaJobResultSchema (the multipart "job" part).
	SchemaProdiaJob       MediaResponseSchema = "prodia-job"
	SchemaTopazStatus     MediaResponseSchema = "topaz-status"
	SchemaTopazDownload   MediaResponseSchema = "topaz-download"
	SchemaAzureMAIImage   MediaResponseSchema = "azure-mai-image"
	SchemaOpenAITranscr   MediaResponseSchema = "openai-transcription"
	SchemaGroqTranscr     MediaResponseSchema = "groq-transcription"
	SchemaMistralTranscr  MediaResponseSchema = "mistral-transcription"
	SchemaElevenLabsTr    MediaResponseSchema = "elevenlabs-transcription"
	SchemaFishAudioTr     MediaResponseSchema = "fishaudio-transcription"
	SchemaRevAIJob        MediaResponseSchema = "revai-job"
	SchemaRevAITranscript MediaResponseSchema = "revai-transcript"
	SchemaXAITranscr      MediaResponseSchema = "xai-transcription"
	SchemaAzureSpeechTr   MediaResponseSchema = "azure-speech-transcription"
	// SchemaFalImage is falImageResponseSchema.
	SchemaFalImage MediaResponseSchema = "fal-image"
	// Gateway embedding, image, reranking, speech and transcription schemas.
	SchemaGatewayEmbedding     MediaResponseSchema = "gateway-embedding"
	SchemaGatewayImage         MediaResponseSchema = "gateway-image"
	SchemaGatewayReranking     MediaResponseSchema = "gateway-reranking"
	SchemaGatewaySpeech        MediaResponseSchema = "gateway-speech"
	SchemaGatewayTranscription MediaResponseSchema = "gateway-transcription"
	// AssemblyAI upload/submit (createJsonResponseHandler) and transcript
	// (a bare schema.parse) bodies; Gladia upload, init and result bodies.
	SchemaAssemblyAIUpload     MediaResponseSchema = "assemblyai-upload"
	SchemaAssemblyAISubmit     MediaResponseSchema = "assemblyai-submit"
	SchemaAssemblyAITranscript MediaResponseSchema = "assemblyai-transcript"
	SchemaGladiaUpload         MediaResponseSchema = "gladia-upload"
	SchemaGladiaInit           MediaResponseSchema = "gladia-init"
	SchemaGladiaResult         MediaResponseSchema = "gladia-result"
	// SchemaCartesiaTranscription and SchemaCartesiaAccessToken are
	// cartesiaTranscriptionResponseSchema and cartesiaAccessTokenResponseSchema.
	SchemaCartesiaTranscription MediaResponseSchema = "cartesia-transcription"
	SchemaCartesiaAccessToken   MediaResponseSchema = "cartesia-access-token"
	// SchemaKlingAICreate and SchemaKlingAIStatus are klingaiCreateTaskSchema
	// and klingaiTaskStatusSchema.
	SchemaKlingAICreate MediaResponseSchema = "klingai-create"
	SchemaKlingAIStatus MediaResponseSchema = "klingai-status"
	// SchemaMiniMaxCreate and SchemaMiniMaxStatus are
	// minimaxCreateVideoResponseSchema and minimaxVideoStatusResponseSchema.
	SchemaMiniMaxCreate MediaResponseSchema = "minimax-create"
	SchemaMiniMaxStatus MediaResponseSchema = "minimax-status"
	// Video task bodies: ByteDance, Alibaba, Topaz.
	SchemaByteDanceTask    MediaResponseSchema = "bytedance-task"
	SchemaByteDanceStatus  MediaResponseSchema = "bytedance-status"
	SchemaAlibabaVideoTask MediaResponseSchema = "alibaba-video-task"
	SchemaAlibabaVideoStat MediaResponseSchema = "alibaba-video-status"
	SchemaTopazVideoCreate MediaResponseSchema = "topaz-video-create"
	SchemaTopazVideoStatus MediaResponseSchema = "topaz-video-status"
	// xAI and HeyGen video task bodies.
	SchemaXAIVideoCreate    MediaResponseSchema = "xai-video-create"
	SchemaXAIVideoStatus    MediaResponseSchema = "xai-video-status"
	SchemaHeyGenVideoCreate MediaResponseSchema = "heygen-video-create"
	SchemaHeyGenVideoStatus MediaResponseSchema = "heygen-video-status"
	// Files, skills and batch APIs.
	SchemaOpenAIFile      MediaResponseSchema = "openai-file"
	SchemaFileDelete      MediaResponseSchema = "file-delete"
	SchemaOpenAISkill     MediaResponseSchema = "openai-skill"
	SchemaOpenAIBatch     MediaResponseSchema = "openai-batch"
	SchemaOpenAIBatchList MediaResponseSchema = "openai-batch-list"
	SchemaXAIFile         MediaResponseSchema = "xai-file"
	SchemaXAIBatch        MediaResponseSchema = "xai-batch"
	SchemaXAIBatchList    MediaResponseSchema = "xai-batch-list"
	SchemaXAIBatchResults MediaResponseSchema = "xai-batch-results"
	// SchemaXAIBatchText is xaiBatchTextResponseSchema, applied to one
	// result's chat_get_completion.
	SchemaXAIBatchText      MediaResponseSchema = "xai-batch-text"
	SchemaDeepSeekFile      MediaResponseSchema = "deepseek-file"
	SchemaGatewayBatchStart MediaResponseSchema = "gateway-batch-start"
	SchemaGatewayBatchState MediaResponseSchema = "gateway-batch-status"
	// SchemaGatewayVideoStart and SchemaGatewayVideoStatus are the
	// /video-model/start and /video-model/status responses
	// (gatewayVideoStartResponseSchema, gatewayVideoStatusResponseSchema).
	SchemaGatewayVideoStart  MediaResponseSchema = "gateway-video-start"
	SchemaGatewayVideoStatus MediaResponseSchema = "gateway-video-status"
	// SchemaGatewayBatchItem is one /batch/results NDJSON line
	// (gatewayBatchItemResultLineSchema: type text, id, status; extra keys pass).
	SchemaGatewayBatchItem MediaResponseSchema = "gateway-batch-item"
	// SchemaGatewayDecision is gatewayDecisionResponseSchema.
	SchemaGatewayDecision MediaResponseSchema = "gateway-decision"
	// SchemaMistralSpeech and SchemaXAISpeechTimestamps are the JSON speech
	// bodies (mistralSpeechResponseSchema, xaiSpeechTimestampsResponseSchema).
	SchemaMistralSpeech       MediaResponseSchema = "mistral-speech"
	SchemaXAISpeechTimestamps MediaResponseSchema = "xai-speech-timestamps"
	// SchemaGatewayCredits and SchemaGatewayGenerationInfo are
	// gatewayCreditsResponseSchema and gatewayGenerationInfoResponseSchema.
	SchemaGatewayCredits        MediaResponseSchema = "gateway-credits"
	SchemaGatewayGenerationInfo MediaResponseSchema = "gateway-generation-info"
	// SchemaFalSpeech is falSpeechResponseSchema.
	SchemaFalSpeech  MediaResponseSchema = "fal-speech"
	SchemaFalJob     MediaResponseSchema = "fal-job"
	SchemaFalTranscr MediaResponseSchema = "fal-transcription"
)

// zIntNonneg is z.number().int().nonnegative().
func zIntNonneg(v interface{}, present bool, path []interface{}) *zodlite.Issue {
	f, ok := v.(float64)
	if !ok {
		return zodlite.Invalid("number", v, present, path)
	}
	if f != math.Trunc(f) {
		return &zodlite.Issue{Path: path, Code: "invalid_type", Expected: "int", Message: "Invalid input: expected int, received number"}
	}
	if f < 0 {
		return &zodlite.Issue{Path: path, Code: "too_small", Message: "Too small: expected number to be >=0"}
	}
	return nil
}

// zURLStr is z.string().url(): a string `new URL()` accepts.
func zURLStr(v interface{}, present bool, path []interface{}) *zodlite.Issue {
	str, ok := v.(string)
	if !ok {
		return zodlite.Invalid("string", v, present, path)
	}
	if u, err := url.Parse(str); err != nil || u.Scheme == "" || (u.Host == "" && u.Opaque == "" && !strings.HasPrefix(str, u.Scheme+":/")) {
		return &zodlite.Issue{Path: path, Code: "invalid_format", Message: "Invalid URL"}
	}
	return nil
}

// zNonEmptyStr is z.string().min(1).
func zNonEmptyStr(v interface{}, present bool, path []interface{}) *zodlite.Issue {
	s, ok := v.(string)
	if !ok {
		return zodlite.Invalid("string", v, present, path)
	}
	if s == "" {
		return &zodlite.Issue{Path: path, Code: "too_small", Message: "Too small: expected string to have >=1 characters"}
	}
	return nil
}

// zNonEmptyArr is z.array(n).min(1).
func zNonEmptyArr(n zNode) zNode {
	arr := zArr(n)
	return func(v interface{}, present bool, path []interface{}) *zodlite.Issue {
		if bad := arr(v, present, path); bad != nil {
			return bad
		}
		if len(v.([]interface{})) == 0 {
			return &zodlite.Issue{Path: path, Code: "too_small", Message: "Too small: expected array to have >=1 items"}
		}
		return nil
	}
}

// zTuple2Num is z.tuple([z.number(), z.number()]).
func zTuple2Num(v interface{}, present bool, path []interface{}) *zodlite.Issue {
	a, ok := v.([]interface{})
	if !ok {
		return zodlite.Invalid("array", v, present, path)
	}
	if len(a) != 2 {
		return &zodlite.Issue{Path: path, Code: "invalid_type", Message: "Invalid input: expected array of 2 numbers"}
	}
	for i, e := range a {
		if bad := zNum(e, true, append(append([]interface{}{}, path...), i)); bad != nil {
			return bad
		}
	}
	return nil
}

func zNumVec() zNode { return zArr(zNum) }

var mediaSchemas = func() map[MediaResponseSchema]zNode {
	openaiEmb := zObj(fl("data", zArr(zObj(fl("embedding", zNumVec())))), fl("usage", zNullish(zObj(fl("prompt_tokens", zNum)))))
	rerankRow := zObj(fl("index", zNum), fl("relevance_score", zNum))
	segmentOpenAI := zObj(fl("id", zNum), fl("seek", zNum), fl("start", zNum), fl("end", zNum), fl("text", zStr),
		fl("tokens", zArr(zNum)), fl("temperature", zNum), fl("avg_logprob", zNum), fl("compression_ratio", zNum), fl("no_speech_prob", zNum))
	details := zOptional(zStr)
	gatewayWarning := zDiscType(map[string]zNode{
		"unsupported":   zodlite.Typed("unsupported", fl("feature", zStr), fl("details", details)),
		"compatibility": zodlite.Typed("compatibility", fl("feature", zStr), fl("details", details)),
		"deprecated":    zodlite.Typed("deprecated", fl("setting", zStr), fl("message", zStr)),
		"other":         zodlite.Typed("other", fl("message", zStr)),
	})
	assemblyStatus := zodlite.Enum("queued", "processing", "completed", "error")
	assemblyWord := zObj(fl("start", zNum), fl("end", zNum), fl("text", zStr), fl("confidence", nNum), fl("speaker", nStr), fl("channel", nStr))
	openAIBatch := zObj(fl("id", zStr), fl("status", zStr), fl("output_file_id", nStr), fl("error_file_id", nStr),
		fl("created_at", nNum), fl("expires_at", nNum),
		fl("request_counts", zNullish(zObj(fl("total", nNum), fl("completed", nNum), fl("failed", nNum)))),
		fl("errors", zNullish(zObj(fl("data", zNullish(zArr(zObj(fl("code", nStr), fl("message", nStr)))))))))
	xaiBatch := zObj(fl("batch_id", zStr), fl("name", nStr), fl("create_time", nStr), fl("expire_time", nStr), fl("cancel_time", nStr),
		fl("cancel_by_xai_message", nStr),
		fl("state", zNullish(zObj(fl("num_requests", nNum), fl("num_pending", nNum), fl("num_success", nNum),
			fl("num_error", nNum), fl("num_cancelled", nNum)))))
	gatewayBatchStatus := []zFld{
		fl("status", zodlite.Enum("completed", "failed", "pending")), fl("rawStatus", nStr),
		fl("requestCounts", zNullish(zObj(fl("total", nNum), fl("pending", nNum), fl("completed", nNum), fl("failed", nNum)))),
		fl("error", zNullish(zObj(fl("message", zStr), fl("type", nStr), fl("code", nStr), fl("statusCode", nNum)))),
		fl("createdAt", nStr), fl("expiresAt", nStr),
		fl("providerMetadata", zNullish(zRec(zRec(zAny)))),
	}
	gatewayVideoData := zodlite.Union(
		zodlite.Typed("url", fl("url", zStr), fl("mediaType", zStr)),
		zodlite.Typed("base64", fl("data", zStr), fl("mediaType", zStr)))
	gatewayVideoMeta := zNullish(zRec(zodlite.Record))
	gatewayVideoWarnings := zNullish(zArr(gatewayWarning))
	gatewayVideoStatusVariants := map[string]zNode{
		"pending":   zObj(fl("warnings", gatewayVideoWarnings), fl("providerMetadata", gatewayVideoMeta)),
		"completed": zObj(fl("videos", zArr(gatewayVideoData)), fl("warnings", gatewayVideoWarnings), fl("providerMetadata", gatewayVideoMeta)),
		"error":     zObj(fl("error", zStr), fl("providerMetadata", gatewayVideoMeta)),
		"cancelled": zObj(fl("providerMetadata", gatewayVideoMeta)),
	}
	gatewayVideoStatus := func(v interface{}, present bool, path []interface{}) *zodlite.Issue {
		m, ok := v.(map[string]interface{})
		if !ok {
			return zodlite.Invalid("object", v, present, path)
		}
		status, _ := m["status"].(string)
		variant, known := gatewayVideoStatusVariants[status]
		if !known {
			return &zodlite.Issue{Path: append(append([]interface{}{}, path...), "status"), Code: "invalid_union", Discriminator: "status", Message: "Invalid input"}
		}
		return variant(v, present, path)
	}
	word := zObj(fl("word", zStr), fl("start", zNum), fl("end", zNum))
	b64Data := zObj(fl("data", zArr(zObj(fl("b64_json", zStr)))))
	// compatImageItem is z.object({b64_json: nullish, url: nullish}).refine(
	// b64_json != null || url != null) from openaiCompatibleImageResponseSchema.
	compatImageBase := zObj(fl("b64_json", nStr), fl("url", nStr))
	compatImageItem := func(v interface{}, present bool, path []interface{}) *zodlite.Issue {
		if bad := compatImageBase(v, present, path); bad != nil {
			return bad
		}
		m := v.(map[string]interface{})
		if m["b64_json"] == nil && m["url"] == nil {
			return &zodlite.Issue{Path: path, Code: "custom", Message: "Expected b64_json or url in the image response"}
		}
		return nil
	}
	return map[MediaResponseSchema]zNode{
		SchemaOpenAIEmbedding: openaiEmb,
		SchemaCompatEmbedding: zObj(fl("data", zArr(zObj(fl("embedding", zNumVec())))), fl("usage", zNullish(zObj(fl("prompt_tokens", zNum)))),
			fl("providerMetadata", zOptional(zRec(zodlite.Record)))),
		SchemaVoyageEmbedding: zObj(fl("data", zArr(zObj(fl("embedding", zNumVec()), fl("index", zNum)))), fl("model", zStr),
			fl("usage", zNullish(zObj(fl("total_tokens", zNum))))),
		SchemaPerplexityEmbedding: zObj(fl("data", zArr(zObj(fl("embedding", zStr)))),
			fl("usage", zNullish(zObj(fl("prompt_tokens", zNum),
				fl("cost", zNullish(zObj(fl("input_cost", nNum), fl("total_cost", nNum), fl("currency", nStr)))))))),
		SchemaAlibabaEmbedding: zObj(fl("output", zObj(fl("embeddings", zArr(zObj(fl("embedding", zNumVec()), fl("text_index", zNum),
			fl("sparse_embedding", zNullish(zArr(zObj(fl("index", zNum), fl("value", zNum), fl("token", nStr)))))))))),
			fl("usage", zNullish(zObj(fl("total_tokens", zNum))))),
		SchemaCohereRerank:   zObj(fl("id", nStr), fl("results", zArr(rerankRow)), fl("meta", zAny)),
		SchemaVoyageRerank:   zObj(fl("data", zArr(rerankRow))),
		SchemaTogetherRerank: zObj(fl("id", nStr), fl("model", nStr), fl("results", zArr(rerankRow)), fl("usage", zObj(fl("prompt_tokens", zNum), fl("completion_tokens", zNum), fl("total_tokens", zNum)))),

		SchemaOpenAIImage: zObj(fl("created", nNum),
			fl("data", zArr(zObj(fl("b64_json", zStr), fl("revised_prompt", nStr)))),
			fl("background", nStr), fl("output_format", nStr), fl("size", nStr), fl("quality", nStr),
			fl("usage", zNullish(zObj(fl("input_tokens", nNum), fl("output_tokens", nNum), fl("total_tokens", nNum),
				fl("input_tokens_details", zNullish(zObj(fl("image_tokens", nNum), fl("text_tokens", nNum)))))))),
		SchemaCompatImage: zObj(fl("data", zArr(compatImageItem)),
			fl("usage", zNullish(zObj(fl("input_tokens", nNum), fl("output_tokens", nNum), fl("total_tokens", nNum))))),
		SchemaTogetherImage:  b64Data,
		SchemaDeepInfraImage: zObj(fl("images", zArr(zStr))),
		SchemaDeepInfraEdit:  b64Data,
		SchemaXAIImage: zObj(fl("data", zArr(zObj(fl("url", nStr), fl("b64_json", nStr), fl("revised_prompt", nStr), fl("respect_moderation", zNullish(zodlite.Boolean))))),
			fl("usage", zNullish(zObj(fl("cost_in_usd_ticks", nNum))))),
		SchemaByteDanceImage: zObj(fl("data", zArr(zObj(fl("b64_json", zStr)))),
			fl("usage", zNullish(zObj(fl("output_tokens", nNum), fl("total_tokens", nNum))))),
		SchemaQuiverSVG: zObj(fl("id", zNonEmptyStr), fl("created", zIntNonneg),
			fl("data", zNonEmptyArr(zObj(fl("svg", zNonEmptyStr), fl("mime_type", zodlite.Literal("image/svg+xml")),
				fl("loop_period_ms", zNullish(zIntNonneg)), fl("opening_animation_ms", zNullish(zIntNonneg))))),
			fl("usage", zNullish(zObj(fl("total_tokens", zIntNonneg), fl("input_tokens", zIntNonneg), fl("output_tokens", zIntNonneg)))),
			fl("credits", zNullish(zIntNonneg))),
		SchemaProdiaJob: zObj(fl("id", zStr), fl("created_at", zOptional(zStr)), fl("updated_at", zOptional(zStr)), fl("expires_at", zOptional(zStr)),
			fl("state", zOptional(zObj(fl("current", zStr)))),
			fl("config", zOptional(zObj(fl("seed", zOptional(zNum))))),
			fl("metrics", zOptional(zObj(fl("elapsed", zOptional(zNum)), fl("ips", zOptional(zNum))))),
			fl("price", zNullish(zObj(fl("product", zStr), fl("dollars", zNum))))),
		SchemaTopazSubmit:   zObj(fl("process_id", nStr)),
		SchemaTopazStatus:   zObj(fl("status", nStr), fl("credits", nNum), fl("output_width", nNum), fl("output_height", nNum), fl("output_format", nStr)),
		SchemaTopazDownload: zObj(fl("download_url", nStr)),
		SchemaAzureMAIImage: zObj(fl("created", nNum), fl("size", nStr),
			fl("usage", zNullish(zObj(fl("num_input_text_tokens", nNum), fl("num_input_image_tokens", nNum), fl("num_output_tokens", nNum)))),
			fl("data", zArr(zObj(fl("b64_json", zStr))))),

		SchemaOpenAITranscr: zObj(fl("text", zStr), fl("language", nStr), fl("languages", zNullish(zArr(zObj(fl("code", zStr))))), fl("duration", nNum),
			fl("words", zNullish(zArr(word))),
			fl("segments", zNullish(zArr(zUnionOf(segmentOpenAI, zObj(fl("type", zodlite.Literal("transcript.text.segment")),
				fl("id", zStr), fl("start", zNum), fl("end", zNum), fl("text", zStr), fl("speaker", zStr)))))),
			fl("usage", zNullish(zRec(zAny)))),
		SchemaGroqTranscr: zObj(fl("text", zStr), fl("x_groq", zObj(fl("id", zStr))), fl("task", nStr), fl("language", nStr), fl("duration", nNum),
			fl("segments", zNullish(zArr(segmentOpenAI))), fl("words", zNullish(zArr(word)))),
		SchemaMistralTranscr: zObj(fl("model", zStr), fl("text", zStr), fl("language", nStr),
			fl("segments", zNullish(zArr(zObj(fl("type", zNullish(zodlite.Literal("transcription_segment"))), fl("text", zStr),
				fl("start", zNum), fl("end", zNum), fl("score", nNum), fl("speaker_id", nStr))))),
			fl("usage", zNullish(zObj(fl("prompt_tokens", nNum), fl("completion_tokens", nNum), fl("total_tokens", nNum),
				fl("prompt_audio_seconds", nNum), fl("request_count", nNum))))),
		SchemaElevenLabsTr: zObj(fl("audio_duration_secs", nNum), fl("language_code", zStr), fl("language_probability", zNum), fl("text", zStr),
			fl("words", zNullish(zArr(zObj(fl("text", zStr), fl("type", zodlite.Enum("word", "spacing", "audio_event")),
				fl("start", nNum), fl("end", nNum), fl("speaker_id", nStr),
				fl("characters", zNullish(zArr(zObj(fl("text", zStr), fl("start", nNum), fl("end", nNum)))))))))),
		SchemaFishAudioTr: zObj(fl("text", zStr), fl("language", nStr), fl("language_code", nStr), fl("duration", nNum),
			fl("segments", zNullish(zArr(zObj(fl("text", zStr), fl("start", zNum), fl("end", zNum)))))),
		SchemaRevAIJob: zObj(fl("id", nStr), fl("status", nStr), fl("language", nStr)),
		SchemaRevAITranscript: zObj(fl("monologues", zNullish(zArr(zObj(fl("elements", zNullish(zArr(zObj(
			fl("type", nStr), fl("value", nStr), fl("ts", nNum), fl("end_ts", nNum)))))))))),
		SchemaXAITranscr: zObj(fl("text", zStr), fl("language", nStr), fl("duration", nNum),
			fl("words", zNullish(zArr(zObj(fl("text", zStr), fl("start", zNum), fl("end", zNum), fl("speaker", nNum)))))),
		SchemaAzureSpeechTr: zObj(fl("combinedPhrases", zArr(zObj(fl("text", zStr)))), fl("durationMilliseconds", nNum),
			fl("phrases", zNullish(zArr(zObj(fl("text", zStr), fl("offsetMilliseconds", nNum), fl("durationMilliseconds", nNum),
				fl("locale", nStr), fl("speaker", nNum), fl("confidence", nNum),
				fl("words", zNullish(zArr(zObj(fl("text", zStr), fl("offsetMilliseconds", nNum), fl("durationMilliseconds", nNum)))))))))),
		SchemaFalImage: func() zNode {
			falImage := zObj(fl("url", zStr), fl("width", nNum), fl("height", nNum), fl("content_type", nStr), fl("file_name", nStr),
				fl("file_data", zOptional(zStr)), fl("file_size", nNum))
			lora := zObj(fl("url", zStr), fl("content_type", zOptional(zStr)), fl("file_name", zOptional(zNullable(zStr))),
				fl("file_data", zOptional(zStr)), fl("file_size", zOptional(zNullable(zNum))))
			common := []zFld{
				fl("timings", zOptional(zObj(fl("inference", zOptional(zNum))))), fl("seed", zOptional(zNum)),
				fl("has_nsfw_concepts", zOptional(zArr(zodlite.Boolean))), fl("prompt", zOptional(zStr)),
				fl("nsfw_content_detected", zOptional(zArr(zodlite.Boolean))), fl("num_inference_steps", zOptional(zNum)),
				fl("debug_latents", zOptional(lora)), fl("debug_per_pass_latents", zOptional(lora)),
			}
			return zUnionOf(
				zObj(append(append([]zFld{}, common...), fl("images", zArr(falImage)))...),
				zObj(append(append([]zFld{}, common...), fl("image", falImage))...),
			)
		}(),
		SchemaGatewayEmbedding: zObj(fl("embeddings", zArr(zNumVec())), fl("usage", zNullish(zObj(fl("tokens", zNum)))),
			fl("warnings", zOptional(zArr(gatewayWarning))), fl("providerMetadata", zOptional(zRec(zRec(zAny))))),
		SchemaGatewayImage: zObj(fl("images", zArr(zStr)), fl("isRetryable", zOptional(zodlite.Boolean)),
			fl("warnings", zOptional(zArr(gatewayWarning))),
			fl("providerMetadata", zOptional(zRec(zObj(fl("images", zOptional(zArr(zAny))))))),
			fl("usage", zOptional(zObj(fl("inputTokens", nNum), fl("outputTokens", nNum), fl("totalTokens", nNum))))),
		SchemaGatewayReranking: zObj(fl("ranking", zArr(zObj(fl("index", zNum), fl("relevanceScore", zNum)))),
			fl("warnings", zOptional(zArr(gatewayWarning))), fl("providerMetadata", zOptional(zRec(zRec(zAny))))),
		SchemaGatewaySpeech: zObj(fl("audio", zStr), fl("warnings", zOptional(zArr(gatewayWarning))),
			fl("usage", zOptional(zRec(zAny))), fl("providerMetadata", zOptional(zRec(zodlite.Record)))),
		SchemaGatewayTranscription: zObj(fl("text", zStr),
			fl("segments", zOptional(zArr(zObj(fl("text", zStr), fl("startSecond", zNum), fl("endSecond", zNum))))),
			fl("language", nStr), fl("durationInSeconds", nNum), fl("warnings", zOptional(zArr(gatewayWarning))),
			fl("usage", zOptional(zRec(zAny))), fl("providerMetadata", zOptional(zRec(zodlite.Record)))),
		SchemaAssemblyAIUpload: zObj(fl("upload_url", zStr)),
		SchemaAssemblyAISubmit: zObj(fl("id", zStr), fl("status", assemblyStatus)),
		SchemaAssemblyAITranscript: zObj(fl("id", zStr), fl("status", assemblyStatus), fl("text", nStr), fl("language_code", nStr),
			fl("speech_model_used", nStr), fl("words", zNullish(zArr(assemblyWord))),
			fl("utterances", zNullish(zArr(zObj(fl("start", zNum), fl("end", zNum), fl("text", zStr), fl("confidence", nNum),
				fl("speaker", nStr), fl("channel", nStr), fl("words", zNullish(zArr(assemblyWord))))))),
			fl("sentiment_analysis_results", zNullish(zArr(zObj(fl("text", zStr), fl("start", nNum), fl("end", nNum),
				fl("sentiment", zStr), fl("confidence", nNum), fl("speaker", nStr))))),
			fl("entities", zNullish(zArr(zObj(fl("entity_type", zStr), fl("text", zStr), fl("start", nNum), fl("end", nNum))))),
			fl("content_safety_labels", zNullish(zRec(zAny))), fl("iab_categories_result", zNullish(zRec(zAny))),
			fl("auto_highlights_result", zNullish(zRec(zAny))), fl("audio_duration", nNum), fl("error", nStr)),
		SchemaGladiaUpload: zObj(fl("audio_url", zStr)),
		SchemaGladiaInit:   zObj(fl("result_url", zStr)),
		SchemaGladiaResult: zObj(fl("status", zodlite.Enum("queued", "processing", "done", "error")),
			fl("result", zNullish(zObj(
				fl("metadata", zObj(fl("audio_duration", zNum))),
				fl("transcription", zObj(fl("full_transcript", zStr), fl("languages", zArr(zStr)),
					fl("utterances", zArr(zObj(fl("start", zNum), fl("end", zNum), fl("text", zStr),
						fl("speaker", zNullish(zUnionOf(zStr, zNum))), fl("confidence", nNum), fl("language", nStr),
						fl("words", zNullish(zArr(zObj(fl("word", zStr), fl("start", zNum), fl("end", zNum), fl("confidence", nNum)))))))))))))),
		SchemaCartesiaTranscription: zObj(fl("text", zStr), fl("language", nStr), fl("duration", nNum),
			fl("words", zNullish(zArr(zObj(fl("word", zStr), fl("start", zNum), fl("end", zNum)))))),
		SchemaCartesiaAccessToken: zObj(fl("token", zNonEmptyStr)),
		SchemaKlingAICreate: zObj(fl("code", zNum), fl("message", zStr), fl("request_id", nStr),
			fl("data", zNullish(zObj(fl("task_id", zStr), fl("task_status", nStr),
				fl("task_info", zNullish(zObj(fl("external_task_id", nStr)))), fl("created_at", nNum), fl("updated_at", nNum))))),
		SchemaKlingAIStatus: zObj(fl("code", zNum), fl("message", zStr), fl("request_id", nStr),
			fl("data", zNullish(zObj(fl("task_id", zStr), fl("task_status", zStr), fl("task_status_msg", nStr),
				fl("task_info", zNullish(zObj(fl("external_task_id", nStr)))),
				fl("watermark_info", zNullish(zObj(fl("enabled", zNullish(zodlite.Boolean))))),
				fl("final_unit_deduction", nStr), fl("created_at", nNum), fl("updated_at", nNum),
				fl("task_result", zNullish(zObj(fl("videos", zNullish(zArr(zObj(fl("id", nStr), fl("url", nStr),
					fl("watermark_url", nStr), fl("duration", nStr)))))))))))),
		SchemaMiniMaxCreate: zObj(fl("task_id", nStr)),
		SchemaMiniMaxStatus: zObj(fl("task", zObj(fl("id", nStr), fl("status", nStr),
			fl("content", zNullish(zObj(fl("url", nStr)))), fl("resolution", nStr), fl("duration", nNum), fl("ratio", nStr),
			fl("usage", zNullish(zObj(fl("total_seconds", nNum), fl("input_seconds", nNum), fl("output_seconds", nNum)))),
			fl("error", zNullish(zObj(fl("code", zNullish(zUnionOf(zStr, zNum))), fl("message", nStr))))))),
		SchemaByteDanceTask: zObj(fl("id", nStr)),
		SchemaByteDanceStatus: zObj(fl("id", nStr), fl("model", nStr), fl("status", zStr),
			fl("content", zNullish(zObj(fl("video_url", nStr), fl("last_frame_url", nStr)))),
			fl("usage", zNullish(zObj(fl("completion_tokens", nNum)))),
			fl("error", zNullish(zObj(fl("code", nStr), fl("message", nStr))))),
		SchemaAlibabaVideoTask: zObj(fl("output", zNullish(zObj(fl("task_status", zStr), fl("task_id", zStr)))), fl("request_id", nStr)),
		SchemaAlibabaVideoStat: zObj(
			fl("output", zNullish(zObj(fl("task_id", zStr), fl("task_status", zStr), fl("video_url", nStr), fl("submit_time", nStr),
				fl("scheduled_time", nStr), fl("end_time", nStr), fl("orig_prompt", nStr), fl("actual_prompt", nStr),
				fl("code", nStr), fl("message", nStr)))),
			fl("usage", zNullish(zObj(fl("duration", nNum), fl("output_video_duration", nNum), fl("input_video_duration", nNum),
				fl("fps", nNum), fl("SR", nNum), fl("size", nStr), fl("ratio", nStr)))),
			fl("request_id", nStr)),
		SchemaTopazVideoCreate: zObj(fl("requestId", nStr), fl("uploadUrls", zNullish(zArr(zStr))),
			fl("estimates", zNullish(zObj(fl("cost", zNullish(zArr(zNum))), fl("time", zNullish(zArr(zNum))))))),
		SchemaTopazVideoStatus: zObj(fl("status", nStr), fl("message", nStr), fl("errorCode", nStr),
			fl("outputSize", zNullish(zUnionOf(zStr, zNum))),
			fl("estimates", zNullish(zObj(fl("cost", zNullish(zArr(zNum))), fl("time", zNullish(zArr(zNum)))))),
			fl("download", zNullish(zObj(fl("url", nStr), fl("expiresAt", zNullish(zUnionOf(zStr, zNum))))))),
		SchemaXAIVideoCreate: zObj(fl("request_id", nStr)),
		SchemaXAIVideoStatus: zObj(fl("status", nStr),
			fl("video", zNullish(zObj(fl("url", nStr), fl("duration", nNum), fl("respect_moderation", zNullish(zodlite.Boolean)),
				fl("file_output", zNullish(zObj(fl("file_id", zStr), fl("filename", zStr), fl("expires_at", nNum),
					fl("public_url", nStr), fl("public_url_error", nStr), fl("public_url_expires_at", nNum)))),
				fl("storage_error", nStr)))),
			fl("model", nStr), fl("usage", zNullish(zObj(fl("cost_in_usd_ticks", nNum)))), fl("progress", nNum),
			fl("error", zNullish(zObj(fl("code", nStr), fl("message", nStr))))),
		SchemaHeyGenVideoCreate: zObj(fl("data", zObj(fl("video_id", zNonEmptyStr)))),
		SchemaHeyGenVideoStatus: zObj(fl("data", zObj(fl("status", zStr), fl("video_url", nStr), fl("duration", nNum),
			fl("width", nNum), fl("height", nNum), fl("aspect_ratio", nStr), fl("seed", nNum),
			fl("timings", zNullish(zObj(fl("inference", nNum)))), fl("failure_code", nStr), fl("failure_message", nStr)))),
		SchemaOpenAIFile: zObj(fl("id", zStr), fl("object", nStr), fl("bytes", nNum), fl("created_at", nNum), fl("filename", nStr),
			fl("purpose", nStr), fl("status", nStr), fl("expires_at", nNum)),
		SchemaXAIFile: zObj(fl("id", zStr), fl("object", nStr), fl("bytes", nNum), fl("created_at", nNum), fl("expires_at", nNum),
			fl("filename", nStr), fl("purpose", nStr), fl("status", nStr)),
		SchemaFileDelete: zObj(fl("id", zStr), fl("object", nStr), fl("deleted", zodlite.Boolean)),
		SchemaOpenAISkill: zObj(fl("id", zStr), fl("name", nStr), fl("description", nStr), fl("default_version", nStr),
			fl("latest_version", nStr), fl("created_at", zNum), fl("updated_at", nNum)),
		SchemaOpenAIBatch:     openAIBatch,
		SchemaOpenAIBatchList: zObj(fl("data", zArr(openAIBatch)), fl("has_more", zodlite.Boolean), fl("last_id", nStr)),
		SchemaXAIBatch:        xaiBatch,
		SchemaXAIBatchList:    zObj(fl("batches", zArr(xaiBatch)), fl("pagination_token", nStr)),
		SchemaXAIBatchText: zObj(fl("id", nStr), fl("created", nNum), fl("model", nStr),
			fl("choices", zNullish(zArr(zObj(
				fl("message", zObj(fl("role", zodlite.Enum("assistant", "tool")), fl("content", nStr), fl("reasoning_content", nStr),
					fl("tool_calls", zNullish(zArr(zObj(fl("id", zStr), fl("type", zodlite.Literal("function")),
						fl("function", zObj(fl("name", zStr), fl("arguments", zStr))))))))),
				fl("index", zNum), fl("finish_reason", nStr))))),
			fl("usage", zNullish(zObj(fl("prompt_tokens", zNum), fl("completion_tokens", zNum), fl("total_tokens", zNum),
				fl("cost_in_usd_ticks", nNum),
				fl("prompt_tokens_details", zNullish(zObj(fl("cached_tokens", nNum)))),
				fl("completion_tokens_details", zNullish(zObj(fl("reasoning_tokens", nNum))))))),
			fl("citations", zNullish(zArr(zURLStr))), fl("service_tier", nStr), fl("code", nStr), fl("error", nStr)),
		SchemaXAIBatchResults: zObj(fl("results", zArr(zObj(fl("batch_request_id", zStr),
			fl("batch_result", zNullish(zObj(fl("response", zNullish(zObj(fl("chat_get_completion", zAny), fl("image_generation", zAny)))),
				fl("error", zNullish(zObj(fl("code", zNullish(zUnionOf(zStr, zNum))), fl("message", nStr))))))),
			fl("error_message", nStr)))), fl("pagination_token", nStr)),
		SchemaDeepSeekFile: zObj(fl("id", zStr), fl("object", zNullish(zodlite.Literal("file"))), fl("bytes", zNullish(zIntNonneg)),
			fl("created_at", zNullish(zIntNonneg)), fl("filename", nStr), fl("purpose", zNullish(zodlite.Literal("user_data"))),
			fl("expires_at", zNullish(zIntNonneg))),
		SchemaGatewayBatchStart: zObj(append(append([]zFld{}, gatewayBatchStatus...), fl("batchId", zStr),
			fl("warnings", zNullish(zArr(zObj(fl("requestId", nStr), fl("warning", zAny))))))...),
		SchemaGatewayBatchState:  zObj(gatewayBatchStatus...),
		SchemaGatewayVideoStart:  zObj(fl("operation", zAny), fl("warnings", gatewayVideoWarnings), fl("providerMetadata", gatewayVideoMeta)),
		SchemaGatewayVideoStatus: gatewayVideoStatus,
		SchemaGatewayDecision: zObj(
			fl("answers", zRec(zDiscType(map[string]zNode{
				"choice":  zodlite.Typed("choice", fl("choice", zStr), fl("probabilities", zOptional(zRec(zNum)))),
				"score":   zodlite.Typed("score", fl("score", zNum), fl("probabilities", zOptional(zRec(zNum)))),
				"boolean": zodlite.Typed("boolean", fl("probability", zNum)),
				"refusal": zodlite.Typed("refusal"),
			}))),
			fl("model", zOptional(zStr)),
			fl("rounding", zOptional(zObj(fl("probabilityDecimals", zOptional(zNum)), fl("scoreDecimals", zOptional(zNum))))),
			fl("usage", zOptional(zObj(fl("inputTokens", zOptional(zNum)), fl("outputTokens", zOptional(zNum))))),
			fl("warnings", zOptional(zArr(gatewayWarning))),
			fl("providerMetadata", zOptional(zRec(zRec(zAny))))),
		SchemaGatewayBatchItem: zObj(fl("type", zodlite.Literal("text")), fl("id", zStr),
			fl("status", zodlite.Enum("cancelled", "expired", "failed", "succeeded"))),
		SchemaMistralSpeech: zObj(fl("audio_data", zStr)),
		SchemaXAISpeechTimestamps: zObj(fl("audio", nStr), fl("content_type", nStr), fl("duration", nNum),
			fl("audio_timestamps", zNullish(zObj(fl("graph_chars", zArr(zStr)),
				fl("graph_times", zArr(zTuple2Num)))))),
		SchemaGatewayCredits: zObj(fl("balance", zStr), fl("total_used", zStr)),
		SchemaGatewayGenerationInfo: zObj(fl("data", zObj(fl("id", zStr), fl("total_cost", zNum), fl("upstream_inference_cost", zNum),
			fl("usage", zNum), fl("created_at", zStr), fl("model", zStr), fl("is_byok", zodlite.Boolean), fl("provider_name", zStr),
			fl("streamed", zodlite.Boolean), fl("finish_reason", zStr), fl("latency", zNum), fl("generation_time", zNum),
			fl("native_tokens_prompt", zNum), fl("native_tokens_completion", zNum), fl("native_tokens_reasoning", zNum),
			fl("native_tokens_cached", zNum), fl("native_tokens_cache_creation", zNum), fl("billable_web_search_calls", zNum)))),
		SchemaFalSpeech: zObj(fl("audio", zObj(fl("url", zStr))), fl("duration_ms", zOptional(zNum)), fl("request_id", zOptional(zStr))),
		SchemaFalJob:    zObj(fl("request_id", nStr)),
		SchemaFalTranscr: zObj(fl("text", zStr),
			fl("chunks", zNullish(zArr(zObj(fl("text", zStr), fl("timestamp", zNullish(zArr(zNum))))))),
			fl("inferred_languages", zNullish(zArr(zStr)))),
	}
}()

// CohereEmbeddingNode is cohereTextEmbeddingResponseSchema(embeddingType).
func cohereEmbeddingNode(embeddingType string) zNode {
	return zObj(fl("embeddings", zObj(fl(embeddingType, zArr(zNumVec())))),
		fl("meta", zObj(fl("billed_units", zObj(fl("input_tokens", zNum))))))
}

// NodeResponseValidator is the http.Request.ValidateResponse check for an
// already-built schema node (TS createJsonResponseHandler(schema)).
func NodeResponseValidator(providerName string, node zodlite.Node) func(statusCode int, headers http.Header, body []byte) error {
	return responseValidator(providerName, func(body []byte) error {
		var value interface{}
		if err := json.Unmarshal(body, &value); err != nil {
			return err
		}
		if bad := node(value, true, nil); bad != nil {
			return bad
		}
		return nil
	})
}

// MediaResponseValidator is the ValidateResponse check for a named TS schema.
func MediaResponseValidator(providerName string, schema MediaResponseSchema) func(statusCode int, headers http.Header, body []byte) error {
	node, ok := mediaSchemas[schema]
	if !ok {
		panic(fmt.Sprintf("providerutils: unknown media response schema %q", schema))
	}
	return NodeResponseValidator(providerName, node)
}

// MediaSchemaParser is the check for a body parsed with a bare schema.parse
// (TS throws the TypeValidationError / ZodError itself, not an APICallError
// "Invalid JSON response"): the failure is the typed validation (or JSON
// parse) error.
func MediaSchemaParser(schema MediaResponseSchema) func(statusCode int, headers http.Header, body []byte) error {
	node, ok := mediaSchemas[schema]
	if !ok {
		panic(fmt.Sprintf("providerutils: unknown media response schema %q", schema))
	}
	return func(_ int, _ http.Header, body []byte) error {
		var value interface{}
		cause := json.Unmarshal(body, &value)
		if cause == nil {
			if bad := node(value, true, nil); bad != nil {
				cause = bad
			}
		}
		if cause == nil {
			return nil
		}
		_, typed := ChunkParseFailure(string(body), cause)
		return typed
	}
}

// CohereEmbeddingResponseValidator is cohereTextEmbeddingResponseSchema for
// the requested embedding type ("float", "int8", ...).
func CohereEmbeddingResponseValidator(providerName, embeddingType string) func(statusCode int, headers http.Header, body []byte) error {
	return NodeResponseValidator(providerName, cohereEmbeddingNode(embeddingType))
}
