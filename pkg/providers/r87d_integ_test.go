package providers_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync/atomic"
	"testing"

	"golang.org/x/oauth2"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/alibaba"
	"github.com/digitallysavvy/go-ai/pkg/providers/anthropic"
	"github.com/digitallysavvy/go-ai/pkg/providers/anthropicaws"
	"github.com/digitallysavvy/go-ai/pkg/providers/azure"
	"github.com/digitallysavvy/go-ai/pkg/providers/bedrock"
	bedrockanthropic "github.com/digitallysavvy/go-ai/pkg/providers/bedrock/anthropic"
	"github.com/digitallysavvy/go-ai/pkg/providers/bedrock/mantle"
	"github.com/digitallysavvy/go-ai/pkg/providers/cerebras"
	"github.com/digitallysavvy/go-ai/pkg/providers/cohere"
	"github.com/digitallysavvy/go-ai/pkg/providers/deepinfra"
	"github.com/digitallysavvy/go-ai/pkg/providers/deepseek"
	"github.com/digitallysavvy/go-ai/pkg/providers/elevenlabs"
	"github.com/digitallysavvy/go-ai/pkg/providers/fireworks"
	"github.com/digitallysavvy/go-ai/pkg/providers/gmicloud"
	"github.com/digitallysavvy/go-ai/pkg/providers/google"
	"github.com/digitallysavvy/go-ai/pkg/providers/googlevertex"
	vertexanthropic "github.com/digitallysavvy/go-ai/pkg/providers/googlevertex/anthropic"
	vertexxai "github.com/digitallysavvy/go-ai/pkg/providers/googlevertex/xai"
	"github.com/digitallysavvy/go-ai/pkg/providers/groq"
	"github.com/digitallysavvy/go-ai/pkg/providers/heygen"
	"github.com/digitallysavvy/go-ai/pkg/providers/huggingface"
	"github.com/digitallysavvy/go-ai/pkg/providers/minimax"
	"github.com/digitallysavvy/go-ai/pkg/providers/mistral"
	"github.com/digitallysavvy/go-ai/pkg/providers/moonshot"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
	"github.com/digitallysavvy/go-ai/pkg/providers/openresponses"
	"github.com/digitallysavvy/go-ai/pkg/providers/perplexity"
	"github.com/digitallysavvy/go-ai/pkg/providers/prodia"
	"github.com/digitallysavvy/go-ai/pkg/providers/quiverai"
	"github.com/digitallysavvy/go-ai/pkg/providers/together"
	"github.com/digitallysavvy/go-ai/pkg/providers/voyage"
	"github.com/digitallysavvy/go-ai/pkg/providers/xai"
	"github.com/digitallysavvy/go-ai/pkg/providers/zai"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// Round 87d integrator (cr-r87d-provrest-to-integrator, class sweep): a
// credential, signing or configuration error raised by a provider's own
// RoundTripper, or by the request-time key / token function, reaches the
// caller unchanged and is not retried. TS throws these from the provider's
// fetch wrapper (bedrock / anthropic-aws createSigV4FetchFunction, the azure
// tokenProvider fetch) or from `await resolve(this.config.headers)` before
// postToApi, and provider-utils handleFetchError returns a rejection that is
// not a failed fetch unchanged (handle-fetch-error.ts);
// retryWithExponentialBackoff retries only an APICallError with isRetryable
// (3fab50aa9b). v0.5 returned a ProviderError around a retryable "Cannot
// connect to API: Post <url>: ..." for the RoundTripper sites, and a
// ProviderError around the function's error for the key functions.

var errR87dintegCred = errors.New("r87d credential unavailable")

// r87dintegAWSText is the TS template (amazon-bedrock-provider.ts,
// bedrock-mantle-provider.ts, amazon-bedrock-anthropic-provider.ts,
// anthropic-aws-provider.ts) for a rejected credentialProvider.
const r87dintegAWSText = "AWS credential provider failed: r87d credential unavailable. Please ensure your credential provider returns valid AWS credentials with accessKeyId and secretAccessKey properties."

type r87dintegTokenSource struct{}

func (r87dintegTokenSource) Token() (*oauth2.Token, error) { return nil, errR87dintegCred }

type r87dintegCall struct {
	name string
	call func(ctx context.Context) error
}

func r87dintegLanguageCalls(name string, lm provider.LanguageModel, err error) []r87dintegCall {
	opts := func() *provider.GenerateOptions { return &provider.GenerateOptions{Prompt: types.Prompt{Text: "hi"}} }
	return []r87dintegCall{
		{name + " generate", func(ctx context.Context) error {
			if err != nil {
				return fmt.Errorf("model: %w", err)
			}
			_, e := lm.DoGenerate(ctx, opts())
			return e
		}},
		{name + " stream", func(ctx context.Context) error {
			if err != nil {
				return fmt.Errorf("model: %w", err)
			}
			s, e := lm.DoStream(ctx, opts())
			if e != nil {
				return e
			}
			defer s.Close() //nolint:errcheck
			for {
				if _, e := s.Next(); e != nil {
					if errors.Is(e, io.EOF) {
						return s.Err()
					}
					return e
				}
			}
		}},
	}
}

// r87dintegProviderCalls returns one call per non-empty model ID. A model
// the provider does not offer is reported as "model: ..." and skipped.
func r87dintegProviderCalls(name string, p provider.Provider, lang, embed, image, speech, transcribe string) []r87dintegCall {
	var calls []r87dintegCall
	if lang != "" {
		lm, err := p.LanguageModel(lang)
		calls = append(calls, r87dintegLanguageCalls(name, lm, err)...)
	}
	model := func(err error) error { return fmt.Errorf("model: %w", err) }
	if embed != "" {
		calls = append(calls, r87dintegCall{name + " embed", func(ctx context.Context) error {
			m, err := p.EmbeddingModel(embed)
			if err != nil {
				return model(err)
			}
			_, err = m.DoEmbed(ctx, "hi", nil)
			return err
		}})
	}
	if image != "" {
		calls = append(calls, r87dintegCall{name + " image", func(ctx context.Context) error {
			m, err := p.ImageModel(image)
			if err != nil {
				return model(err)
			}
			_, err = m.DoGenerate(ctx, &provider.ImageGenerateOptions{Prompt: "a cat"})
			return err
		}})
	}
	if speech != "" {
		calls = append(calls, r87dintegCall{name + " speech", func(ctx context.Context) error {
			m, err := p.SpeechModel(speech)
			if err != nil {
				return model(err)
			}
			_, err = m.DoGenerate(ctx, &provider.SpeechGenerateOptions{Text: "hi"})
			return err
		}})
	}
	if transcribe != "" {
		calls = append(calls, r87dintegCall{name + " transcribe", func(ctx context.Context) error {
			m, err := p.TranscriptionModel(transcribe)
			if err != nil {
				return model(err)
			}
			_, err = m.DoTranscribe(ctx, &provider.TranscriptionOptions{Audio: []byte("RIFF0000WAVE"), MimeType: "audio/wav"})
			return err
		}})
	}
	return calls
}

// r87dintegCheck runs c and reports whether it ran (false for a model the
// provider does not offer).
func r87dintegCheck(t *testing.T, c r87dintegCall, want func(error) bool) bool {
	t.Helper()
	err := c.call(context.Background())
	if err != nil && strings.HasPrefix(err.Error(), "model: ") {
		return false
	}
	if err == nil {
		t.Errorf("%s: no error", c.name)
		return true
	}
	if !want(err) {
		t.Errorf("%s: error %T %q, want the credential error unchanged", c.name, err, err.Error())
	}
	if strings.Contains(err.Error(), "Cannot connect to API") || strings.Contains(err.Error(), "provider error (") {
		t.Errorf("%s: credential error was wrapped: %q", c.name, err.Error())
	}
	if ai.IsGatewayCallRetryable(err) {
		t.Errorf("%s: credential error is retryable", c.name)
	}
	return true
}

// r87dintegSame: the error is the credential error itself (TS
// google-vertex-anthropic-provider-node.test.ts "propagates errors thrown
// from custom generateAuthToken": rejects.toThrow('token mint failed')).
func r87dintegSame(err error) bool { return err == errR87dintegCred }

func r87dintegAWS(err error) bool { return err.Error() == r87dintegAWSText }

func TestR87dintegTransportCredentialErrorsUnchanged(t *testing.T) {
	t.Setenv("AWS_REGION", "us-east-1")
	tokenErr := func(context.Context) (string, error) { return "", errR87dintegCred }
	ran := 0
	run := func(calls []r87dintegCall, want func(error) bool) {
		for _, c := range calls {
			if r87dintegCheck(t, c, want) {
				ran++
			}
		}
	}

	// Azure tokenProvider (TS: the fetch wrapper awaits tokenProvider()).
	az, err := azure.New(azure.Config{ResourceName: "r87d", ADTokenProvider: tokenErr})
	if err != nil {
		t.Fatal(err)
	}
	azCalls := r87dintegProviderCalls("azure", az, "gpt-4o", "text-embedding-3-small", "dall-e-3", "tts-1", "whisper-1")
	cm, err := az.ChatModel("gpt-4o")
	azCalls = append(azCalls, r87dintegLanguageCalls("azure chat", cm, err)...)
	co, err := az.CompletionModel("gpt-35-turbo-instruct")
	azCalls = append(azCalls, r87dintegLanguageCalls("azure completion", co, err)...)
	run(azCalls, r87dintegSame)

	// Google Vertex AuthToken, express-mode APIKeyFunc, MaaS and xAI.
	vx, err := googlevertex.New(googlevertex.Config{Project: "p", Location: "us-central1", AuthToken: tokenErr})
	if err != nil {
		t.Fatal(err)
	}
	vxCalls := r87dintegProviderCalls("vertex", vx, "gemini-2.5-flash", "text-embedding-005", "", "", "gemini-2.5-flash")
	am, err := vx.AnthropicModel("claude-sonnet-4-5@20250929")
	vxCalls = append(vxCalls, r87dintegLanguageCalls("vertex anthropic model", am, err)...)
	vxe, err := googlevertex.New(googlevertex.Config{APIKeyFunc: tokenErr})
	if err != nil {
		t.Fatal(err)
	}
	vxCalls = append(vxCalls, r87dintegProviderCalls("vertex express", vxe, "gemini-2.5-flash", "", "", "", "")...)
	maas := googlevertex.NewMaaS(googlevertex.MaaSConfig{Project: "p", Location: "us-central1", AuthToken: tokenErr})
	vxCalls = append(vxCalls, r87dintegProviderCalls("vertex maas", maas, "meta/llama-4-maverick-17b-128e-instruct-maas", "", "", "", "")...)
	xp, err := vertexxai.New(vertexxai.Config{Project: "p", Location: "us-central1", AuthToken: tokenErr})
	if err != nil {
		t.Fatal(err)
	}
	xm, err := xp.LanguageModel("grok-4")
	vxCalls = append(vxCalls, r87dintegLanguageCalls("vertex xai", xm, err)...)
	run(vxCalls, r87dintegSame)

	// Google Vertex Anthropic with a TokenSource: the Go oauth2 source's
	// error keeps its cause (known difference "Google credential loading
	// errors").
	va := vertexanthropic.New(vertexanthropic.Options{Project: "p", Location: "us-east5", TokenSource: r87dintegTokenSource{}})
	run(r87dintegProviderCalls("vertex anthropic", va, "claude-sonnet-4-5@20250929", "", "", "", ""),
		func(err error) bool { return errors.Is(err, errR87dintegCred) })

	// SigV4 credential providers (TS anthropic-aws-provider.test.ts "wraps
	// credentialProvider rejections with a guided message").
	cp := bedrock.CredentialProvider(func(context.Context) (bedrock.Credentials, error) {
		return bedrock.Credentials{}, errR87dintegCred
	})
	aws, err := anthropicaws.New(anthropicaws.Config{WorkspaceID: "ws", CredentialProvider: func(context.Context) (anthropicaws.Credentials, error) {
		return anthropicaws.Credentials{}, errR87dintegCred
	}})
	if err != nil {
		t.Fatal(err)
	}
	awsCalls := r87dintegProviderCalls("anthropicaws", aws, "claude-sonnet-4-5", "", "", "", "")
	awsCalls = append(awsCalls, r87dintegProviderCalls("mantle", mantle.New(mantle.ProviderSettings{CredentialProvider: cp}), "openai.gpt-oss-120b", "", "", "", "")...)
	rm, err := mantle.New(mantle.ProviderSettings{CredentialProvider: cp}).ResponsesModel("openai.gpt-oss-120b")
	awsCalls = append(awsCalls, r87dintegLanguageCalls("mantle responses", rm, err)...)
	awsCalls = append(awsCalls, r87dintegProviderCalls("bedrock anthropic", bedrockanthropic.New(bedrockanthropic.Config{CredentialProvider: cp}), "us.anthropic.claude-sonnet-4-5-20250929-v1:0", "", "", "", "")...)
	br := bedrock.New(bedrock.Config{CredentialProvider: cp})
	awsCalls = append(awsCalls, r87dintegProviderCalls("bedrock", br, "anthropic.claude-3-haiku-20240307-v1:0", "amazon.titan-embed-text-v2:0", "amazon.nova-canvas-v1:0", "", "")...)
	run(awsCalls, r87dintegAWS)

	if ran < 30 {
		t.Fatalf("only %d calls ran", ran)
	}
}

// Request-time key functions (TS apiKey given as a function, 7716e189ee):
// `await resolve(this.config.headers)` throws the function's error as is.
func TestR87dintegKeyFuncErrorsUnchanged(t *testing.T) {
	kf := providerutils.APIKeyFunc(func(context.Context) (string, error) { return "", errR87dintegCred })
	az, err := azure.New(azure.Config{ResourceName: "r87d", APIKeyFunc: kf})
	if err != nil {
		t.Fatal(err)
	}
	ps := map[string]provider.Provider{
		"azure":         az,
		"alibaba":       alibaba.New(alibaba.Config{APIKeyFunc: kf}),
		"anthropic":     anthropic.New(anthropic.Config{APIKeyFunc: kf}),
		"cerebras":      cerebras.New(cerebras.Config{APIKeyFunc: kf}),
		"cohere":        cohere.New(cohere.Config{APIKeyFunc: kf}),
		"deepinfra":     deepinfra.New(deepinfra.Config{APIKeyFunc: kf}),
		"deepseek":      deepseek.New(deepseek.Config{APIKeyFunc: kf}),
		"elevenlabs":    elevenlabs.New(elevenlabs.Config{APIKeyFunc: kf}),
		"fireworks":     fireworks.New(fireworks.Config{APIKeyFunc: kf}),
		"gmicloud":      gmicloud.New(gmicloud.Config{APIKeyFunc: kf}),
		"google":        google.New(google.Config{APIKeyFunc: kf}),
		"groq":          groq.New(groq.Config{APIKeyFunc: kf}),
		"heygen":        heygen.New(heygen.Config{APIKeyFunc: kf}),
		"huggingface":   huggingface.New(huggingface.Config{APIKeyFunc: kf}),
		"minimax":       minimax.New(minimax.Config{APIKeyFunc: kf}),
		"mistral":       mistral.New(mistral.Config{APIKeyFunc: kf}),
		"moonshot":      moonshot.New(moonshot.Config{APIKeyFunc: kf}),
		"openai":        openai.New(openai.Config{APIKeyFunc: kf}),
		"openresponses": openresponses.New(openresponses.Config{APIKeyFunc: kf, BaseURL: "http://127.0.0.1:1/v1"}),
		"perplexity":    perplexity.New(perplexity.Config{APIKeyFunc: kf}),
		"prodia":        prodia.New(prodia.Config{APIKeyFunc: kf}),
		"quiverai":      quiverai.New(quiverai.Config{APIKeyFunc: kf}),
		"together":      together.New(together.Config{APIKeyFunc: kf}),
		"voyage":        voyage.New(voyage.Config{APIKeyFunc: kf}),
		"xai":           xai.New(xai.Config{APIKeyFunc: kf}),
		"zai":           zai.New(zai.Config{APIKeyFunc: kf}),
		"bedrock":       bedrock.New(bedrock.Config{APIKeyFunc: kf, Region: "us-east-1"}),
		"mantle":        mantle.New(mantle.ProviderSettings{APIKeyFunc: kf, Region: "us-east-1"}),
	}
	ran := 0
	for name, p := range ps {
		// google validates image model IDs before the request.
		image := "i1"
		if name == "google" {
			image = ""
		}
		calls := r87dintegProviderCalls(name, p, "m1", "e1", image, "s1", "t1")
		if vp, ok := p.(interface {
			VideoModel(string) (provider.VideoModelV3, error)
		}); ok {
			calls = append(calls, r87dintegCall{name + " video", func(ctx context.Context) error {
				m, err := vp.VideoModel("v1")
				if err != nil {
					return fmt.Errorf("model: %w", err)
				}
				_, err = m.DoGenerate(ctx, &provider.VideoModelV3CallOptions{Prompt: "a cat"})
				return err
			}})
		}
		for _, c := range calls {
			if r87dintegCheck(t, c, r87dintegSame) {
				ran++
			}
		}
	}
	if ran < 60 {
		t.Fatalf("only %d calls ran", ran)
	}
}

// The core retry does not retry a credential error (TS retries only an
// APICallError with isRetryable): the token function runs once. v0.5 ran it
// three times (the retryable "Cannot connect to API" wrapper).
func TestR87dintegCredentialErrorNotRetried(t *testing.T) {
	var calls atomic.Int32
	az, err := azure.New(azure.Config{ResourceName: "r87d", ADTokenProvider: func(context.Context) (string, error) {
		calls.Add(1)
		return "", errR87dintegCred
	}})
	if err != nil {
		t.Fatal(err)
	}
	lm, err := az.LanguageModel("gpt-4o")
	if err != nil {
		t.Fatal(err)
	}
	_, err = ai.GenerateText(context.Background(), ai.GenerateTextOptions{Model: lm, Prompt: "hi"})
	if !errors.Is(err, errR87dintegCred) {
		t.Fatalf("err = %v", err)
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("token provider ran %d times, want 1", got)
	}
}
