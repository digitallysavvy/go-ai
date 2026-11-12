package openai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

const r94provbErrorFrame = `{"error":{"code":"insufficient_quota","message":"quota exceeded","type":"insufficient_quota","param":null,"extra":1},"other":2}`

// TestR94provbEarlyStreamErrorDataIsErrorField: TS Chat and Completions
// streams pass chunk.error (the openaiErrorDataSchema output: message, type,
// param, code) to throwIfOpenAIStreamErrorBeforeOutput, so the thrown
// APICallError's data is that inner object and its responseBody is
// JSON.stringify of it, not the whole frame.
func TestR94provbEarlyStreamErrorDataIsErrorField(t *testing.T) {
	const wantBody = `{"message":"quota exceeded","type":"insufficient_quota","param":null,"code":"insufficient_quota"}`
	for name, next := range map[string]func() error{
		"chat": func() error {
			s := newOpenAIStream(io.NopCloser(strings.NewReader("data: "+r94provbErrorFrame+"\n\n")), true)
			defer s.Close() //nolint:errcheck
			_, err := s.Next()
			return err
		},
		"completion": func() error {
			s := newCompletionStream(io.NopCloser(strings.NewReader("data: "+r94provbErrorFrame+"\n\n")), false)
			defer s.Close() //nolint:errcheck
			_, err := s.Next()
			return err
		},
	} {
		var pe *providererrors.ProviderError
		if err := next(); !errors.As(err, &pe) {
			t.Fatalf("%s: err = %T %v", name, err, err)
		}
		if pe.ResponseBody != wantBody {
			t.Errorf("%s: responseBody = %s, want %s", name, pe.ResponseBody, wantBody)
		}
		if got, _ := json.Marshal(pe.Data); string(got) != `{"code":"insufficient_quota","message":"quota exceeded","param":null,"type":"insufficient_quota"}` {
			t.Errorf("%s: data = %s", name, got)
		}
		if pe.StatusCode != 429 || pe.IsRetryable() {
			t.Errorf("%s: status %d retryable %v", name, pe.StatusCode, pe.IsRetryable())
		}
	}
}

// TestR94provbMidStreamErrorDataIsSchemaOutput: after output has started,
// TS createOpenAIProviderStreamError(value.error) gets the parsed chunk, so
// the stream error's data has only the schema keys.
func TestR94provbMidStreamErrorDataIsSchemaOutput(t *testing.T) {
	s := newOpenAIStream(io.NopCloser(strings.NewReader(
		`data: {"id":"c1","choices":[{"index":0,"delta":{"role":"assistant","content":"Hi"},"finish_reason":null}]}`+"\n\n"+
			"data: "+r94provbErrorFrame+"\n\n")), true)
	defer s.Close() //nolint:errcheck
	for {
		c, err := s.Next()
		if err != nil {
			t.Fatal("no error part")
		}
		if c.Type != provider.ChunkTypeError {
			continue
		}
		var spe *providererrors.StreamProviderError
		if !errors.As(c.Err, &spe) {
			t.Fatalf("err = %T", c.Err)
		}
		if got, _ := json.Marshal(spe.Data); string(got) != `{"code":"insufficient_quota","message":"quota exceeded","param":null,"type":"insufficient_quota"}` {
			t.Errorf("data = %s", got)
		}
		return
	}
}

// TestR94provbEditResolvesKeyBeforeDownload (cr-r94-tools-to-provb): TS
// resolves config.headers before it downloads URL edit inputs, so a missing
// key or a failing APIKeyFunc is the error even when the download would fail
// (OpenAI image edits and the OpenAI-compatible image edits).
func TestR94provbEditResolvesKeyBeforeDownload(t *testing.T) {
	t.Setenv("OPENAI_API_KEY", "")
	edit := &provider.ImageGenerateOptions{Prompt: "x", Files: []provider.ImageFile{{Type: "url", URL: "https://127.0.0.1:1/x.png"}}}
	_, err := NewImageModel(New(Config{}), "gpt-image-1").DoGenerate(context.Background(), edit)
	if !providererrors.IsLoadAPIKeyError(err) {
		t.Fatalf("openai: err = %T %v, want LoadAPIKeyError", err, err)
	}
	keyErr := errors.New("r94provb key")
	calls := 0
	keyFunc := func(context.Context) (string, error) { calls++; return "", keyErr }
	_, err = NewImageModel(New(Config{APIKeyFunc: keyFunc, Name: "acme", BaseURL: "https://example.invalid/v1", OpenAICompatible: true}), "m").DoGenerate(context.Background(), edit)
	if !errors.Is(err, keyErr) || calls != 1 {
		t.Fatalf("compat: err = %T %v (calls %d), want the key function error", err, err, calls)
	}
}
