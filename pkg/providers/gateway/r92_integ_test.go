package gateway

import (
	"encoding/json"
	"testing"
	"time"
)

// cr-r92-provg-to-integrator CR 2: the gateway's TS `new Date(str)` sites
// (gateway-language-model.ts 171, gateway-batch.ts 482, provider-utils
// transcription-stream-envelope.ts 296) parse like Date.parse and keep
// millisecond precision only.
var r92integGatewayDates = []struct {
	in   string
	want time.Time
}{
	{"2026-01-02", time.Date(2026, 1, 2, 0, 0, 0, 0, time.UTC)},
	{"2026-01-02T03:04:05.123456Z", time.Date(2026, 1, 2, 3, 4, 5, 123000000, time.UTC)},
	{"Fri, 02 Jan 2026 03:04:05 GMT", time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)},
}

func TestR92integGatewayTimestampsAreJSDates(t *testing.T) {
	for _, tc := range r92integGatewayDates {
		s := &gatewayTextStream{}
		chunk, err, _ := s.convertChunk(&gatewayStreamChunk{Type: "response-metadata", Timestamp: tc.in})
		if err != nil || chunk == nil || chunk.ResponseMetadata == nil || !chunk.ResponseMetadata.Timestamp.Equal(tc.want) {
			t.Errorf("language stream %q: %+v %v, want %v", tc.in, chunk, err, tc.want)
		}

		raw, _ := json.Marshal(map[string]interface{}{"content": []interface{}{}, "finishReason": map[string]interface{}{"unified": "stop", "raw": "stop"},
			"usage": map[string]interface{}{}, "warnings": []interface{}{}, "response": map[string]interface{}{"timestamp": tc.in}})
		res := decodeGatewayBatchItemResult(raw)
		if res.ResponseMetadata == nil || !res.ResponseMetadata.Timestamp.Equal(tc.want) {
			t.Errorf("batch item %q: %+v, want %v", tc.in, res.ResponseMetadata, tc.want)
		}

		part, ok := parseGatewayTranscriptionStreamPart(`{"type":"response-metadata","modelId":"m","timestamp":"` + tc.in + `"}`)
		if !ok || !part.Timestamp.Equal(tc.want) {
			t.Errorf("transcription part %q: %+v %v, want %v", tc.in, part, ok, tc.want)
		}
	}
}
