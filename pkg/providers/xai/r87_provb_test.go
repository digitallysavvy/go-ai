package xai

import (
	"context"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS buildXaiStreamingTranscriptionUrl appends to url.searchParams, which
// re-parses the base URL's query leniently (an invalid escape stays literal,
// "+" is a space). Want printed by Node 24:
// new URL("https://x.example/v1?k=%zz&p=a+b%41/stt") + append.
func TestR87provbStreamingTranscriptionBaseQueryLenient(t *testing.T) {
	u, err := buildXAIStreamingTranscriptionURL("https://x.example/v1?k=%zz&p=a+b%41", "grok-stt", provider.AudioFormat{Type: "audio/pcm"}, XAITranscriptionProviderOptions{})
	if err != nil {
		t.Fatal(err)
	}
	want := "wss://x.example/v1?k=%25zz&p=a+bA%2Fstt&model=grok-stt&encoding=pcm"
	if got := u.String(); got != want {
		t.Errorf("URL =\n%s\nwant\n%s", got, want)
	}
}

// TS `new Error(raw.message ?? 'xAI STT error')`: an empty message is kept,
// a missing one uses the default.
func TestR87provbSTTErrorMessageNullish(t *testing.T) {
	for _, tc := range []struct {
		event map[string]interface{}
		want  string
	}{
		{map[string]interface{}{"type": "error", "message": ""}, ""},
		{map[string]interface{}{"type": "error"}, "xAI STT error"},
		{map[string]interface{}{"type": "error", "message": nil}, "xAI STT error"},
	} {
		server := newXAISTTTestServer(t)
		model := newTestXAITranscriptionModel(server.ts.URL)
		result, err := model.DoStream(context.Background(), &provider.TranscriptionStreamOptions{
			Audio:            newXAIChanAudioStream([]byte{1, 2, 3}),
			InputAudioFormat: provider.AudioFormat{Type: "audio/pcm", Rate: intPtr(16000)},
		})
		if err != nil {
			t.Fatal(err)
		}
		errCh := make(chan error, 1)
		go func() {
			_, err := drainXAIStream(result.Stream)
			errCh <- err
		}()
		server.send(map[string]interface{}{"type": "transcript.created"})
		server.waitForFrame(t, "audio.done", time.Second)
		server.send(tc.event)
		err = <-errCh
		_ = result.Stream.Close()
		server.close()
		if err == nil || err.Error() != tc.want {
			t.Errorf("%v: stream error = %v, want %q", tc.event, err, tc.want)
		}
	}
}

// TS xaiErrorDataSchema members require their strings (z.string()), so an
// empty message / error / code matches its member.
func TestR87provbErrorMessageEmptyStrings(t *testing.T) {
	for body, want := range map[string]string{
		`{"error":{"message":""}}`:  "",
		`{"code":"","error":""}`:    ": ",
		`{"code":"c","error":""}`:   "c: ",
		`{"error":""}`:              "",
		`{"error":{"message":"m"}}`: "m",
		`{"code":7,"error":"e"}`:    "e",
		`{"error":{"message":1}}`:   "Bad Request", // no member matches: status text
	} {
		if got := newXAIProviderError("xai", 400, nil, []byte(body)).Message; got != want {
			t.Errorf("%s: message = %q, want %q", body, got, want)
		}
	}
}
