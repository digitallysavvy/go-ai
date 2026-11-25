package google

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"reflect"
	"regexp"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS: google-extraction.test.ts 'extracts typed fields with Gemini structured output'
func TestPortprovgGoogleExtractionModel(t *testing.T) {
	fixture, err := os.ReadFile("testdata/port2/extraction.json")
	if err != nil {
		t.Fatal(err)
	}
	var calls int
	var gotPath string
	var gotBody map[string]interface{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls++
		gotPath = r.URL.Path
		b, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(b, &gotBody)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(fixture)
	}))
	defer server.Close()

	p := New(Config{APIKey: "test-key", BaseURL: server.URL + "/v1beta"})
	var _ provider.ExtractionModelProvider = p
	model, err := p.ExtractionModel("gemini-3.8-flash")
	if err != nil {
		t.Fatal(err)
	}
	decimals := 2
	result, err := model.DoExtract(context.Background(), provider.ExtractionCallOptions{
		State:         []provider.DecisionStatePart{provider.DecisionTextPart("Invoice INV-2041. Total due $1,250.00 by March 3. Contacts: Ana, Bo.")},
		ReferenceDate: "2026-11-11",
		Fields: map[string]provider.ExtractionField{
			"invoiceNumber": {Type: "text"},
			"total":         {Type: "number", Decimals: &decimals},
			"dueDate":       {Type: "date"},
			"contacts":      {Type: "list", Items: "text"},
			"purchaseOrder": {Type: "text"},
		},
		FieldOrder: []string{"invoiceNumber", "total", "dueDate", "contacts", "purchaseOrder"},
	})
	if err != nil {
		t.Fatal(err)
	}
	if model.Provider() != "google.extraction" || model.ModelID() != "gemini-3.8-flash" {
		t.Fatalf("provider %q model %q", model.Provider(), model.ModelID())
	}
	want := map[string]provider.ExtractionAnswer{
		"invoiceNumber": {Type: "text", Value: "INV-2041"},
		"total":         {Type: "number", Value: float64(1250)},
		"dueDate":       {Type: "date", Value: "2027-03-03"},
		"contacts":      {Type: "list", Value: []interface{}{"Ana", "Bo"}},
		"purchaseOrder": {Type: "text", Value: nil},
	}
	if !reflect.DeepEqual(result.Answers, want) {
		t.Fatalf("answers = %#v", result.Answers)
	}
	if calls != 1 {
		t.Fatalf("calls = %d", calls)
	}
	if gotPath != "/v1beta/models/gemini-3.8-flash:generateContent" {
		t.Fatalf("path = %q", gotPath)
	}
	cfg, _ := gotBody["generationConfig"].(map[string]interface{})
	if cfg["responseMimeType"] != "application/json" {
		t.Fatalf("generationConfig = %v", cfg)
	}
	schema, _ := cfg["responseJsonSchema"].(map[string]interface{})
	if !reflect.DeepEqual(schema["required"], []interface{}{"f0", "f1", "f2", "f3", "f4"}) {
		t.Fatalf("required = %v", schema["required"])
	}
}

// TS parseOffsetSeconds is Number.parseFloat(offset): a wire audioOffset that
// is not a string or number reads as its String() form (known difference
// closed: arrays, booleans and objects).
func TestPortprovgLiveOffsetReadsAsJSString(t *testing.T) {
	cases := []struct {
		in   interface{}
		want float64
		ok   bool
	}{
		{"0.280s", 0.28, true},
		{float64(2), 2, true},
		{[]interface{}{"2s"}, 2, true},
		{[]interface{}{float64(1.5), float64(9)}, 1.5, true},
		{[]interface{}{[]interface{}{"3s"}}, 3, true},
		{[]interface{}{nil, "3s"}, 0, false},
		{[]interface{}{}, 0, false},
		{true, 0, false},
		{map[string]interface{}{"a": 1}, 0, false},
		{nil, 0, false},
	}
	for _, c := range cases {
		got, ok := liveOffsetSeconds(c.in)
		if ok != c.ok || (ok && got != c.want) {
			t.Errorf("%#v: got %v %v, want %v %v", c.in, got, ok, c.want, c.ok)
		}
	}
}

// TS: google-file-inspection.test.ts '%s requests' / 'sends inspection fields
// to v1beta with streaming: %s' (generateContent and interactions x
// streaming false / true).
func TestPortprovgFileInspectionRequestsSendV1beta(t *testing.T) {
	const modelID = "gemini-4-hydrogen"
	metadata := map[string]interface{}{
		"mediaResolution":  map[string]interface{}{"level": "MEDIA_RESOLUTION_HIGH"},
		"documentMetadata": map[string]interface{}{"startPage": 3, "endPage": 4},
	}
	pdf := portprovgInteractionsCases()[0]
	for _, surface := range []string{"generateContent", "interactions"} {
		for _, streaming := range []bool{false, true} {
			name := surface + "/stream=" + map[bool]string{false: "false", true: "true"}[streaming]
			t.Run(name, func(t *testing.T) {
				var requestURL string
				var requestBody map[string]interface{}
				srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					requestURL = r.URL.Path
					b, _ := io.ReadAll(r.Body)
					_ = json.Unmarshal(b, &requestBody)
					if surface == "interactions" {
						// A user_input wrapper uses a different nested-result
						// schema; top-level function_result inputs accept EAP files.
						steps, _ := requestBody["input"].([]interface{})
						for _, step := range steps {
							content, _ := step.(map[string]interface{})["content"].([]interface{})
							for _, c := range content {
								cm, _ := c.(map[string]interface{})
								results, _ := cm["result"].([]interface{})
								if cm["type"] != "function_result" {
									continue
								}
								for _, part := range results {
									switch part.(map[string]interface{})["type"] {
									case "function", "image", "text", "video":
									default:
										w.Header().Set("Content-Type", "application/json")
										w.WriteHeader(http.StatusBadRequest)
										_, _ = w.Write([]byte(`{"error":{"code":"invalid_request","message":"Unsupported content type in function_result.result."}}`))
										return
									}
								}
							}
						}
						file := "basic.json"
						if streaming {
							file = "basic.chunks.txt"
							w.Header().Set("Content-Type", "text/event-stream")
							for _, l := range strings.Split(string(interactionsFixture(t, file)), "\n") {
								if strings.TrimSpace(l) != "" {
									_, _ = fmt.Fprintf(w, "data: %s\n\n", l)
								}
							}
							return
						}
						w.Header().Set("Content-Type", "application/json")
						_, _ = w.Write(interactionsFixture(t, file))
						return
					}
					response := `{"candidates":[{"content":{"role":"model","parts":[{"text":"ok"}]},"finishReason":"STOP"}]}`
					if streaming {
						w.Header().Set("Content-Type", "text/event-stream")
						_, _ = fmt.Fprintf(w, "data: %s\n\n", response)
						return
					}
					w.Header().Set("Content-Type", "application/json")
					_, _ = w.Write([]byte(response))
				}))
				defer srv.Close()

				p := New(Config{APIKey: "test-key", BaseURL: srv.URL + "/v1beta"})
				var model provider.LanguageModel
				var err error
				google := metadata
				if surface == "interactions" {
					model, err = p.Interactions(modelID)
					google = pdf.options
				} else {
					model, err = p.LanguageModel(modelID)
				}
				if err != nil {
					t.Fatal(err)
				}
				opts := &provider.GenerateOptions{Prompt: types.Prompt{Messages: []types.Message{{Role: types.RoleTool, Content: []types.ContentPart{types.ToolResultContent{
					ToolCallID: "call-1", ToolName: "view_file",
					Output: &types.ToolResultOutput{Type: types.ToolResultOutputContent, Content: []types.ToolResultContentBlock{
						types.FileContentBlock{
							MediaType:       pdf.mediaType,
							FileData:        types.FileData{Type: types.FileDataTypeReference, Reference: types.ProviderReference{"google": portprovgInspectionURI}},
							ProviderOptions: map[string]interface{}{"google": google},
						},
					}},
				}}}}}}
				if streaming {
					s, err := model.DoStream(context.Background(), opts)
					if err != nil {
						t.Fatal(err)
					}
					var finished bool
					for _, c := range drainAll(t, s) {
						if c.Type == provider.ChunkTypeError {
							t.Fatalf("error chunk %+v", c)
						}
						if c.Type == provider.ChunkTypeFinish {
							finished = true
						}
					}
					if !finished {
						t.Fatal("no finish chunk")
					}
				} else {
					res, err := model.DoGenerate(context.Background(), opts)
					if err != nil {
						t.Fatal(err)
					}
					if res.Text == "" {
						t.Fatalf("no text in %+v", res)
					}
				}
				if !strings.HasPrefix(requestURL, "/v1beta/") {
					t.Fatalf("url = %q", requestURL)
				}
				toJSON := func(v interface{}) interface{} {
					b, _ := json.Marshal(v)
					var out interface{}
					_ = json.Unmarshal(b, &out)
					return out
				}
				if surface == "interactions" {
					if requestBody["model"] != modelID {
						t.Fatalf("model = %v", requestBody["model"])
					}
					block := map[string]interface{}{"uri": portprovgInspectionURI, "mime_type": "application/pdf"}
					for k, v := range pdf.expected {
						block[k] = v
					}
					want := []interface{}{map[string]interface{}{"type": "function_result", "call_id": "call-1", "name": "view_file", "result": []interface{}{block}}}
					if !reflect.DeepEqual(toJSON(requestBody["input"]), toJSON(want)) {
						t.Fatalf("input = %v", toJSON(requestBody["input"]))
					}
				} else {
					verb := "generateContent"
					if streaming {
						verb = "streamGenerateContent"
					}
					if !strings.Contains(requestURL, "/models/"+modelID+":"+verb) {
						t.Fatalf("url = %q", requestURL)
					}
					contents, _ := requestBody["contents"].([]interface{})
					parts, _ := contents[0].(map[string]interface{})["parts"].([]interface{})
					fr, _ := parts[0].(map[string]interface{})["functionResponse"].(map[string]interface{})
					inner, _ := fr["parts"].([]interface{})
					want := map[string]interface{}{"fileData": map[string]interface{}{"fileUri": portprovgInspectionURI, "mimeType": "application/pdf"}}
					for k, v := range metadata {
						want[k] = v
					}
					if len(inner) == 0 || !reflect.DeepEqual(toJSON(inner[0]), toJSON(want)) {
						t.Fatalf("functionResponse.parts = %v", toJSON(inner))
					}
				}
				matched := false
				for _, pattern := range model.(interface{ SupportedURLs() map[string][]string }).SupportedURLs()["*"] {
					if regexp.MustCompile(pattern).MatchString(portprovgInspectionURI) {
						matched = true
					}
				}
				if !matched {
					t.Fatal("the uploaded file URI must be a supported URL")
				}
			})
		}
	}
}
