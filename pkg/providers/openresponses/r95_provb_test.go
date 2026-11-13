package openresponses

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/internal/tsfixture"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TestR95provbImageFullMediaType (cr-r95-tools-to-provb): TS
// convert-to-open-responses-input uses resolveFullMediaType for an inline
// image, so "image/" or "image/*" on PNG bytes is sent as image/png.
func TestR95provbImageFullMediaType(t *testing.T) {
	png := []byte("\x89PNG\r\n\x1a\n0000")
	for _, mt := range []string{"image", "image/", "image/*"} {
		var warnings []types.Warning
		out, err := convertUserContent([]types.ContentPart{types.FileContent{MediaType: mt, Data: png}}, &warnings, "openResponses")
		if err != nil {
			t.Fatalf("%q: %v", mt, err)
		}
		img, ok := out[0].(InputImageContent)
		if !ok || !strings.HasPrefix(img.ImageURL, "data:image/png;base64,") {
			t.Errorf("%q: part = %#v, want a data:image/png input image", mt, out[0])
		}
	}
}

func r95provbGenerate(t *testing.T, body string) *types.GenerateResult {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(body))
	}))
	defer srv.Close()
	res, err := New(Config{BaseURL: srv.URL, Name: "test"}).languageModelForTest("m").DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: r91provbUser("hi")})
	if err != nil {
		t.Fatal(err)
	}
	return res
}

// TestR95provbFinishReasonLikeTS: TS mapOpenResponsesFinishReason maps
// max_output_tokens to length and content_filter to content-filter even
// when the response has tool calls, and a present reason it does not know
// ("" or a non-string included) to other (tool-calls with tool calls); only
// a missing reason is stop.
func TestR95provbFinishReasonLikeTS(t *testing.T) {
	call := `{"type":"function_call","id":"fc","call_id":"c1","name":"f","arguments":"{}"}`
	for _, tc := range []struct {
		output, details string
		want            types.FinishReason
	}{
		{call, `{"reason":"max_output_tokens"}`, types.FinishReasonLength},
		{call, `{"reason":"content_filter"}`, types.FinishReasonContentFilter},
		{``, `{"reason":""}`, types.FinishReasonOther},
		{``, `{"reason":"max_tokens"}`, types.FinishReasonOther},
		{call, `{"reason":"stop"}`, types.FinishReasonToolCalls},
		{``, `{}`, types.FinishReasonStop},
	} {
		res := r95provbGenerate(t, `{"id":"r","created_at":1,"model":"m","status":"incomplete","output":[`+tc.output+`],"incomplete_details":`+tc.details+`}`)
		if res.FinishReason != tc.want {
			t.Errorf("%s %s: finish = %q, want %q", tc.output, tc.details, res.FinishReason, tc.want)
		}
	}

	s := newOpenResponsesStream(io.NopCloser(strings.NewReader(`data: {"type":"response.incomplete","response":{"incomplete_details":{"reason":""}}}

`)), nil, "test")
	var finish *provider.StreamChunk
	for {
		c, err := s.Next()
		if err != nil {
			break
		}
		if c.Type == provider.ChunkTypeFinish {
			finish = c
		}
	}
	if finish == nil || finish.FinishReason != types.FinishReasonOther {
		t.Errorf("stream finish = %+v, want other", finish)
	}
}

// TestR95provbEmptySystemMessageInstructions: TS joins every system
// message's content into instructions when there is one, so a system
// message of "" is sent as instructions: "" (no system message: no key).
func TestR95provbEmptySystemMessageInstructions(t *testing.T) {
	lm := New(Config{BaseURL: "http://localhost/v1", Name: "test"}).languageModelForTest("m")
	for _, tc := range []struct {
		name     string
		messages []types.Message
		want     bool
	}{
		{"empty system", []types.Message{{Role: types.RoleSystem, Content: []types.ContentPart{types.TextContent{Text: ""}}}, {Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}}}, true},
		{"no system", []types.Message{{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "hi"}}}}, false},
	} {
		body, _, err := lm.(*LanguageModel).buildRequestBody(&provider.GenerateOptions{Prompt: types.Prompt{Messages: tc.messages}}, false)
		if err != nil {
			t.Fatal(err)
		}
		v, ok := body["instructions"]
		if ok != tc.want || (ok && v != "") {
			t.Errorf("%s: instructions = %#v (present %v), want present %v", tc.name, v, ok, tc.want)
		}
	}
}

// TestR95provbTSCasesFinishReasonTS replays map-open-responses-finish-reason
// .test.ts against the mapping the language model uses
// (mapOpenResponsesFinishReasonTS): an undefined or null reason is absent.
func TestR95provbTSCasesFinishReasonTS(t *testing.T) {
	cases := tsfixture.LoadCases(t, "testdata/tscases/finish_reason.json.gz")
	tsfixture.RunFn(t, cases, func(args []interface{}) (interface{}, map[string]interface{}, error) {
		in, _ := args[0].(map[string]interface{})
		reason, present := in["finishReason"].(string)
		hasToolCalls, _ := in["hasToolCalls"].(bool)
		return string(mapOpenResponsesFinishReasonTS(reason, present, hasToolCalls)), nil, nil
	}, nil)
}
