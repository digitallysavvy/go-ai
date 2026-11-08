package cartesia

import "testing"

// cr-r86b-prova-to-integrator 4 (query escaping): TS builds the STT URLs with
// url.searchParams.set, i.e. URLSearchParams form escaping ("~" as %7E, "*"
// literal, space "+") in insertion order. Node 24:
// u.searchParams.set('language','a~b*c d') -> language=a%7Eb*c+d;
// new URL('wss://h/stt/websocket?model=m&access_token=old').searchParams
// .set('access_token','t~1') -> ?model=m&access_token=t%7E1.
func TestR86bintegCartesiaQueryEscaping(t *testing.T) {
	lang := "a~b*c d"
	u, err := buildCartesiaStreamingTranscriptionURL("https://api.cartesia.ai", "2025-04-16", "ink", "pcm_s16le", 24000, &lang, "t~1", false, true, nil)
	if err != nil {
		t.Fatal(err)
	}
	if want := "wss://api.cartesia.ai/stt/websocket?model=ink&encoding=pcm_s16le&sample_rate=24000&cartesia_version=2025-04-16&access_token=t%7E1&language=a%7Eb*c+d"; u.String() != want {
		t.Errorf("transcription URL = %s, want %s", u, want)
	}
	m := &RealtimeModel{}
	got := m.GetWebSocketConfig("t~1", "wss://h/stt/websocket?model=m&access_token=old").URL
	if want := "wss://h/stt/websocket?model=m&access_token=t%7E1"; got != want {
		t.Errorf("GetWebSocketConfig URL = %s, want %s", got, want)
	}
}
