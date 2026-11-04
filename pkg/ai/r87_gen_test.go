package ai

import (
	"context"
	"encoding/json"
	"reflect"
	"regexp"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

const r87genSecretURL = "https://user:password@model.example.test:8443/private?key=secret#fragment"

var r87genSecretPattern = regexp.MustCompile(`password|private|secret|fragment`)

// r87genURLStream is a provider stream that reports its request body and
// URL (provider.StreamRequestBody and the RequestURL capability).
type r87genURLStream struct {
	*testutil.MockTextStream
	body interface{}
	url  string
}

func (s *r87genURLStream) RequestBody() interface{} { return s.body }
func (s *r87genURLStream) RequestURL() string       { return s.url }

// TestR87genGenerateTextSanitizesRequestURL ports generate-text.test.ts
// (934100cad4) "sanitizes provider request URLs": the step results and the
// result request carry only the origin, whatever Include says.
func TestR87genGenerateTextSanitizesRequestURL(t *testing.T) {
	for _, include := range []*IncludeOptions{nil, {RequestBody: true, RequestMessages: true}} {
		model := &testutil.MockLanguageModel{
			DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
				return &types.GenerateResult{
					Text:         "hello",
					Content:      []types.ContentPart{types.TextContent{Text: "hello"}},
					FinishReason: types.FinishReasonStop,
					RawRequest:   map[string]interface{}{"model": "m"},
					RequestURL:   r87genSecretURL,
				}, nil
			},
		}
		result, err := GenerateText(context.Background(), GenerateTextOptions{Model: model, Prompt: "hello", Include: include})
		if err != nil {
			t.Fatal(err)
		}
		if result.Request.URL != "https://model.example.test:8443" {
			t.Errorf("include %v: result.Request.URL = %q", include, result.Request.URL)
		}
		if len(result.Steps) != 1 || result.Steps[0].Request.URL != "https://model.example.test:8443" {
			t.Errorf("include %v: step request = %+v", include, result.Steps)
		}
		raw, _ := json.Marshal(result)
		if r87genSecretPattern.Match(raw) {
			t.Errorf("include %v: result JSON leaks the URL: %s", include, raw)
		}
	}
}

// TestR87genGenerateTextOmitsNonHTTPRequestURL: TS sanitizeRequestMetadata
// drops a url that has no http(s) origin.
func TestR87genGenerateTextOmitsNonHTTPRequestURL(t *testing.T) {
	for _, u := range []string{"", "not a URL", "/relative", "file:///etc/passwd", "data:text/plain,secret"} {
		model := &testutil.MockLanguageModel{
			DoGenerateFunc: func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
				return &types.GenerateResult{Text: "x", FinishReason: types.FinishReasonStop, RequestURL: u}, nil
			},
		}
		result, err := GenerateText(context.Background(), GenerateTextOptions{Model: model, Prompt: "hello"})
		if err != nil {
			t.Fatal(err)
		}
		if result.Request.URL != "" || result.Steps[0].Request.URL != "" {
			t.Errorf("%q: request URL %q / %q, want none", u, result.Request.URL, result.Steps[0].Request.URL)
		}
	}
}

// TestR87genStreamObjectRequestBody: TS stream-object.ts resolves request
// (the result's and the step-end / end events') from doStream's request,
// so a provider stream's request body reaches them as in GenerateObject.
// Go reported an empty request for StreamObject.
func TestR87genStreamObjectRequestBody(t *testing.T) {
	body := map[string]interface{}{"model": "m", "stream": true}
	model := &testutil.MockLanguageModel{
		StructuredSupport: true,
		DoStreamFunc: func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
			return &r87genURLStream{
				MockTextStream: testutil.NewMockTextStream([]provider.StreamChunk{
					{Type: provider.ChunkTypeText, Text: `{"value":"hello"}`},
					{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
				}),
				body: body,
				url:  r87genSecretURL,
			}, nil
		},
	}
	var stepReq, endReq GenerateStepRequest
	result, err := StreamObject(context.Background(), StreamObjectOptions{
		Model:  model,
		Prompt: "hello",
		Schema: schema.NewSimpleJSONSchema(map[string]interface{}{"type": "object"}),
		OnStepEnd: func(ctx context.Context, e ObjectOnStepFinishEvent) {
			stepReq = e.Request
		},
		OnEnd: func(ctx context.Context, e ObjectOnFinishEvent) {
			endReq = e.Request
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	for name, got := range map[string]interface{}{"result": result.Request.Body, "step end": stepReq.Body, "end": endReq.Body} {
		if !reflect.DeepEqual(got, body) {
			t.Errorf("%s request body = %v, want %v", name, got, body)
		}
	}
	for name, v := range map[string]interface{}{"result": result.Request, "step end": stepReq, "end": endReq} {
		raw, _ := json.Marshal(v)
		if r87genSecretPattern.Match(raw) {
			t.Errorf("%s request leaks the URL: %s", name, raw)
		}
	}
}

// TestR87genStreamRequestURLCapability: the RequestURL capability is read
// through provider.TextStream, and a stream without it reports none.
func TestR87genStreamRequestURLCapability(t *testing.T) {
	s := &r87genURLStream{MockTextStream: testutil.NewMockTextStream(nil), url: r87genSecretURL}
	if got := streamRequestURL(s); got != r87genSecretURL {
		t.Errorf("streamRequestURL = %q", got)
	}
	if got := streamRequestURL(testutil.NewMockTextStream(nil)); got != "" {
		t.Errorf("streamRequestURL without the capability = %q", got)
	}
}
