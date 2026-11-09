package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TestR90provgHeadquartersOption ports TS 44db2252f0
// (gateway-provider-options.ts `headquarters?: string[]`): the typed option
// reaches providerOptions.gateway.headquarters on the wire.
func TestR90provgHeadquartersOption(t *testing.T) {
	var body map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&body)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"text":"ok","finishReason":"stop","usage":{}}`))
	}))
	defer server.Close()

	p, err := New(Config{APIKey: "test-key", BaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	model, err := p.LanguageModel("openai/gpt-4o")
	if err != nil {
		t.Fatal(err)
	}
	for _, hq := range [][]string{{"US", "DE"}, {"!US"}} {
		_, err = model.DoGenerate(context.Background(), &provider.GenerateOptions{
			Prompt:          types.Prompt{Text: "hello"},
			ProviderOptions: GatewayProviderOptions{Headquarters: hq}.ToProviderOptions(),
		})
		if err != nil {
			t.Fatal(err)
		}
		gw, _ := body["providerOptions"].(map[string]interface{})["gateway"].(map[string]interface{})
		got, _ := gw["headquarters"].([]interface{})
		want := make([]interface{}, len(hq))
		for i, c := range hq {
			want[i] = c
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("headquarters = %#v, want %#v", gw["headquarters"], want)
		}
	}
	if _, ok := (GatewayProviderOptions{}).toMap()["headquarters"]; ok {
		t.Fatal("unset Headquarters must not be sent")
	}
	b, _ := json.Marshal(GatewayProviderOptions{Headquarters: []string{"US"}})
	if string(b) != `{"headquarters":["US"]}` {
		t.Fatalf("json = %s", b)
	}
}

// TestR90provgModelIDs: TS 84208104a4 (Claude Fable 5.2) and 6d65092d9e
// (Meta Muse Ember) gateway model IDs.
func TestR90provgModelIDs(t *testing.T) {
	want := map[GatewayLanguageModelID]string{
		GatewayLanguageModelAnthropicClaudeFable52:      "anthropic/claude-fable-5.2",
		GatewayLanguageModelAnthropicClaudeFable52Batch: "anthropic/claude-fable-5.2-batch",
		GatewayLanguageModelMetaMuseEmber:               "meta/muse-ember",
	}
	listed := map[GatewayLanguageModelID]bool{}
	for _, id := range GatewayLanguageModelIDs {
		listed[id] = true
	}
	for id, s := range want {
		if string(id) != s {
			t.Errorf("%s != %s", id, s)
		}
		if !listed[id] {
			t.Errorf("%s missing from GatewayLanguageModelIDs", id)
		}
	}
}

// TestR90provgRerankDocumentsUnion: TS core rerank sends {type: 'text'} when
// the first document is a string, else {type: 'object'}, for any document
// values (rerank.ts documentsToSend); a []json.RawMessage or struct slice is
// still wrapped.
func TestR90provgRerankDocumentsUnion(t *testing.T) {
	type doc struct {
		A int `json:"a"`
	}
	cases := []struct {
		docs interface{}
		want string
	}{
		{[]string{"x"}, `{"type":"text","values":["x"]}`},
		{[]map[string]interface{}{{"a": 1}}, `{"type":"object","values":[{"a":1}]}`},
		{[]json.RawMessage{json.RawMessage(`{"a":1}`)}, `{"type":"object","values":[{"a":1}]}`},
		{[]doc{{A: 1}}, `{"type":"object","values":[{"a":1}]}`},
		{[]interface{}{map[string]interface{}{"a": 1}}, `{"type":"object","values":[{"a":1}]}`},
	}
	for _, tc := range cases {
		b, err := json.Marshal(gatewayRerankDocuments(tc.docs))
		if err != nil {
			t.Fatal(err)
		}
		if string(b) != tc.want {
			t.Errorf("%T: got %s, want %s", tc.docs, b, tc.want)
		}
	}
}

// TestR90provgErrorMessageStringify: TS getErrorMessage(data) is
// JSON.stringify of the parsed error body (insertion order, HTML left
// literal).
func TestR90provgErrorMessageStringify(t *testing.T) {
	got := gatewayErrorMessage([]byte(`{"z":1,"a":"<x>"}`), 400)
	if want := `{"z":1,"a":"<x>"}`; got != want {
		t.Fatalf("message = %s, want %s", got, want)
	}
}

// TestR90provgGenerationInfoFractional: TS gatewayGenerationInfoResponseSchema
// reads latency, generation_time and the token counts as z.number(), so a
// fractional value parses instead of failing the call.
func TestR90provgGenerationInfoFractional(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"data":{"id":"gen_1","model":"m","provider_name":"p","finish_reason":"stop","total_cost":1,"upstream_inference_cost":0,"usage":1,"is_byok":false,"streamed":true,"created_at":"x","native_tokens_prompt":10,"native_tokens_completion":5,"native_tokens_reasoning":0,"native_tokens_cached":0,"native_tokens_cache_creation":0,"latency":200.5,"generation_time":1500.25,"billable_web_search_calls":0}}`))
	}))
	defer server.Close()
	p, _ := New(Config{APIKey: "k", BaseURL: server.URL + "/v3/ai"})
	info, err := p.GetGenerationInfo(context.Background(), GenerationInfoParams{ID: "gen_1"})
	if err != nil {
		t.Fatal(err)
	}
	if info.Latency != 200 || info.GenerationTime != 1500 || info.PromptTokens != 10 {
		t.Fatalf("info = %+v", info)
	}
}

// TestR90provgGenerationInfoEmptyID: TS sends the request for an empty id
// (`id=`) and reports the API's answer; there is no client-side check.
func TestR90provgGenerationInfoEmptyID(t *testing.T) {
	var query string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.RawQuery
		w.WriteHeader(http.StatusNotFound)
		_, _ = w.Write([]byte(`{"error":{"message":"not found","type":"not_found"}}`))
	}))
	defer server.Close()
	p, _ := New(Config{APIKey: "k", BaseURL: server.URL + "/v3/ai"})
	if _, err := p.GetGenerationInfo(context.Background(), GenerationInfoParams{}); err == nil {
		t.Fatal("expected the API error")
	}
	if query != "id=" {
		t.Fatalf("query = %q, want id=", query)
	}
}

// TestR90provgSpendReportFractionalAndEmptyDates: TS gateway-spend-report.ts
// reads the counts as z.number() and always sends start_date / end_date,
// even empty, leaving validation to the API.
func TestR90provgSpendReportFractionalAndEmptyDates(t *testing.T) {
	var query string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		query = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"results":[{"total_cost":1,"input_tokens":10.5,"request_count":2}]}`))
	}))
	defer server.Close()
	p, _ := New(Config{APIKey: "k", BaseURL: server.URL + "/v3/ai"})
	res, err := p.GetSpendReport(context.Background(), SpendReportParams{})
	if err != nil {
		t.Fatal(err)
	}
	if query != "start_date=&end_date=" {
		t.Fatalf("query = %q", query)
	}
	if len(res.Results) != 1 || res.Results[0].InputTokens != 10 || res.Results[0].RequestCount != 2 {
		t.Fatalf("results = %+v", res.Results)
	}
}

// TestR90provgClientSecretSchema: TS gatewayClientSecretResponseSchema takes
// token: z.string() (an empty string passes) and expiresAt: z.number()
// (a fractional value parses).
func TestR90provgClientSecretSchema(t *testing.T) {
	serverURL, closeServer := newGatewayIPv4TestServer(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"token":"","expiresAt":1893456000.5}`))
	}))
	defer closeServer()
	p, _ := New(Config{APIKey: "k", BaseURL: serverURL + "/v4/ai"})
	secret, err := p.MintRealtimeClientSecret(context.Background(), MintRealtimeClientSecretParams{ModelID: "openai/gpt-realtime"})
	if err != nil {
		t.Fatal(err)
	}
	if secret.Token != "" || secret.ExpiresAt == nil || *secret.ExpiresAt != 1893456000 {
		t.Fatalf("secret = %+v", secret)
	}
}

// TestR90provgMetadataEmptyModelType: TS gatewayAvailableModelsResponseSchema
// keeps a model only when modelType is null/absent or a known type, so an
// empty modelType is filtered out.
func TestR90provgMetadataEmptyModelType(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"models":[` +
			`{"id":"a/x","name":"X","specification":{"specificationVersion":"v4","provider":"a","modelId":"x"},"modelType":""},` +
			`{"id":"a/y","name":"Y","specification":{"specificationVersion":"v4","provider":"a","modelId":"y"},"modelType":null},` +
			`{"id":"a/z","name":"Z","specification":{"specificationVersion":"v4","provider":"a","modelId":"z"},"modelType":"image"}]}`))
	}))
	defer server.Close()
	p, _ := New(Config{APIKey: "k", BaseURL: server.URL})
	meta, err := p.GetAvailableModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(meta.Models) != 2 || meta.Models[0].ID != "a/y" || meta.Models[1].ModelType != "image" {
		t.Fatalf("models = %+v", meta.Models)
	}
}

// TestR90provgVideoEmptyLists: TS buildRequestBody sends frameImages and
// inputReferences whenever they are present (`frameImages && ...`), an empty
// list included.
func TestR90provgVideoEmptyLists(t *testing.T) {
	p, _ := New(Config{APIKey: "k", BaseURL: "https://example.com/v4/ai"})
	m := NewVideoModel(p, "google/veo-3.0-generate-001")
	body, err := m.buildRequestBody(&provider.VideoModelV3CallOptions{Prompt: "p", FrameImages: []provider.VideoFrameImage{}, InputReferences: []provider.VideoModelV3File{}})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := body["frameImages"]; !ok {
		t.Fatalf("frameImages missing: %v", body)
	}
	if _, ok := body["inputReferences"]; !ok {
		t.Fatalf("inputReferences missing: %v", body)
	}
}

// TestR90provgBatchModelMismatchQuoting: TS validateSingleModel writes
// `Found "${modelId}" and "${request.modelId}".` (gateway-batch.ts), quoting
// the ids as is.
func TestR90provgBatchModelMismatchQuoting(t *testing.T) {
	_, err := validateGatewayTextBatchRequests([]provider.BatchV4Request{
		{Type: provider.BatchRequestTypeText, Text: &provider.TextBatchV4Request{ID: "1", ModelID: `a\b`}},
		{Type: provider.BatchRequestTypeText, Text: &provider.TextBatchV4Request{ID: "2", ModelID: "c"}},
	})
	if err == nil || !strings.Contains(err.Error(), `Found "a\b" and "c".`) {
		t.Fatalf("err = %v", err)
	}
	err = unsupportedGatewayBatchRequestType(`x\y`)
	if !strings.Contains(err.Error(), `with type "x\y".`) {
		t.Fatalf("err = %v", err)
	}
}

// TestR90provgDecisionStateBeforeOptions: TS doDecide runs
// toGatewayDecisionState (which rejects URL file data) before
// parseProviderOptions, so a URL file fails first even with invalid gateway
// options.
func TestR90provgDecisionStateBeforeOptions(t *testing.T) {
	p, _ := New(Config{APIKey: "k", BaseURL: "https://example.com/v4/ai"})
	m := NewDecisionModel(p, "openai/gpt-5")
	_, err := m.DoDecide(context.Background(), provider.DecisionCallOptions{
		State:           []provider.DecisionStatePart{{Type: "file", File: &types.FileContent{FileData: types.FileData{Type: types.FileDataTypeURL, URL: "https://x/y.png"}, MediaType: "image/png"}}},
		Questions:       map[string]provider.DecisionQuestion{"q": {Type: "boolean"}},
		ProviderOptions: map[string]interface{}{"gateway": map[string]interface{}{"models": 5}},
	})
	if err == nil || !strings.Contains(err.Error(), "Gateway decision file input: url data") {
		t.Fatalf("err = %v", err)
	}
}
