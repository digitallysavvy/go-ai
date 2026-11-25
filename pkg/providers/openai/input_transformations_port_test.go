package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Ports of TS openai-responses-input-transformations.test.ts and the batch
// result case in openai-batch.test.ts (1a7440bcde): input_transformations is
// returned as providerMetadata.openai.inputTransformations.

func portprovbTransformationCases() []struct {
	name            string
	transformations interface{} // nil = JSON null; portprovbMissing = key absent
	want            interface{} // nil = no inputTransformations key
} {
	removed := func(id, reason string) map[string]interface{} {
		return map[string]interface{}{"id": id, "type": "reasoning", "action": "removed", "reason": reason}
	}
	return []struct {
		name            string
		transformations interface{}
		want            interface{}
	}{
		{"a removed reasoning item", []interface{}{removed("rs_1", "prefix_mismatch")}, []interface{}{removed("rs_1", "prefix_mismatch")}},
		{"several removed reasoning items", []interface{}{removed("rs_1", "end_user_mismatch"), removed("rs_2", "model_mismatch")},
			[]interface{}{removed("rs_1", "end_user_mismatch"), removed("rs_2", "model_mismatch")}},
		{"no transformations", []interface{}{}, []interface{}{}},
		{"a future action and reason without an id", []interface{}{map[string]interface{}{"type": "reasoning", "action": "future_action", "reason": "future_reason"}},
			[]interface{}{map[string]interface{}{"type": "reasoning", "action": "future_action", "reason": "future_reason"}}},
		{"null", nil, nil},
		{"a missing field", portprovbMissing{}, nil},
	}
}

type portprovbMissing struct{}

func portprovbResponseBody(transformations interface{}) map[string]interface{} {
	response := map[string]interface{}{"id": "resp_test", "created_at": 0, "model": "gpt-6.2-astra", "output": []interface{}{}}
	if _, missing := transformations.(portprovbMissing); !missing {
		response["input_transformations"] = transformations
	}
	return response
}

func portprovbModel(t *testing.T, handler http.HandlerFunc) *ResponsesLanguageModel {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	return NewResponsesLanguageModel(New(Config{APIKey: "test-key", BaseURL: server.URL}), "gpt-6.2-astra")
}

func portprovbHello() *provider.GenerateOptions {
	return &provider.GenerateOptions{Prompt: types.Prompt{Messages: []types.Message{userMsg("Hello")}}}
}

func portprovbCheckTransformations(t *testing.T, metadata map[string]interface{}, want interface{}) {
	t.Helper()
	openai, _ := metadata["openai"].(map[string]interface{})
	got, has := openai["inputTransformations"]
	if want == nil {
		if has {
			t.Fatalf("inputTransformations present: %#v", got)
		}
		return
	}
	// Compare through JSON so []map and []interface{} shapes match.
	gotJSON, _ := json.Marshal(got)
	wantJSON, _ := json.Marshal(want)
	if !has || string(gotJSON) != string(wantJSON) {
		t.Fatalf("inputTransformations = %s, want %s", gotJSON, wantJSON)
	}
}

func TestPortprovbInputTransformationsGenerate(t *testing.T) {
	for _, tc := range portprovbTransformationCases() {
		t.Run(tc.name, func(t *testing.T) {
			body, _ := json.Marshal(portprovbResponseBody(tc.transformations))
			model := portprovbModel(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write(body)
			})
			result, err := model.DoGenerate(context.Background(), portprovbHello())
			if err != nil {
				t.Fatal(err)
			}
			portprovbCheckTransformations(t, result.ProviderMetadata, tc.want)
		})
	}
}

func portprovbStreamFinishMetadata(t *testing.T, model *ResponsesLanguageModel) map[string]interface{} {
	t.Helper()
	stream, err := model.DoStream(context.Background(), portprovbHello())
	if err != nil {
		t.Fatal(err)
	}
	defer stream.Close() //nolint:errcheck
	for {
		chunk, err := stream.Next()
		if err != nil {
			t.Fatal("no finish part")
		}
		if chunk.Type == provider.ChunkTypeFinish {
			var metadata map[string]interface{}
			if err := json.Unmarshal(chunk.ProviderMetadata, &metadata); err != nil {
				t.Fatal(err)
			}
			return metadata
		}
	}
}

func TestPortprovbInputTransformationsStream(t *testing.T) {
	for _, tc := range portprovbTransformationCases() {
		t.Run(tc.name, func(t *testing.T) {
			event, _ := json.Marshal(map[string]interface{}{"type": "response.completed", "response": portprovbResponseBody(tc.transformations)})
			model := portprovbModel(t, func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "text/event-stream")
				_, _ = fmt.Fprintf(w, "data: %s\n\n", event)
			})
			portprovbCheckTransformations(t, portprovbStreamFinishMetadata(t, model), tc.want)
		})
	}
}

// TS 'returns the transformations of a response that fails after output starts'.
func TestPortprovbInputTransformationsFailedStream(t *testing.T) {
	transformations := []interface{}{map[string]interface{}{"id": "rs_1", "type": "reasoning", "action": "removed", "reason": "prefix_mismatch"}}
	failed, _ := json.Marshal(map[string]interface{}{"type": "response.failed", "sequence_number": 2, "response": map[string]interface{}{
		"error": map[string]interface{}{"code": "server_error", "message": "failed"}, "input_transformations": transformations}})
	model := portprovbModel(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/event-stream")
		for _, e := range []string{
			`{"type":"response.created","response":{"id":"resp_test","created_at":0,"model":"gpt-6.2-astra"}}`,
			`{"type":"response.output_item.added","output_index":0,"item":{"id":"msg_1","type":"message"}}`,
			string(failed),
		} {
			_, _ = fmt.Fprintf(w, "data: %s\n\n", e)
		}
	})
	portprovbCheckTransformations(t, portprovbStreamFinishMetadata(t, model), transformations)
}

// TS openai-batch.test.ts: the batch result carries inputTransformations.
func TestPortprovbBatchResultInputTransformations(t *testing.T) {
	body := `{"id":"resp_1","created_at":1,"model":"gpt-5.6","output":[],"usage":{"input_tokens":1,"output_tokens":1},` +
		`"input_transformations":[{"id":"rs_1","type":"reasoning","action":"removed","reason":"prefix_mismatch"}]}`
	res, berr := convertOpenAIBatchResponseBodyBytes(json.RawMessage(body))
	if berr != nil {
		t.Fatalf("convert: %+v", berr)
	}
	meta, _ := res.ProviderMetadata["openai"].(map[string]interface{})
	want := []interface{}{map[string]interface{}{"id": "rs_1", "type": "reasoning", "action": "removed", "reason": "prefix_mismatch"}}
	if !reflect.DeepEqual(meta["inputTransformations"], want) {
		t.Fatalf("metadata = %#v", meta)
	}
	if strings.Contains(fmt.Sprint(meta), "promptCacheDiagnostics") {
		t.Fatalf("metadata = %#v", meta)
	}
}
