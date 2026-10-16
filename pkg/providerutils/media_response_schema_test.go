package providerutils

import (
	"errors"
	"net/http"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// Each case is a body the TS zod schema accepts (ok) or rejects (bad).
func TestMediaResponseSchemas(t *testing.T) {
	cases := []struct {
		schema MediaResponseSchema
		ok     []string
		bad    []string
	}{
		{SchemaOpenAIEmbedding,
			[]string{`{"data":[{"embedding":[0.1]}]}`, `{"data":[],"usage":null}`, `{"data":[{"embedding":[1]}],"usage":{"prompt_tokens":3},"extra":1}`},
			[]string{`[]`, `{}`, `{"data":[{"embedding":"x"}]}`, `{"data":[{"embedding":[1]}],"usage":{"prompt_tokens":"3"}}`}},
		{SchemaCompatEmbedding,
			[]string{`{"data":[{"embedding":[1]}],"providerMetadata":{"a":{"b":1}}}`},
			[]string{`{"data":[{"embedding":[1]}],"providerMetadata":{"a":1}}`, `{"data":[{"embedding":[1]}],"providerMetadata":null}`}},
		{SchemaVoyageEmbedding,
			[]string{`{"data":[{"embedding":[1],"index":0}],"model":"m"}`},
			[]string{`{"data":[{"embedding":[1],"index":0}]}`, `{"data":[{"embedding":[1]}],"model":"m"}`}},
		{SchemaPerplexityEmbedding,
			[]string{`{"data":[{"embedding":"AAAA"}],"usage":{"prompt_tokens":1,"cost":{"total_cost":0.1}}}`},
			[]string{`{"data":[{"embedding":[1]}]}`, `{"data":[{"embedding":"x"}],"usage":{"cost":{}}}`}},
		{SchemaAlibabaEmbedding,
			[]string{`{"output":{"embeddings":[{"embedding":[1],"text_index":0}]},"usage":{"total_tokens":2}}`},
			[]string{`{"output":{"embeddings":[{"embedding":[1]}]}}`, `{"data":[]}`}},
		{SchemaCohereRerank,
			[]string{`{"results":[{"index":0,"relevance_score":0.5}]}`, `{"id":null,"results":[],"meta":{}}`},
			[]string{`{"results":[{"index":0}]}`, `{"results":"x"}`}},
		{SchemaVoyageRerank,
			[]string{`{"data":[{"index":0,"relevance_score":1}]}`},
			[]string{`{"results":[]}`}},
		{SchemaTogetherRerank,
			[]string{`{"results":[],"usage":{"prompt_tokens":1,"completion_tokens":0,"total_tokens":1}}`},
			[]string{`{"results":[]}`}},
		{SchemaOpenAIImage,
			[]string{`{"data":[{"b64_json":"aa"}]}`, `{"created":1,"data":[{"b64_json":"aa","revised_prompt":null}],"usage":{"input_tokens_details":{"text_tokens":1}}}`},
			[]string{`{"data":[{"url":"https://x"}]}`, `{}`}},
		{SchemaCompatImage, []string{`{"data":[{"b64_json":"aa"}],"usage":null}`}, []string{`{"data":[{}]}`}},
		{SchemaTogetherImage, []string{`{"data":[{"b64_json":"aa"}]}`}, []string{`{"data":[{"url":"u"}]}`}},
		{SchemaDeepInfraImage, []string{`{"images":["a"]}`}, []string{`{"images":[1]}`, `{"data":[]}`}},
		{SchemaXAIImage,
			[]string{`{"data":[{"url":"u"},{"b64_json":"b","respect_moderation":false}],"usage":{"cost_in_usd_ticks":5}}`},
			[]string{`{"data":[{"respect_moderation":"no"}]}`, `{"data":"x"}`}},
		{SchemaByteDanceImage, []string{`{"data":[{"b64_json":"x"}]}`}, []string{`{"data":[{"url":"x"}]}`}},
		{SchemaQuiverSVG,
			[]string{`{"id":"a","created":1,"data":[{"svg":"<svg/>","mime_type":"image/svg+xml"}]}`},
			[]string{`{"id":"a","created":1,"data":[]}`, `{"id":"","created":1,"data":[{"svg":"s","mime_type":"image/svg+xml"}]}`,
				`{"id":"a","created":1.5,"data":[{"svg":"s","mime_type":"image/svg+xml"}]}`,
				`{"id":"a","created":1,"data":[{"svg":"s","mime_type":"image/png"}]}`,
				`{"id":"a","created":1,"data":[{"svg":"s","mime_type":"image/svg+xml"}],"credits":-1}`}},
		{SchemaTopazSubmit, []string{`{}`, `{"process_id":null}`}, []string{`{"process_id":1}`, `[]`}},
		{SchemaTopazStatus, []string{`{"status":"Pending"}`}, []string{`{"credits":"1"}`}},
		{SchemaTopazDownload, []string{`{"download_url":"u"}`}, []string{`{"download_url":1}`}},
		{SchemaAzureMAIImage, []string{`{"data":[{"b64_json":"x"}]}`}, []string{`{"data":[{}]}`}},
		{SchemaFalImage,
			[]string{`{"images":[{"url":"u"}]}`, `{"image":{"url":"u","width":1}}`, `{"images":[],"seed":1,"timings":{"inference":0.1}}`},
			[]string{`{"images":[{"width":1}]}`, `{}`, `{"images":[{"url":"u"}],"seed":null}`}},
		{SchemaOpenAITranscr,
			[]string{`{"text":"a"}`, `{"text":"a","segments":[{"type":"transcript.text.segment","id":"s","start":0,"end":1,"text":"a","speaker":"A"}]}`},
			[]string{`{"text":"a","segments":[{"text":"a","start":0,"end":1}]}`, `{"language":"en"}`}},
		{SchemaGroqTranscr, []string{`{"text":"a","x_groq":{"id":"r"}}`}, []string{`{"text":"a"}`}},
		{SchemaMistralTranscr, []string{`{"model":"m","text":"a"}`}, []string{`{"text":"a"}`}},
		{SchemaElevenLabsTr,
			[]string{`{"language_code":"en","language_probability":0.9,"text":"a","words":[{"text":"a","type":"word"}]}`},
			[]string{`{"language_code":"en","text":"a"}`, `{"language_code":"en","language_probability":1,"text":"a","words":[{"text":"a","type":"other"}]}`}},
		{SchemaFishAudioTr, []string{`{"text":"a","segments":[{"text":"a","start":0,"end":1}]}`}, []string{`{"text":1}`}},
		{SchemaRevAIJob, []string{`{}`, `{"id":"a","status":"in_progress"}`}, []string{`{"id":1}`}},
		{SchemaRevAITranscript, []string{`{}`, `{"monologues":[{"elements":[{"value":"a"}]}]}`}, []string{`{"monologues":"x"}`}},
		{SchemaXAITranscr, []string{`{"text":"a","words":[{"text":"a","start":0,"end":1,"speaker":0}]}`}, []string{`{"text":"a","words":[{"text":"a"}]}`}},
		{SchemaAzureSpeechTr, []string{`{"combinedPhrases":[{"text":"a"}]}`}, []string{`{"phrases":[]}`}},
		{SchemaFalJob, []string{`{}`, `{"request_id":"r"}`}, []string{`{"request_id":1}`}},
		{SchemaFalTranscr, []string{`{"text":"a","chunks":[{"text":"a","timestamp":[0,1]}]}`}, []string{`{"chunks":[]}`}},
		{SchemaMistralSpeech, []string{`{"audio_data":"a"}`}, []string{`{}`, `{"audio_data":1}`}},
		{SchemaXAISpeechTimestamps,
			[]string{`{}`, `{"audio":"a","audio_timestamps":{"graph_chars":["a"],"graph_times":[[0,1]]}}`},
			[]string{`{"audio_timestamps":{"graph_chars":["a"],"graph_times":[[0]]}}`, `{"audio_timestamps":{"graph_chars":[1],"graph_times":[]}}`}},
		{SchemaGatewayCredits, []string{`{"balance":"1","total_used":"2"}`}, []string{`{"balance":1,"total_used":"2"}`}},
		{SchemaGatewayGenerationInfo,
			[]string{`{"data":{"id":"g","total_cost":1,"upstream_inference_cost":0,"usage":1,"created_at":"t","model":"m","is_byok":false,"provider_name":"p","streamed":true,"finish_reason":"stop","latency":1,"generation_time":1,"native_tokens_prompt":1,"native_tokens_completion":1,"native_tokens_reasoning":0,"native_tokens_cached":0,"native_tokens_cache_creation":0,"billable_web_search_calls":0}}`},
			[]string{`{"data":{"id":"g"}}`, `{}`}},
		{SchemaFalSpeech, []string{`{"audio":{"url":"u"}}`}, []string{`{"audio":{}}`, `{"audio":{"url":"u"},"duration_ms":"1"}`}},
		{SchemaGatewayEmbedding,
			[]string{`{"embeddings":[[1]],"usage":{"tokens":1},"warnings":[{"type":"other","message":"m"}],"providerMetadata":{"a":{"b":1}}}`},
			[]string{`{"embeddings":[1]}`, `{"embeddings":[],"warnings":[{"type":"mystery"}]}`}},
		{SchemaGatewayImage, []string{`{"images":["a"],"usage":{"inputTokens":1}}`}, []string{`{"images":"a"}`, `{"images":[1]}`}},
		{SchemaGatewayReranking, []string{`{"ranking":[{"index":0,"relevanceScore":1}]}`}, []string{`{"ranking":[{"index":0,"relevance_score":1}]}`}},
		{SchemaGatewaySpeech, []string{`{"audio":"a","usage":{"k":[1]}}`}, []string{`{"audio":1}`}},
		{SchemaGatewayTranscription,
			[]string{`{"text":"a","segments":[{"text":"a","startSecond":0,"endSecond":1}],"language":null}`},
			[]string{`{"text":"a","segments":[{"text":"a","start":0,"end":1}]}`}},
		{SchemaAssemblyAIUpload, []string{`{"upload_url":"u"}`}, []string{`{}`}},
		{SchemaAssemblyAISubmit, []string{`{"id":"a","status":"queued"}`}, []string{`{"id":"a","status":"waiting"}`, `{"status":"queued"}`}},
		{SchemaAssemblyAITranscript,
			[]string{`{"id":"a","status":"completed","words":[{"start":0,"end":1,"text":"x"}],"content_safety_labels":{"a":1}}`},
			[]string{`{"id":"a","status":"completed","words":[{"text":"x"}]}`, `{"id":"a","status":"completed","entities":[{"text":"x"}]}`}},
		{SchemaGladiaUpload, []string{`{"audio_url":"u"}`}, []string{`{}`}},
		{SchemaGladiaInit, []string{`{"result_url":"u"}`}, []string{`{"result_url":null}`}},
		{SchemaGladiaResult,
			[]string{`{"status":"queued"}`, `{"status":"done","result":null}`,
				`{"status":"done","result":{"metadata":{"audio_duration":1},"transcription":{"full_transcript":"a","languages":["en"],"utterances":[{"start":0,"end":1,"text":"a","speaker":1}]}}}`},
			[]string{`{"status":"finished"}`, `{"status":"done","result":{"metadata":{},"transcription":{}}}`}},
		{SchemaCartesiaTranscription, []string{`{"text":"a","words":null}`}, []string{`{"words":[]}`, `{"text":"a","words":[{"word":"a"}]}`}},
		{SchemaCartesiaAccessToken, []string{`{"token":"t"}`}, []string{`{"token":""}`, `{}`}},
		{SchemaKlingAICreate, []string{`{"code":0,"message":"ok","data":{"task_id":"t"}}`, `{"code":0,"message":"ok"}`},
			[]string{`{"message":"ok"}`, `{"code":0,"message":"ok","data":{}}`}},
		{SchemaKlingAIStatus,
			[]string{`{"code":0,"message":"ok","data":{"task_id":"t","task_status":"succeed","task_result":{"videos":[{"url":"u"}]}}}`},
			[]string{`{"code":0,"message":"ok","data":{"task_id":"t"}}`, `{"code":"0","message":"ok"}`}},
		{SchemaMiniMaxCreate, []string{`{}`, `{"task_id":"t"}`}, []string{`{"task_id":1}`}},
		{SchemaMiniMaxStatus, []string{`{"task":{"status":"succeeded","error":{"code":5}}}`}, []string{`{}`, `{"task":{"duration":"1"}}`}},
		{SchemaByteDanceTask, []string{`{}`, `{"id":"i"}`}, []string{`{"id":2}`}},
		{SchemaByteDanceStatus, []string{`{"status":"succeeded","content":{"video_url":"u"}}`}, []string{`{}`, `{"status":"x","error":{"code":1}}`}},
		{SchemaAlibabaVideoTask, []string{`{}`, `{"output":{"task_status":"PENDING","task_id":"t"}}`}, []string{`{"output":{"task_id":"t"}}`}},
		{SchemaAlibabaVideoStat, []string{`{"output":{"task_id":"t","task_status":"SUCCEEDED","video_url":"u"},"usage":{"SR":1}}`},
			[]string{`{"output":{"task_status":"x"}}`, `{"usage":{"fps":"1"}}`}},
		{SchemaTopazVideoCreate, []string{`{}`, `{"requestId":"r","uploadUrls":["u"],"estimates":{"cost":[1]}}`}, []string{`{"uploadUrls":"u"}`}},
		{SchemaTopazVideoStatus, []string{`{"status":"complete","outputSize":5,"download":{"url":"u","expiresAt":1}}`}, []string{`{"download":{"url":1}}`, `{"outputSize":true}`}},
		{SchemaOpenAIFile, []string{`{"id":"f"}`, `{"id":"f","bytes":null,"expires_at":5}`}, []string{`{}`, `{"id":"f","bytes":"1"}`}},
		{SchemaXAIFile, []string{`{"id":"f","status":"processed"}`}, []string{`{"id":1}`}},
		{SchemaFileDelete, []string{`{"id":"f","deleted":true}`}, []string{`{"id":"f"}`, `{"id":"f","deleted":"yes"}`}},
		{SchemaOpenAISkill, []string{`{"id":"s","created_at":1}`}, []string{`{"id":"s"}`}},
		{SchemaOpenAIBatch,
			[]string{`{"id":"b","status":"validating"}`, `{"id":"b","status":"x","request_counts":{"total":1},"errors":{"data":[{"code":"c"}]}}`},
			[]string{`{"id":"b"}`, `{"id":"b","status":"x","request_counts":{"total":"1"}}`}},
		{SchemaOpenAIBatchList, []string{`{"data":[],"has_more":false}`}, []string{`{"data":[]}`, `{"data":[{"id":"b"}],"has_more":true}`}},
		{SchemaXAIBatch, []string{`{"batch_id":"b","state":{"num_requests":1}}`}, []string{`{"id":"b"}`}},
		{SchemaXAIBatchList, []string{`{"batches":[{"batch_id":"b"}]}`}, []string{`{"batches":[{}]}`}},
		{SchemaXAIBatchResults,
			[]string{`{"results":[{"batch_request_id":"r","batch_result":{"error":{"code":5,"message":"m"}}}],"pagination_token":null}`},
			[]string{`{"results":[{}]}`, `{}`}},
		{SchemaDeepSeekFile,
			[]string{`{"id":"f","object":"file","bytes":1,"purpose":"user_data"}`},
			[]string{`{"id":"f","object":"batch"}`, `{"id":"f","bytes":-1}`, `{"id":"f","bytes":1.5}`, `{"id":"f","purpose":"assistants"}`}},
		{SchemaGatewayBatchStart, []string{`{"status":"pending","batchId":"b","warnings":[{"warning":1}]}`}, []string{`{"status":"pending"}`, `{"status":"done","batchId":"b"}`}},
		{SchemaGatewayBatchState, []string{`{"status":"completed","requestCounts":{"total":1},"providerMetadata":{"a":{"b":1}}}`}, []string{`{"status":"running"}`, `{"status":"failed","error":{}}`}},
		{SchemaXAIVideoCreate, []string{`{}`, `{"request_id":"r"}`}, []string{`{"request_id":1}`}},
		{SchemaXAIVideoStatus, []string{`{"status":"done","video":{"url":"u","file_output":{"file_id":"f","filename":"n"}}}`}, []string{`{"video":{"file_output":{}}}`, `{"progress":"1"}`}},
		{SchemaHeyGenVideoCreate, []string{`{"data":{"video_id":"v"}}`}, []string{`{"data":{"video_id":""}}`, `{}`}},
		{SchemaHeyGenVideoStatus, []string{`{"data":{"status":"completed","timings":{"inference":1}}}`}, []string{`{"data":{}}`, `{"data":{"status":"x","width":"1"}}`}},
	}
	for _, c := range cases {
		validate := MediaResponseValidator("p", c.schema)
		for _, body := range c.ok {
			if err := validate(200, http.Header{}, []byte(body)); err != nil {
				t.Errorf("%s: %s rejected: %v", c.schema, body, err)
			}
		}
		for _, body := range c.bad {
			err := validate(200, http.Header{"X-Req": {"1"}}, []byte(body))
			var pe *providererrors.ProviderError
			if !errors.As(err, &pe) || pe.Message != "Invalid JSON response" || pe.StatusCode != 200 || pe.ResponseBody != body {
				t.Errorf("%s: %s accepted or wrong error: %v", c.schema, body, err)
			}
		}
		if err := validate(200, http.Header{}, []byte(`not json`)); !IsInvalidJSONResponse(err) {
			t.Errorf("%s: non-JSON body error = %v", c.schema, err)
		}
	}
}

func TestCohereEmbeddingResponseValidator(t *testing.T) {
	validate := CohereEmbeddingResponseValidator("cohere", "int8")
	if err := validate(200, http.Header{}, []byte(`{"embeddings":{"int8":[[1]]},"meta":{"billed_units":{"input_tokens":2}}}`)); err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{
		`{"embeddings":{"float":[[1]]},"meta":{"billed_units":{"input_tokens":2}}}`,
		`{"embeddings":[[1]],"meta":{"billed_units":{"input_tokens":2}}}`,
		`{"embeddings":{"int8":[[1]]}}`,
	} {
		if err := validate(200, http.Header{}, []byte(body)); !IsInvalidJSONResponse(err) {
			t.Errorf("%s accepted: %v", body, err)
		}
	}
}
