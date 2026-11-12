package openai

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS 5a645e92d2 openai-responses-text-format-update.test.ts: the rows that
// throw before any request (the wire recording has no call for them, so
// TestTSWire skips them), each for generate and stream (TS describe.each).

var r92provbFormat = map[string]interface{}{
	"type":   "json_schema",
	"name":   "count",
	"schema": map[string]interface{}{"type": "object", "properties": map[string]interface{}{"total": map[string]interface{}{"type": "integer"}}, "required": []interface{}{"total"}, "additionalProperties": false},
	"strict": true,
}

var r92provbUser = types.Message{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "Count the crates."}}}

func r92provbUpdate(options map[string]interface{}, content string) types.Message {
	return types.Message{Role: types.RoleSystem, Content: []types.ContentPart{types.TextContent{Text: content}}, ProviderOptions: map[string]interface{}{"openai": options}}
}

// r92provbFormatRequest is the TS request helper: it returns the call error
// and the number of requests the server saw.
func r92provbFormatRequest(t *testing.T, method string, msgs []types.Message, openaiOpts map[string]interface{}, modelID string) (error, int32) {
	t.Helper()
	var calls int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		atomic.AddInt32(&calls, 1)
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer srv.Close()
	p := New(Config{APIKey: "test-api-key", BaseURL: srv.URL})
	m, err := p.ResponsesModel(modelID)
	if err != nil {
		t.Fatal(err)
	}
	opts := &provider.GenerateOptions{Prompt: types.Prompt{Messages: msgs}}
	if openaiOpts != nil {
		opts.ProviderOptions = map[string]interface{}{"openai": openaiOpts}
	}
	if method == "generate" {
		_, err = m.DoGenerate(context.Background(), opts)
	} else {
		var s provider.TextStream
		s, err = m.DoStream(context.Background(), opts)
		if s != nil {
			_ = s.Close()
		}
	}
	return err, atomic.LoadInt32(&calls)
}

func r92provbWantUnsupported(t *testing.T, err error, calls int32, functionality, message string) {
	t.Helper()
	var uf *providererrors.UnsupportedFunctionalityError
	if !errors.As(err, &uf) || uf.Functionality != functionality || (message != "" && uf.Message != message) {
		t.Fatalf("err = %#v, want UnsupportedFunctionalityError{%q, %q}", err, functionality, message)
	}
	if calls != 0 {
		t.Fatalf("server calls = %d, want 0", calls)
	}
}

func TestR92provbResponsesFormatUpdateRejections(t *testing.T) {
	for _, method := range []string{"generate", "stream"} {
		prefix := "Responses format updates (" + method + ") > "

		// TS 182: rejects incompatible settings %j.
		for _, c := range []struct {
			title string
			opts  map[string]interface{}
		}{
			{`{"reasoningMode":"pro"}`, map[string]interface{}{"reasoningMode": "pro"}},
			{`{"contextManagement":[{"type":"compaction","compactThreshold":1000}]}`, map[string]interface{}{"contextManagement": []interface{}{map[string]interface{}{"type": "compaction", "compactThreshold": 1000}}}},
			{`{"truncation":"auto"}`, map[string]interface{}{"truncation": "auto"}},
		} {
			t.Run(prefix+"rejects incompatible settings "+c.title, func(t *testing.T) {
				err, calls := r92provbFormatRequest(t, method, []types.Message{r92provbUpdate(map[string]interface{}{"textFormatUpdate": r92provbFormat}, ""), r92provbUser}, c.opts, "gpt-6-sol")
				r92provbWantUnsupported(t, err, calls, "Message-level textFormatUpdate",
					"Configuration updates require standard reasoning mode without automatic compaction or automatic truncation")
			})
		}

		// TS 201.
		t.Run(prefix+"rejects unsupported models instead of silently losing historical updates", func(t *testing.T) {
			err, calls := r92provbFormatRequest(t, method, []types.Message{r92provbUpdate(map[string]interface{}{"textFormatUpdate": r92provbFormat}, ""), r92provbUser}, nil, "gpt-5.4")
			r92provbWantUnsupported(t, err, calls, "Message-level textFormatUpdate", "Configuration updates are only supported by GPT-6 and later models")
		})

		// TS 216.
		t.Run(prefix+"requires an empty control message", func(t *testing.T) {
			err, calls := r92provbFormatRequest(t, method, []types.Message{r92provbUpdate(map[string]interface{}{"textFormatUpdate": r92provbFormat}, "Instructions"), r92provbUser}, nil, "gpt-6-sol")
			r92provbWantUnsupported(t, err, calls, "Message-level textFormatUpdate", "Message-level textFormatUpdate requires empty system message content.")
		})

		// TS 226.
		t.Run(prefix+"rejects adjacent reasoning and format updates", func(t *testing.T) {
			err, calls := r92provbFormatRequest(t, method, []types.Message{
				r92provbUpdate(map[string]interface{}{"reasoningEffortUpdate": "low"}, ""),
				r92provbUpdate(map[string]interface{}{"textFormatUpdate": r92provbFormat}, ""),
				r92provbUser,
			}, nil, "gpt-6-sol")
			r92provbWantUnsupported(t, err, calls, "Adjacent configuration updates", "")
		})

		// TS 240: rejects request-level effort with a leading format update
		// (previousResponseId: %s).
		for _, prev := range []interface{}{nil, "resp_previous"} {
			title := "undefined"
			opts := map[string]interface{}{"reasoningEffortUpdate": "high"}
			if prev != nil {
				title = prev.(string)
				opts["previousResponseId"] = prev
			}
			t.Run(prefix+"rejects request-level effort with a leading format update (previousResponseId: "+title+")", func(t *testing.T) {
				err, calls := r92provbFormatRequest(t, method, []types.Message{r92provbUpdate(map[string]interface{}{"textFormatUpdate": r92provbFormat}, ""), r92provbUser}, opts, "gpt-6-sol")
				r92provbWantUnsupported(t, err, calls, "Adjacent configuration updates",
					"Configuration updates cannot be consecutive. Set reasoningEffortUpdate and textFormatUpdate in the same empty system message, and omit request-level reasoningEffortUpdate when using message-level updates.")
			})
		}

		// TS 280.
		t.Run(prefix+"rejects json_object as an update format", func(t *testing.T) {
			err, calls := r92provbFormatRequest(t, method, []types.Message{r92provbUpdate(map[string]interface{}{"textFormatUpdate": map[string]interface{}{"type": "json_object"}}, ""), r92provbUser}, nil, "gpt-6-sol")
			var ia *providererrors.InvalidArgumentError
			if !errors.As(err, &ia) || calls != 0 {
				t.Fatalf("err = %#v, calls = %d", err, calls)
			}
		})
	}
}

// TS 2ebc78b654 openai-image-model.test.ts 800 "should use the configured
// fetch and abort signal when downloading a URL %s" (image, mask, markup):
// the edit's URL download runs under the call's context, and an aborted
// download fails the call before anything is posted. Go downloads through
// the DNS-pinned transport, not the provider HTTP client (known difference
// "DNS protection on by default", S2).
func TestR92provbImageEditDownloadAbort(t *testing.T) {
	for _, target := range []string{"image", "mask", "markup"} {
		t.Run("should use the configured fetch and abort signal when downloading a URL "+target, func(t *testing.T) {
			var calls int32
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				atomic.AddInt32(&calls, 1)
				w.WriteHeader(http.StatusInternalServerError)
			}))
			defer srv.Close()
			m, err := New(Config{APIKey: "test-api-key", BaseURL: srv.URL}).ImageModel("gpt-image-1")
			if err != nil {
				t.Fatal(err)
			}
			png := provider.ImageFile{Type: "file", Data: []byte{137, 80, 78, 71}, MediaType: "image/png"}
			url := provider.ImageFile{Type: "url", URL: "https://example.com/" + target + ".png"}
			opts := &provider.ImageGenerateOptions{Prompt: "edit", Files: []provider.ImageFile{png}}
			switch target {
			case "image":
				opts.Files = []provider.ImageFile{url}
			case "mask":
				opts.Mask = &url
			case "markup":
				opts.ProviderOptions = map[string]interface{}{"openai": map[string]interface{}{"markup": url.URL}}
			}
			ctx, cancel := context.WithCancel(context.Background())
			cancel()
			_, err = m.DoGenerate(ctx, opts)
			if !errors.Is(err, context.Canceled) {
				t.Fatalf("err = %v, want the aborted download", err)
			}
			if n := atomic.LoadInt32(&calls); n != 0 {
				t.Fatalf("posted %d requests after the aborted download", n)
			}
		})
	}
}

func r92provbCompletionKinds(t *testing.T, s *completionStream) ([]provider.ChunkType, error) {
	t.Helper()
	var kinds []provider.ChunkType
	for {
		c, err := s.Next()
		if err != nil {
			if err == io.EOF {
				return kinds, nil
			}
			return kinds, err
		}
		kinds = append(kinds, c.Type)
	}
}

// TS openai-completion-language-model.ts doStream: the early-error peek
// (throwIfOpenAIStreamErrorBeforeOutput) ends at a chunk that fails to
// parse, so the parts read before it come first and a later error frame is
// an error part. TS OpenAICompatibleCompletionLanguageModel has no peek:
// nothing is held back, and an error frame is an error part in arrival
// order.
func TestR92provbCompletionStreamHeldChunkOrder(t *testing.T) {
	first := "data: {\"id\":\"c\",\"object\":\"text_completion\",\"created\":1,\"model\":\"m\",\"choices\":[{\"text\":\"\",\"index\":0,\"logprobs\":null,\"finish_reason\":null}]}\n\n"
	errFrame := "data: {\"error\":{\"message\":\"boom\",\"type\":\"server_error\",\"code\":null}}\n\n"
	t.Run("openai: a parse failure ends the early-error peek", func(t *testing.T) {
		s := newCompletionStream(io.NopCloser(strings.NewReader(first+"data: {bad\n\n"+errFrame)), false)
		kinds, err := r92provbCompletionKinds(t, s)
		want := []provider.ChunkType{provider.ChunkTypeResponseMetadata, provider.ChunkTypeTextStart, provider.ChunkTypeError, provider.ChunkTypeError, provider.ChunkTypeTextEnd, provider.ChunkTypeFinish}
		if err != nil || !reflect.DeepEqual(kinds, want) {
			t.Fatalf("parts = %v, %v; want %v", kinds, err, want)
		}
	})
	t.Run("openai-compatible: no chunk is held back", func(t *testing.T) {
		s := newCompletionStream(io.NopCloser(strings.NewReader("data: {\"id\":\"c\",\"created\":1,\"model\":\"m\",\"choices\":[]}\n\n"+errFrame)), true)
		s.compat = true
		kinds, err := r92provbCompletionKinds(t, s)
		want := []provider.ChunkType{provider.ChunkTypeRaw, provider.ChunkTypeResponseMetadata, provider.ChunkTypeTextStart, provider.ChunkTypeRaw, provider.ChunkTypeError, provider.ChunkTypeTextEnd, provider.ChunkTypeFinish}
		if err != nil || !reflect.DeepEqual(kinds, want) {
			t.Fatalf("parts = %v, %v; want %v", kinds, err, want)
		}
	})
}

// TS loadApiKey throws LoadAPIKeyError from the headers, before the fetch,
// and it propagates unchanged; the chat, completion, embedding and
// Responses handlers already returned it as is, the image, speech and
// transcription handlers wrapped it in a status-0 ProviderError.
func TestR92provbMissingAPIKeyUnwrapped(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	p := New(Config{BaseURL: "http://127.0.0.1:1"})
	want := func(name string, err error) {
		t.Helper()
		if _, ok := err.(*providererrors.LoadAPIKeyError); !ok {
			t.Fatalf("%s: err = %T %v, want *LoadAPIKeyError", name, err, err)
		}
	}
	im, _ := p.ImageModel("dall-e-3")
	_, err := im.DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "x"})
	want("image", err)
	_, err = im.DoGenerate(context.Background(), &provider.ImageGenerateOptions{Prompt: "x", Files: []provider.ImageFile{{Type: "file", Data: []byte{137, 80, 78, 71}, MediaType: "image/png"}}})
	want("image edit", err)
	sm, _ := p.SpeechModel("tts-1")
	_, err = sm.DoGenerate(context.Background(), &provider.SpeechGenerateOptions{Text: "x"})
	want("speech", err)
	tm, _ := p.TranscriptionModel("whisper-1")
	_, err = tm.DoTranscribe(context.Background(), &provider.TranscriptionOptions{Audio: []byte{1}, MimeType: "audio/wav"})
	want("transcription", err)

}

// CR r92 provc -> provb: TS sets limit, then after (openai-batch.ts:385-390).
func TestR92provbBatchListQueryOrder(t *testing.T) {
	var rawQuery string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rawQuery = r.URL.RawQuery
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"object":"list","data":[],"has_more":false}`))
	}))
	defer srv.Close()
	limit := 5
	_, err := NewBatch(New(Config{APIKey: "k", BaseURL: srv.URL})).DoListBatches(context.Background(), provider.BatchV4ListOptions{Limit: &limit, Cursor: "c"})
	if err != nil || rawQuery != "limit=5&after=c" {
		t.Fatalf("query = %q, err = %v", rawQuery, err)
	}
}
