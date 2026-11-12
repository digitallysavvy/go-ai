package deepseek

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func r94provbDeepSeekServer(t *testing.T, generateBody, streamBody string) provider.LanguageModel {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]interface{}
		_ = json.NewDecoder(r.Body).Decode(&body)
		if body["stream"] == true {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = io.WriteString(w, streamBody)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = io.WriteString(w, generateBody)
	}))
	t.Cleanup(srv.Close)
	lm, err := New(Config{APIKey: "k", BaseURL: srv.URL}).ResponsesModel("deepseek-v4")
	if err != nil {
		t.Fatal(err)
	}
	return lm
}

// TestR94provbResponsesUsageRawSchemaKeys: TS usage.raw is the
// createUsageSchema output (z.object), so unknown keys are dropped at the top
// level and inside the nested detail objects; a null member is kept.
func TestR94provbResponsesUsageRawSchemaKeys(t *testing.T) {
	const usage = `{"input_tokens":3,"input_tokens_details":{"cached_tokens":1,"extra":5},"output_tokens":2,"output_tokens_details":null,"total_tokens":5,"server_tool_use":{"tool_search":1,"x":2},"prompt_cache_miss_tokens":9}`
	const want = `{"input_tokens":3,"input_tokens_details":{"cached_tokens":1},"output_tokens":2,"output_tokens_details":null,"server_tool_use":{"tool_search":1},"total_tokens":5}`
	lm := r94provbDeepSeekServer(t,
		`{"id":"r1","status":"completed","output":[],"usage":`+usage+`}`,
		`data: {"type":"response.completed","response":{"id":"r1","status":"completed","usage":`+usage+`}}`+"\n\n")
	opts := &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}}
	res, err := lm.DoGenerate(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	if got, _ := json.Marshal(res.Usage.Raw); string(got) != want {
		t.Errorf("generate raw usage = %s, want %s", got, want)
	}
	s, err := lm.DoStream(context.Background(), opts)
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close() //nolint:errcheck
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
	if finish == nil || finish.Usage == nil {
		t.Fatal("no finish usage")
	}
	if got, _ := json.Marshal(finish.Usage.Raw); string(got) != want {
		t.Errorf("stream raw usage = %s, want %s", got, want)
	}
}

// TestR94provbResponsesFailedBodyAndEmptyReason: TS throws the failed
// response with responseBody JSON.stringify(rawResponse) (compact), and the
// raw finish reason is `incomplete_details?.reason ?? status`, so a present
// "" reason is kept.
func TestR94provbResponsesFailedBodyAndEmptyReason(t *testing.T) {
	lm := r94provbDeepSeekServer(t, "{\n  \"status\": \"failed\",\n  \"error\": {\"code\": \"timeout\", \"message\": \"slow\"}\n}", "")
	_, err := lm.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	var pe *providererrors.ProviderError
	if !errors.As(err, &pe) {
		t.Fatalf("err = %T %v", err, err)
	}
	if pe.ResponseBody != `{"status":"failed","error":{"code":"timeout","message":"slow"}}` {
		t.Errorf("responseBody = %q", pe.ResponseBody)
	}

	lm = r94provbDeepSeekServer(t, `{"id":"r1","status":"incomplete","incomplete_details":{"reason":""},"output":[]}`,
		`data: {"type":"response.incomplete","response":{"id":"r1","status":"incomplete","incomplete_details":{"reason":""}}}`+"\n\n")
	res, err := lm.DoGenerate(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	if res.RawFinishReason != "" || res.FinishReason != types.FinishReasonOther {
		t.Errorf("generate finish = %q / %q, want other / \"\"", res.FinishReason, res.RawFinishReason)
	}
	s, err := lm.DoStream(context.Background(), &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}})
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close() //nolint:errcheck
	for {
		c, err := s.Next()
		if err != nil {
			t.Fatal("no finish")
		}
		if c.Type == provider.ChunkTypeFinish {
			if c.RawFinishReason != "" {
				t.Errorf("stream raw finish = %q, want \"\"", c.RawFinishReason)
			}
			return
		}
	}
}

// TestR94provbFilesUnfollowed3xxIsError (cr-r94-provg-to-provb): TS
// postFormDataToApi checks response.ok, so a 300 is the API error.
func TestR94provbFilesUnfollowed3xxIsError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusMultipleChoices)
		_, _ = io.WriteString(w, `{"id":"file-1"}`)
	}))
	defer srv.Close()
	_, err := New(Config{APIKey: "k", BaseURL: srv.URL}).Files().UploadFile(context.Background(), types.UploadFileOptions{
		Data: types.FileData{Type: types.FileDataTypeData, Data: []byte{0x89, 'P', 'N', 'G', 0x0d, 0x0a, 0x1a, 0x0a}}, MediaType: "image/png", Filename: "a.png"})
	var pe *providererrors.ProviderError
	if !errors.As(err, &pe) || pe.StatusCode != http.StatusMultipleChoices {
		t.Fatalf("err = %T %v, want the 300 API error", err, err)
	}
}

type r94provbClosingReader struct{ closed bool }

func (r *r94provbClosingReader) Read([]byte) (int, error) { return 0, io.EOF }
func (r *r94provbClosingReader) Close() error           { r.closed = true; return nil }

// TestR94provbFilesStreamUploadUnsupported (cr-r94-prova-to-provb): TS
// convertInlineFileDataToUint8Array cancels a stream and throws
// UnsupportedFunctionalityError('streaming file upload') before any request.
func TestR94provbFilesStreamUploadUnsupported(t *testing.T) {
	stream := &r94provbClosingReader{}
	_, err := New(Config{APIKey: "k", BaseURL: "http://127.0.0.1:1"}).Files().UploadFile(context.Background(), types.UploadFileOptions{
		Data: types.FileData{Type: types.FileDataTypeStream, Stream: stream}, MediaType: "image/png", Filename: "a.png"})
	var ufe *providererrors.UnsupportedFunctionalityError
	if !errors.As(err, &ufe) || ufe.Functionality != "streaming file upload" || !stream.closed {
		t.Fatalf("err = %T %v, closed %v; want UnsupportedFunctionalityError and a closed stream", err, err, stream.closed)
	}
}
