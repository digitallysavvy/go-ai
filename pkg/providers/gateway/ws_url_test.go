package gateway

import "testing"

func TestGatewayWebSocketURLMatchesURLSearchParams(t *testing.T) {
	got := ToGatewaySpeechURL("https://gw.example/v3/ai", "openai/a~b*c d")
	want := "wss://gw.example/v3/ai/speech-model?ai-model-id=openai%2Fa%7Eb*c+d"
	if got != want {
		t.Fatalf("got %s want %s", got, want)
	}
	if got := ToGatewayTranscriptionURL("http://localhost:3000/x", "m"); got != "ws://localhost:3000/x/transcription-model?ai-model-id=m" {
		t.Fatal(got)
	}
	if got := ToGatewayRealtimeURL("https://h/http", "m~"); got != "wss://h/http/realtime-model?ai-model-id=m%7E" {
		t.Fatal(got)
	}
	// TS /^http/: an "http" that is not at the start is left alone.
	if got := ToGatewayRealtimeURL("wss://h/httpapi", "m"); got != "wss://h/httpapi/realtime-model?ai-model-id=m" {
		t.Fatal(got)
	}
}
