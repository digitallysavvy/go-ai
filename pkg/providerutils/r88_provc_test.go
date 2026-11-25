package providerutils

import (
	"net/http"
	"testing"
)

// cr-r88-provg-to-provc: Gateway schema additions (TS 2e44144812,
// 41465fd1b6, 32acf432db).
func TestR88provcGatewaySchemaAdditions(t *testing.T) {
	for _, c := range []struct {
		schema  MediaResponseSchema
		ok, bad []string
	}{
		{SchemaGatewayVideoStatus,
			[]string{`{"status":"completed","videos":[{"type":"uploaded","mediaType":"video/mp4"}],"warnings":[]}`},
			[]string{`{"status":"completed","videos":[{"type":"uploaded"}],"warnings":[]}`}},
		{SchemaGatewayVideoEvent,
			[]string{`{"type":"result","videos":[{"type":"uploaded","mediaType":"video/mp4"}]}`},
			[]string{`{"type":"result","videos":[{"type":"uploaded","mediaType":1}]}`}},
		{SchemaGatewayTranscription,
			[]string{`{"text":"a","words":[{"text":"a","startSecond":0,"endSecond":1}]}`, `{"text":"a","words":null}`},
			[]string{`{"text":"a","words":[{"text":"a","start":0,"end":1}]}`, `{"text":"a","words":{}}`}},
		{SchemaGatewayGenerationInfo,
			[]string{`{"data":{"id":"g","total_cost":1,"market_cost":0.5,"surcharge_cost":null,"upstream_inference_cost":0,"usage":1,"created_at":"t","model":"m","is_byok":false,"provider_name":"p","streamed":true,"finish_reason":"stop","latency":1,"generation_time":1,"native_tokens_prompt":1,"native_tokens_completion":1,"native_tokens_reasoning":0,"native_tokens_cached":0,"native_tokens_cache_creation":0,"billable_web_search_calls":0}}`},
			[]string{`{"data":{"id":"g","total_cost":1,"gateway_cost":"1","upstream_inference_cost":0,"usage":1,"created_at":"t","model":"m","is_byok":false,"provider_name":"p","streamed":true,"finish_reason":"stop","latency":1,"generation_time":1,"native_tokens_prompt":1,"native_tokens_completion":1,"native_tokens_reasoning":0,"native_tokens_cached":0,"native_tokens_cache_creation":0,"billable_web_search_calls":0}}`}},
	} {
		validate := MediaResponseValidator("p", c.schema)
		for _, body := range c.ok {
			if err := validate(200, http.Header{}, []byte(body)); err != nil {
				t.Errorf("%s: %s rejected: %v", c.schema, body, err)
			}
		}
		for _, body := range c.bad {
			if err := validate(200, http.Header{}, []byte(body)); err == nil {
				t.Errorf("%s: %s accepted", c.schema, body)
			}
		}
	}
}
