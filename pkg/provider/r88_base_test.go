package provider

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func TestRealtimeUsageJSON(t *testing.T) {
	intp := func(v int) *int { return &v }
	withZeroSeconds := RealtimeUsage{InputTokens: intp(3)}
	withZeroSeconds.SetSeconds(0)

	cases := []struct {
		name  string
		usage RealtimeUsage
		want  string
	}{
		{"zero value keeps v0.5.2 shape", RealtimeUsage{}, `{"seconds":0}`},
		{"seconds only (OpenAI Live)", RealtimeUsage{Seconds: 12}, `{"seconds":12}`},
		{"token-only omits seconds", RealtimeUsage{InputTokens: intp(10), OutputTokens: intp(4), CachedInputAudioTokens: intp(0)}, `{"inputTokens":10,"outputTokens":4,"cachedInputAudioTokens":0}`},
		{"tokens and non-zero seconds", RealtimeUsage{InputTokens: intp(1), Seconds: 2.5}, `{"inputTokens":1,"seconds":2.5}`},
		{"tokens and seconds marked present", withZeroSeconds, `{"inputTokens":3,"seconds":0}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := json.Marshal(tc.usage)
			if err != nil {
				t.Fatal(err)
			}
			if string(got) != tc.want {
				t.Fatalf("marshal = %s, want %s", got, tc.want)
			}
			// Pointer and embedded forms marshal the same way.
			ev, err := json.Marshal(RealtimeServerEvent{Type: "session-usage", Usage: &tc.usage})
			if err != nil {
				t.Fatal(err)
			}
			var back struct {
				Usage json.RawMessage `json:"usage"`
			}
			if err := json.Unmarshal(ev, &back); err != nil {
				t.Fatal(err)
			}
			if string(back.Usage) != tc.want {
				t.Fatalf("event usage = %s, want %s", back.Usage, tc.want)
			}
			// Round trip.
			var decoded RealtimeUsage
			if err := json.Unmarshal(got, &decoded); err != nil {
				t.Fatal(err)
			}
			again, err := json.Marshal(decoded)
			if err != nil {
				t.Fatal(err)
			}
			if string(again) != tc.want {
				t.Fatalf("round trip = %s, want %s", again, tc.want)
			}
		})
	}

	// A seconds-only payload decodes to the same value as a literal.
	var decoded RealtimeUsage
	if err := json.Unmarshal([]byte(`{"seconds":12}`), &decoded); err != nil {
		t.Fatal(err)
	}
	if decoded != (RealtimeUsage{Seconds: 12}) {
		t.Fatalf("decoded = %+v, want Seconds 12 literal", decoded)
	}
}

type r88RealtimeModel struct{}

func (r88RealtimeModel) GetServerConnection(context.Context, RealtimeServerConnectionOptions) (WebSocketConfig, error) {
	return WebSocketConfig{}, nil
}

func (r88RealtimeModel) BuildSessionConfigForIntent(RealtimeSessionConfig, RealtimeSessionIntent) (any, error) {
	return nil, nil
}

func (r88RealtimeModel) SerializeClientEventForIntent(RealtimeClientEvent, RealtimeSessionIntent) (json.RawMessage, error) {
	return nil, nil
}

type r88TranscriptionURLModel struct{}

func (r88TranscriptionURLModel) DoTranscribeFromURL(context.Context, *TranscriptionURLOptions) (*types.TranscriptionResult, error) {
	return nil, nil
}

var (
	_ RealtimeServerConnectionProvider = r88RealtimeModel{}
	_ RealtimeIntentCodec              = r88RealtimeModel{}
	_ TranscriptionURLModel            = r88TranscriptionURLModel{}
)

func TestR88BaseDeclarations(t *testing.T) {
	if (RealtimeServerConnectionOptions{}).Intent != "" ||
		RealtimeSessionIntentConversation != "conversation" ||
		RealtimeSessionIntentTranscription != "transcription" {
		t.Fatal("realtime intent values do not match TS")
	}
	if ChunkTypePing != "ping" || ChunkTypeSteeringMessage != "steering-message" {
		t.Fatal("chunk type values do not match TS")
	}
	if VideoDataTypeUploaded != "uploaded" {
		t.Fatal("uploaded video type does not match TS")
	}
	if TranscriptionTimestampGranularityWord != "word" || TranscriptionTimestampGranularitySegment != "segment" {
		t.Fatal("timestamp granularities do not match TS")
	}
	if (TranscriptionOptions{}).TimestampGranularities != nil {
		t.Fatal("zero TimestampGranularities must be nil (unset)")
	}

	// Words are omitted when unavailable and use TS field names.
	b, _ := json.Marshal(types.TranscriptionResult{Text: "hi"})
	var m map[string]any
	_ = json.Unmarshal(b, &m)
	if _, ok := m["words"]; ok {
		t.Fatalf("words should be omitted: %s", b)
	}
	b, _ = json.Marshal(types.TranscriptionWord{Text: "hi", StartSecond: 0.5, EndSecond: 1})
	if string(b) != `{"text":"hi","startSecond":0.5,"endSecond":1}` {
		t.Fatalf("word JSON = %s", b)
	}

	step := 2
	_ = StreamChunk{Type: ChunkTypeSteeringMessage, ID: "s1", SteeringMessage: &types.Message{Role: types.RoleUser}, StepNumber: &step}
}
