package providers_test

import (
	"context"
	"io"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/providers/cartesia"
	"github.com/digitallysavvy/go-ai/pkg/providers/elevenlabs"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
	"github.com/digitallysavvy/go-ai/pkg/providers/xai"
)

type silentAudio struct{}

func (silentAudio) Next(context.Context) ([]byte, error) { return nil, io.EOF }
func (silentAudio) Cancel(error)                         {}

// TS loadApiKey runs for every realtime/streaming handshake: with no key the
// call fails with a LoadAPIKeyError before anything is dialed, and the env var
// is read at call time.
func TestRealtimeAndStreamingHandshakesRaiseLoadAPIKeyError(t *testing.T) {
	for _, env := range []string{"OPENAI_API_KEY", "XAI_API_KEY", "ELEVENLABS_API_KEY", "CARTESIA_API_KEY", "AZURE_API_KEY", "AZURE_OPENAI_API_KEY"} {
		t.Setenv(env, "")
	}
	ctx := context.Background()
	rate := 24000
	audio := provider.TranscriptionStreamOptions{Audio: silentAudio{}, InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: &rate}}
	oa := openai.New(openai.Config{BaseURL: "http://127.0.0.1:1"})
	xp := xai.New(xai.Config{BaseURL: "http://127.0.0.1:1"})
	cases := map[string]func() error{
		"openai realtime client secret": func() error {
			_, err := openai.NewRealtimeModel(oa, "gpt-realtime").DoCreateClientSecret(ctx, provider.ClientSecretOptions{})
			return err
		},
		"openai live websocket": func() error {
			_, err := openai.NewRealtimeModelLive(oa, "gpt-live").GetServerWebSocketConfig()
			return err
		},
		"openai transcription stream": func() error {
			_, err := openai.NewTranscriptionModel(oa, "gpt-realtime-whisper").DoStream(ctx, &audio)
			return err
		},
		"openai speech translation stream": func() error {
			_, err := openai.NewSpeechTranslationModel(oa, "gpt-realtime-translate").DoStream(ctx, &provider.SpeechTranslationStreamOptions{
				Audio: silentAudio{}, InputAudioFormat: audio.InputAudioFormat, TargetLanguage: "es"})
			return err
		},
		"xai realtime client secret": func() error {
			_, err := xai.NewRealtimeModel(xp, "grok-voice").DoCreateClientSecret(ctx, provider.ClientSecretOptions{})
			return err
		},
		"xai transcription stream": func() error {
			_, err := xai.NewTranscriptionModel(xp, "grok-stt").DoStream(ctx, &audio)
			return err
		},
		"elevenlabs transcription stream": func() error {
			p := elevenlabs.New(elevenlabs.Config{BaseURL: "http://127.0.0.1:1"})
			_, err := elevenlabs.NewTranscriptionModel(p, "scribe_v2_realtime").DoStream(ctx, &audio)
			return err
		},
		"cartesia realtime client secret": func() error {
			p := cartesia.New(cartesia.Config{BaseURL: "http://127.0.0.1:1"})
			_, err := cartesia.NewRealtimeModel(p, "ink-2").DoCreateClientSecret(ctx, provider.ClientSecretOptions{})
			return err
		},
		"cartesia transcription stream": func() error {
			p := cartesia.New(cartesia.Config{BaseURL: "http://127.0.0.1:1"})
			m, err := p.TranscriptionModel("ink-2")
			if err != nil {
				return err
			}
			_, err = m.(interface {
				DoStream(context.Context, *provider.TranscriptionStreamOptions) (*provider.TranscriptionStreamResult, error)
			}).DoStream(ctx, &audio)
			return err
		},
	}
	for name, call := range cases {
		t.Run(name, func(t *testing.T) {
			err := call()
			if !providererrors.IsLoadAPIKeyError(err) {
				t.Fatalf("error = %v, want LoadAPIKeyError", err)
			}
		})
	}
}
