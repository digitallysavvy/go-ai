package google

import (
	"errors"
	"io"
	"strings"
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// TS parseJsonLines (provider-utils response-handler.ts) skips a line only
// when line.trim() is empty and JSON.parses the line as read: a line holding
// only U+FEFF and a space is skipped, U+0085 alone and U+00A0 before the JSON
// value are JSONParseErrors (Node: JSON.parse("\u00a0{}") throws).
func TestR86bprovaGoogleResultsJSLineTrim(t *testing.T) {
	valid := `{"key":"cancelled-request","error":{"code":1,"message":"The request was cancelled."}}`
	cases := []struct {
		name  string
		body  string
		items int
		fail  bool
	}{
		{"bom-only line skipped", valid + "\n\ufeff \n", 1, false},
		{"nbsp before value", "\u00a0" + valid + "\n", 0, true},
		{"nel-only line", valid + "\n\u0085\n", 1, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			stream := newGoogleBatchFileResultsStream(&Batch{provider: New(Config{APIKey: "k"})}, io.NopCloser(strings.NewReader(tc.body)), "https://x.example/r", 1<<20)
			items := 0
			for {
				item, err := stream.Next()
				if err == io.EOF {
					break
				}
				if err != nil {
					var jp *providererrors.JSONParseError
					if !tc.fail || !errors.As(err, &jp) {
						t.Fatalf("err = %T %v", err, err)
					}
					tc.fail = false
					break
				}
				if item == nil {
					t.Fatal("nil item")
				}
				items++
			}
			if items != tc.items || tc.fail {
				t.Fatalf("items = %d (want %d), missing JSONParseError = %v", items, tc.items, tc.fail)
			}
		})
	}
}

// TS Number.parseFloat skips leading JS whitespace (U+FEFF, U+00A0, \v), not
// U+0085. Node: 1.5, 2, 5, NaN.
func TestR86bprovaParseOffsetSecondsJSWhitespace(t *testing.T) {
	for in, want := range map[string]float64{"\ufeff1.5s": 1.5, "\u00a02s": 2, "\v.5e1s": 5, "\u00851s": -1} {
		got, ok := parseOffsetSeconds(in)
		if want < 0 {
			if ok {
				t.Fatalf("%q: got %v, want NaN", in, got)
			}
			continue
		}
		if !ok || got != want {
			t.Fatalf("%q: got %v %v, want %v", in, got, ok, want)
		}
	}
}

// TS getRealtimeWebSocketURL / the auth token URL strip one trailing "/"
// (pathname.replace(/\/$/, ”)). Node on literal copies of the TS functions:
// wss://x.example/a//ws/P?key=k and https://x.example/a//v1alpha/auth_tokens.
func TestR86bprovaRealtimeURLsOneTrailingSlash(t *testing.T) {
	const base = "https://x.example/a//"
	for got, want := range map[string]string{
		googleWebSocketURL(base):                        "wss://x.example/a//ws/" + realtimeWebSocketPath,
		googleAuthTokenURL(base, "k"):                   "https://x.example/a//v1alpha/auth_tokens?key=k",
		getLiveTranscriptionWebSocketURL(base, "k"):     "wss://x.example/a//ws/" + liveTranscriptionWebSocketPath + "?key=k",
		getLiveSpeechTranslationWebSocketURL(base, "k"): "wss://x.example/a//ws/" + liveSpeechTranslationWebSocketPath + "?key=k",
	} {
		if got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	}
}

// TS query escaping of the Live API credentials: the auth token URL and the
// access_token use encodeURIComponent, the transcription and translation key
// url.searchParams.set (URLSearchParams). Node: a~b*c%20d, t~*%2F%20,
// key=a%7Eb*c+d.
func TestR86bprovaRealtimeCredentialQueryEscaping(t *testing.T) {
	const base = "https://x.example/v1beta"
	for got, want := range map[string]string{
		googleAuthTokenURL(base, "a~b*c d"):                                   "https://x.example/v1alpha/auth_tokens?key=a~b*c%20d",
		(&GoogleRealtimeModel{}).GetWebSocketConfig("t~*/ ", "wss://h/p").URL: "wss://h/p?access_token=t~*%2F%20",
		getLiveTranscriptionWebSocketURL(base, "a~b*c d"):                     "wss://x.example/ws/" + liveTranscriptionWebSocketPath + "?key=a%7Eb*c+d",
		getLiveSpeechTranslationWebSocketURL(base, "a~b*c d"):                 "wss://x.example/ws/" + liveSpeechTranslationWebSocketPath + "?key=a%7Eb*c+d",
	} {
		if got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	}
}
