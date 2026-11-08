package ai

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"reflect"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/internal/fileutil"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// r88provcURLTranscriber is a transcription model with native URL support.
type r88provcURLTranscriber struct {
	testutil.MockTranscriptionModel
	mu       sync.Mutex
	urlCalls []*provider.TranscriptionURLOptions
	fileOpts []*provider.TranscriptionOptions
	fromURL  func(call int) (*types.TranscriptionResult, error)
	file     func(call int) (*types.TranscriptionResult, error)
}

func (m *r88provcURLTranscriber) DoTranscribeFromURL(_ context.Context, opts *provider.TranscriptionURLOptions) (*types.TranscriptionResult, error) {
	m.mu.Lock()
	m.urlCalls = append(m.urlCalls, opts)
	n := len(m.urlCalls)
	m.mu.Unlock()
	return m.fromURL(n)
}

func (m *r88provcURLTranscriber) DoTranscribe(_ context.Context, opts *provider.TranscriptionOptions) (*types.TranscriptionResult, error) {
	m.mu.Lock()
	m.fileOpts = append(m.fileOpts, opts)
	n := len(m.fileOpts)
	m.mu.Unlock()
	if m.file == nil {
		return r88provcTranscript(), nil
	}
	return m.file(n)
}

func r88provcTranscript() *types.TranscriptionResult {
	return &types.TranscriptionResult{Text: "hello", Response: &types.ResponseMetadata{ModelID: "m"}}
}

// r88provcAudioEvents records the transcription start / end audio fields.
type r88provcAudioEvents struct {
	telemetry.NoopTelemetryIntegration
	mu     sync.Mutex
	events []string
}

func (r *r88provcAudioEvents) add(name string, n *int64, mediaType string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	size := "nil"
	if n != nil {
		size = strings.Repeat("#", int(*n))
	}
	r.events = append(r.events, name+":"+size+":"+mediaType)
}

func (r *r88provcAudioEvents) OnStart(ctx context.Context, e telemetry.TelemetryStartEvent) context.Context {
	r.add("start", e.AudioByteLength, e.AudioMediaType)
	return ctx
}

func (r *r88provcAudioEvents) OnEnd(_ context.Context, e telemetry.TelemetryFinishEvent) {
	r.add("end", e.AudioByteLength, e.AudioMediaType)
}

func (r *r88provcAudioEvents) OnError(_ context.Context, e telemetry.TelemetryErrorEvent) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, "error:"+e.Error.Error())
}

func (r *r88provcAudioEvents) get() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.events...)
}

func r88provcTelemetry(rec *r88provcAudioEvents) *TelemetrySettings {
	return &TelemetrySettings{IsEnabled: telemetry.Bool(true), Integrations: []telemetry.TelemetryIntegration{rec}}
}

// TS transcribe.test.ts (5d18351e1e): a generic downloaded Content-Type is
// ignored and the bytes are sniffed; an audio type is normalized.
func TestR88provcTranscribeDownloadedAudioMediaType(t *testing.T) {
	aac := []byte{0xff, 0xf1, 0x50, 0x40}
	for _, tc := range []struct {
		downloaded string
		data       []byte
		want       string
	}{
		{"application/octet-stream", aac, "audio/aac"},
		{"application/octet-stream; charset=binary", aac, "audio/aac"},
		{"Application/Octet-Stream", aac, "audio/aac"},
		{"binary/octet-stream", aac, "audio/aac"},
		{"text/plain", aac, "audio/aac"},
		{"audio/*", aac, "audio/aac"},
		{"audio", aac, "audio/aac"},
		{"Audio/MPEG; charset=binary", []byte("not-a-signature"), "audio/mpeg"},
		{" audio/ogg ", []byte("x"), "audio/ogg"},
		{"application/octet-stream", []byte("x"), "audio/wav"},
	} {
		var got string
		model := &testutil.MockTranscriptionModel{DoTranscribeFunc: func(_ context.Context, opts *provider.TranscriptionOptions) (*types.TranscriptionResult, error) {
			got = opts.MimeType
			return r88provcTranscript(), nil
		}}
		data := tc.data
		_, err := Transcribe(context.Background(), TranscribeOptions{
			Model: model, AudioURL: "https://example.com/audio",
			DownloadWithMetadata: func(context.Context, string) (*DownloadResult, error) {
				return &DownloadResult{Data: data, MediaType: tc.downloaded}, nil
			},
		})
		if err != nil || got != tc.want {
			t.Fatalf("%q: media type = %q (%v), want %q", tc.downloaded, got, err, tc.want)
		}
	}
}

// TS transcribe.test.ts (41465fd1b6): words reach the result, and the shared
// timestamp granularities are forwarded only when set (an empty slice too).
func TestR88provcTranscribeWordsAndGranularities(t *testing.T) {
	for _, granularities := range [][]string{nil, {}, {"word", "segment"}} {
		var got *provider.TranscriptionOptions
		model := &testutil.MockTranscriptionModel{DoTranscribeFunc: func(_ context.Context, opts *provider.TranscriptionOptions) (*types.TranscriptionResult, error) {
			got = opts
			r := r88provcTranscript()
			r.Words = []types.TranscriptionWord{{Text: "hello", StartSecond: 0, EndSecond: 0.5}}
			return r, nil
		}}
		res, err := Transcribe(context.Background(), TranscribeOptions{Model: model, Audio: []byte("x"), TimestampGranularities: granularities})
		if err != nil {
			t.Fatal(err)
		}
		if (granularities == nil) != (got.TimestampGranularities == nil) || !reflect.DeepEqual(got.TimestampGranularities, granularities) {
			t.Fatalf("%v: forwarded %#v", granularities, got.TimestampGranularities)
		}
		if !reflect.DeepEqual(res.Words, []TranscriptionWord{{Text: "hello", StartSecond: 0, EndSecond: 0.5}}) {
			t.Fatalf("words = %#v", res.Words)
		}
	}
	model := &testutil.MockTranscriptionModel{DoTranscribeFunc: func(context.Context, *provider.TranscriptionOptions) (*types.TranscriptionResult, error) {
		return r88provcTranscript(), nil
	}}
	res, err := Transcribe(context.Background(), TranscribeOptions{Model: model, Audio: []byte("x")})
	if err != nil || res.Words != nil {
		t.Fatalf("words = %#v, err = %v", res.Words, err)
	}
}

// TS transcribe.test.ts (36802d48af): native URL transcription.
func TestR88provcTranscribeNativeURL(t *testing.T) {
	t.Run("http(s) URLs go to capable models without a download", func(t *testing.T) {
		for _, scheme := range []string{"http", "https"} {
			rec := &r88provcAudioEvents{}
			model := &r88provcURLTranscriber{fromURL: func(int) (*types.TranscriptionResult, error) { return r88provcTranscript(), nil }}
			zero := 0
			res, err := Transcribe(context.Background(), TranscribeOptions{
				Model: model, AudioURL: scheme + "://example.com/large-audio.mp3#frag", MaxRetries: &zero,
				Headers:                map[string]string{"custom-header": "value"},
				ProviderOptions:        map[string]interface{}{"groq": map[string]interface{}{"language": "en"}},
				TimestampGranularities: []string{"word", "segment"},
				MimeType:               "audio/mpeg",
				Telemetry:              r88provcTelemetry(rec),
			})
			if err != nil || res.Text != "hello" {
				t.Fatal(err)
			}
			if len(model.fileOpts) != 0 || len(model.urlCalls) != 1 {
				t.Fatalf("file calls %d, url calls %d", len(model.fileOpts), len(model.urlCalls))
			}
			call := model.urlCalls[0]
			if call.AudioURL != scheme+"://example.com/large-audio.mp3#frag" || call.Headers["custom-header"] != "value" ||
				!reflect.DeepEqual(call.ProviderOptions, map[string]interface{}{"groq": map[string]interface{}{"language": "en"}}) ||
				!reflect.DeepEqual(call.TimestampGranularities, []string{"word", "segment"}) || !strings.Contains(call.Headers["User-Agent"]+call.Headers["user-agent"], "ai/") {
				t.Fatalf("url call = %+v", call)
			}
			if got := rec.get(); !reflect.DeepEqual(got, []string{"start:nil:", "end:nil:"}) {
				t.Fatalf("events = %v", got)
			}
		}
	})

	t.Run("native calls are retried without switching to a file upload", func(t *testing.T) {
		model := &r88provcURLTranscriber{fromURL: func(n int) (*types.TranscriptionResult, error) {
			if n == 1 {
				return nil, providererrors.NewProviderError("mock", 429, "", "Retry this request", nil)
			}
			return r88provcTranscript(), nil
		}}
		one := 1
		res, err := Transcribe(context.Background(), TranscribeOptions{Model: model, AudioURL: "https://example.com/audio.mp3", MaxRetries: &one})
		if err != nil || res.Text != "hello" || len(model.urlCalls) != 2 || len(model.fileOpts) != 0 {
			t.Fatalf("err = %v, url %d, file %d", err, len(model.urlCalls), len(model.fileOpts))
		}
	})

	t.Run("a declined URL is downloaded once and only the file call is retried", func(t *testing.T) {
		restoreValidator, restoreTransport := downloadURLValidator, downloadTransport
		t.Cleanup(func() { downloadURLValidator, downloadTransport = restoreValidator, restoreTransport })
		var fetches int
		var mu sync.Mutex
		downloadURLValidator = func(string) error { return nil }
		downloadTransport = func() http.RoundTripper {
			return r88provcRoundTrip(func(r *http.Request) (*http.Response, error) {
				mu.Lock()
				fetches++
				mu.Unlock()
				return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": {"Audio/MPEG; charset=binary"}}, Body: io.NopCloser(strings.NewReader("abc")), Request: r}, nil
			})
		}
		rec := &r88provcAudioEvents{}
		model := &r88provcURLTranscriber{
			fromURL: func(int) (*types.TranscriptionResult, error) { return nil, nil },
			file: func(n int) (*types.TranscriptionResult, error) {
				if n == 1 {
					return nil, providererrors.NewProviderError("mock", 429, "", "Retry file", nil)
				}
				return r88provcTranscript(), nil
			},
		}
		one := 1
		res, err := Transcribe(context.Background(), TranscribeOptions{
			Model: model, AudioURL: "https://example.com/redirect", MaxRetries: &one,
			TimestampGranularities: []string{}, Telemetry: r88provcTelemetry(rec),
		})
		if err != nil || res.Text != "hello" {
			t.Fatal(err)
		}
		if len(model.urlCalls) != 1 || model.urlCalls[0].TimestampGranularities == nil || fetches != 1 || len(model.fileOpts) != 2 {
			t.Fatalf("url %d, fetches %d, file %d", len(model.urlCalls), fetches, len(model.fileOpts))
		}
		last := model.fileOpts[1]
		if string(last.Audio) != "abc" || last.MimeType != "audio/mpeg" || last.TimestampGranularities == nil || len(last.TimestampGranularities) != 0 {
			t.Fatalf("file call = %+v", last)
		}
		if got := rec.get(); !reflect.DeepEqual(got, []string{"start:nil:", "end:###:audio/mpeg"}) {
			t.Fatalf("events = %v", got)
		}
	})

	t.Run("a declined URL still goes through the download URL validation", func(t *testing.T) {
		model := &r88provcURLTranscriber{fromURL: func(int) (*types.TranscriptionResult, error) { return nil, nil }}
		zero := 0
		_, err := Transcribe(context.Background(), TranscribeOptions{Model: model, AudioURL: "http://127.0.0.1/audio", MaxRetries: &zero})
		if err == nil || !strings.Contains(err.Error(), "loopback") || len(model.fileOpts) != 0 {
			t.Fatalf("err = %v", err)
		}
	})

	t.Run("a native failure is a correlated error event", func(t *testing.T) {
		rec := &r88provcAudioEvents{}
		failure := errors.New("Provider could not fetch audio")
		model := &r88provcURLTranscriber{fromURL: func(int) (*types.TranscriptionResult, error) { return nil, failure }}
		zero := 0
		_, err := Transcribe(context.Background(), TranscribeOptions{Model: model, AudioURL: "https://example.com/audio.mp3", MaxRetries: &zero, Telemetry: r88provcTelemetry(rec)})
		if err != failure {
			t.Fatalf("err = %v", err)
		}
		if got := rec.get(); !reflect.DeepEqual(got, []string{"start:nil:", "error:Provider could not fetch audio"}) {
			t.Fatalf("events = %v", got)
		}
	})

	t.Run("a custom download, bytes or a non-http URL skip the native path", func(t *testing.T) {
		model := &r88provcURLTranscriber{fromURL: func(int) (*types.TranscriptionResult, error) { t.Fatal("native call"); return nil, nil }}
		if _, err := Transcribe(context.Background(), TranscribeOptions{Model: model, AudioURL: "https://example.com/a.mp3",
			Download: func(context.Context, string) ([]byte, error) { return []byte("abc"), nil }}); err != nil {
			t.Fatal(err)
		}
		if _, err := Transcribe(context.Background(), TranscribeOptions{Model: model, AudioURL: "https://example.com/a.mp3",
			DownloadWithMetadata: func(context.Context, string) (*DownloadResult, error) {
				return &DownloadResult{Data: []byte("abc"), MediaType: "audio/mpeg"}, nil
			}}); err != nil {
			t.Fatal(err)
		}
		if _, err := Transcribe(context.Background(), TranscribeOptions{Model: model, Audio: []byte("abc"), AudioURL: "https://example.com/a.mp3"}); err != nil {
			t.Fatal(err)
		}
		if _, err := Transcribe(context.Background(), TranscribeOptions{Model: model, AudioURL: "data:audio/mpeg;base64,YWJj"}); err != nil {
			t.Fatal(err)
		}
		if len(model.fileOpts) != 4 || model.fileOpts[1].MimeType != "audio/mpeg" {
			t.Fatalf("file calls = %d", len(model.fileOpts))
		}
	})
}

type r88provcRoundTrip func(*http.Request) (*http.Response, error)

func (f r88provcRoundTrip) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

var _ = fileutil.IsFullMediaType

// TS generate-video.test.ts (2e44144812): uploaded video outcomes.
func TestR88provcGenerateVideoUploadedOutcome(t *testing.T) {
	t.Run("upload confirmations are counted, not downloaded", func(t *testing.T) {
		downloads := 0
		model := &mockVideoModel{generateFn: func(context.Context, *provider.VideoModelV3CallOptions) (*provider.VideoModelV3Response, error) {
			return &provider.VideoModelV3Response{
				Videos:           []provider.VideoModelV3VideoData{{Type: provider.VideoDataTypeUploaded, MediaType: "video/mp4"}},
				Warnings:         []types.Warning{{Type: "other", Message: "upload warning"}},
				ProviderMetadata: map[string]interface{}{"testProvider": map[string]interface{}{"requestId": "uploaded-1"}},
				Response:         provider.VideoModelV3ResponseInfo{ModelID: "mock-video"},
			}, nil
		}}
		res, err := GenerateVideo(context.Background(), GenerateVideoOptions{Model: model, Prompt: VideoPrompt{Text: "a cat"},
			Download: func(context.Context, string) ([]byte, error) { downloads++; return nil, nil }})
		if err != nil {
			t.Fatal(err)
		}
		if res.Video != nil || len(res.Videos) != 0 || res.Videos == nil || res.UploadedVideos != 1 || downloads != 0 || len(res.Responses) != 1 {
			t.Fatalf("result = %+v", res)
		}
		if !reflect.DeepEqual(res.ProviderMetadata, map[string]interface{}{"testProvider": map[string]interface{}{"requestId": "uploaded-1"}}) {
			t.Fatalf("metadata = %v", res.ProviderMetadata)
		}
		if len(res.Warnings) != 1 || res.Warnings[0].Message != "upload warning" {
			t.Fatalf("warnings = %v", res.Warnings)
		}
		b, _ := json.Marshal(res)
		if !strings.Contains(string(b), `"video":null,"videos":[],"uploadedVideos":1`) {
			t.Fatalf("json = %s", b)
		}
	})

	t.Run("uploads alongside downloadable videos across calls", func(t *testing.T) {
		one := 1
		call := 0
		var mu sync.Mutex
		model := &mockVideoModel{maxPerCall: &one, generateFn: func(context.Context, *provider.VideoModelV3CallOptions) (*provider.VideoModelV3Response, error) {
			mu.Lock()
			call++
			n := call
			mu.Unlock()
			video := provider.VideoModelV3VideoData{Type: "binary", Binary: []byte("v"), MediaType: "video/mp4"}
			if n == 1 {
				video = provider.VideoModelV3VideoData{Type: provider.VideoDataTypeUploaded, MediaType: "video/mp4"}
			}
			return &provider.VideoModelV3Response{Videos: []provider.VideoModelV3VideoData{video}, Response: provider.VideoModelV3ResponseInfo{ModelID: "mock-video"}}, nil
		}}
		res, err := GenerateVideo(context.Background(), GenerateVideoOptions{Model: model, Prompt: VideoPrompt{Text: "a cat"}, N: 2})
		if err != nil {
			t.Fatal(err)
		}
		if res.UploadedVideos != 1 || len(res.Videos) != 1 || res.Video != res.Videos[0] || len(res.Responses) != 2 {
			t.Fatalf("result = %+v", res)
		}
	})

	t.Run("upload-only results across parallel calls", func(t *testing.T) {
		one := 1
		model := &mockVideoModel{maxPerCall: &one, generateFn: func(context.Context, *provider.VideoModelV3CallOptions) (*provider.VideoModelV3Response, error) {
			return &provider.VideoModelV3Response{Videos: []provider.VideoModelV3VideoData{{Type: provider.VideoDataTypeUploaded}}, Response: provider.VideoModelV3ResponseInfo{ModelID: "mock-video"}}, nil
		}}
		res, err := GenerateVideo(context.Background(), GenerateVideoOptions{Model: model, Prompt: VideoPrompt{Text: "a cat"}, N: 2})
		if err != nil || res.UploadedVideos != 2 || res.Video != nil || len(res.Videos) != 0 {
			t.Fatalf("result = %+v, err = %v", res, err)
		}
	})

	t.Run("uploaded outcomes through the start/status flow", func(t *testing.T) {
		zero := 0
		model := &mockAsyncVideoModel{
			startFn: func(context.Context, *provider.VideoModelV3StartOptions) (*provider.VideoModelV3OperationStartResult, error) {
				return &provider.VideoModelV3OperationStartResult{Operation: json.RawMessage(`{"requestId":"uploaded-1"}`), Response: defaultVideoResponseInfo()}, nil
			},
			statusFn: func(context.Context, *provider.VideoModelV3StatusOptions) (*provider.VideoModelV3OperationStatusResult, error) {
				return &provider.VideoModelV3OperationStatusResult{
					Status:   provider.VideoOperationStatusCompleted,
					Videos:   []provider.VideoModelV3VideoData{{Type: provider.VideoDataTypeUploaded, MediaType: "video/mp4"}},
					Response: defaultVideoResponseInfo(),
				}, nil
			},
		}
		res, err := GenerateVideo(context.Background(), GenerateVideoOptions{Model: model, Prompt: VideoPrompt{Text: "a cat"}, Poll: &VideoPollOptions{IntervalMs: &zero}})
		if err != nil {
			t.Fatal(err)
		}
		if res.UploadedVideos != 1 || len(res.Videos) != 0 || res.Video != nil {
			t.Fatalf("result = %+v", res)
		}
	})

	t.Run("no videos at all is still NoVideoGeneratedError", func(t *testing.T) {
		model := &mockVideoModel{generateFn: func(context.Context, *provider.VideoModelV3CallOptions) (*provider.VideoModelV3Response, error) {
			return &provider.VideoModelV3Response{Response: provider.VideoModelV3ResponseInfo{ModelID: "mock-video"}}, nil
		}}
		_, err := GenerateVideo(context.Background(), GenerateVideoOptions{Model: model, Prompt: VideoPrompt{Text: "a cat"}})
		if !IsNoVideoGeneratedError(err) {
			t.Fatalf("err = %v", err)
		}
	})
}

// r88provcIntentModel is a realtime model with the server connection and
// intent codec capabilities (ruling 1, TS 892af4dab7).
type r88provcIntentModel struct {
	mockRealtimeModel
	gotIntent RealtimeSessionIntent
}

func (m *r88provcIntentModel) GetServerConnection(_ context.Context, opts provider.RealtimeServerConnectionOptions) (provider.WebSocketConfig, error) {
	m.gotIntent = opts.Intent
	if opts.Intent == RealtimeSessionIntentTranscription {
		return provider.WebSocketConfig{URL: "wss://server.test/realtime?intent=transcription", Headers: map[string]string{"Authorization": "Bearer long-lived"}}, nil
	}
	return provider.WebSocketConfig{URL: "wss://server.test/realtime?model=m"}, nil
}

func (m *r88provcIntentModel) BuildSessionConfigForIntent(cfg provider.RealtimeSessionConfig, intent RealtimeSessionIntent) (any, error) {
	return map[string]any{"intent": string(intent)}, nil
}

func (m *r88provcIntentModel) SerializeClientEventForIntent(event provider.RealtimeClientEvent, intent RealtimeSessionIntent) (json.RawMessage, error) {
	return json.Marshal(map[string]interface{}{"type": event.Type, "intent": string(intent)})
}

func TestR88provcConnectRealtimeIntent(t *testing.T) {
	if RealtimeSessionIntentConversation != "conversation" || RealtimeSessionIntentTranscription != "transcription" {
		t.Fatal("intent values")
	}
	instructions := "hi"
	conn := &mockRealtimeConn{}
	dialer := &mockRealtimeDialer{conn: conn}
	model := &r88provcIntentModel{}
	session, err := ConnectRealtime(context.Background(), model, RealtimeSessionOptions{
		Dialer: dialer, Intent: RealtimeSessionIntentTranscription,
		ClientSecret:  &provider.ClientSecretResult{Token: "ignored", URL: "ws://ignored"},
		SessionConfig: &provider.RealtimeSessionConfig{Instructions: &instructions},
	})
	if err != nil {
		t.Fatal(err)
	}
	defer session.Close() //nolint:errcheck
	if model.gotIntent != RealtimeSessionIntentTranscription || dialer.config.URL != "wss://server.test/realtime?intent=transcription" || dialer.config.Headers["Authorization"] != "Bearer long-lived" {
		t.Fatalf("dial config = %+v", dialer.config)
	}
	if err := session.Send(context.Background(), provider.RealtimeClientEvent{Type: "input-audio-append", Audio: "abc"}); err != nil {
		t.Fatal(err)
	}
	conn.mu.Lock()
	sent := append([][]byte(nil), conn.sent...)
	conn.mu.Unlock()
	if len(sent) != 2 || string(sent[0]) != `{"intent":"transcription","type":"session-update"}` || string(sent[1]) != `{"intent":"transcription","type":"input-audio-append"}` {
		t.Fatalf("sent = %q", sent)
	}

	// Without an intent the existing client-secret flow is unchanged.
	conn2 := &mockRealtimeConn{}
	dialer2 := &mockRealtimeDialer{conn: conn2}
	session2, err := ConnectRealtime(context.Background(), &r88provcIntentModel{}, RealtimeSessionOptions{Dialer: dialer2})
	if err != nil {
		t.Fatal(err)
	}
	defer session2.Close() //nolint:errcheck
	if dialer2.config.URL != "ws://example.test" {
		t.Fatalf("dial config = %+v", dialer2.config)
	}
	if err := session2.Send(context.Background(), provider.RealtimeClientEvent{Type: "input-audio-append", Audio: "abc"}); err != nil {
		t.Fatal(err)
	}
	if got := string(conn2.sent[0]); got != `{"audio":"abc","type":"input-audio-append"}` {
		t.Fatalf("sent = %s", got)
	}

	// A model without the capability cannot take an intent.
	_, err = ConnectRealtime(context.Background(), mockRealtimeModel{}, RealtimeSessionOptions{Dialer: &mockRealtimeDialer{conn: &mockRealtimeConn{}}, Intent: RealtimeSessionIntentConversation})
	if err == nil || !strings.Contains(err.Error(), "RealtimeServerConnectionProvider") {
		t.Fatalf("err = %v", err)
	}
}
