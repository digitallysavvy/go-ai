package azure

import (
	"compress/gzip"
	"context"
	"encoding/json"
	"io"
	stdhttp "net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// Ports of the "recorded MAI-Transcribe" tests in
// packages/azure/src/azure-transcription-model.test.ts over the TS fixtures.

func loadAzureFixture(t *testing.T, name string) []byte {
	t.Helper()
	f, err := os.Open("testdata/fixtures/" + name + ".json.gz")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close() //nolint:errcheck
	zr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	b, err := io.ReadAll(zr)
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func recordedMAITranscribe(t *testing.T, modelID, fixture string, providerOptions map[string]interface{}) (result *transcribeResultView, body []byte) {
	t.Helper()
	body = loadAzureFixture(t, fixture)
	server := httptest.NewServer(stdhttp.HandlerFunc(func(w stdhttp.ResponseWriter, r *stdhttp.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(body)
	}))
	t.Cleanup(server.Close)
	p, err := New(Config{APIKey: "speech-key", ResourceName: "r", SpeechBaseURL: server.URL})
	if err != nil {
		t.Fatal(err)
	}
	model, err := p.TranscriptionModel(modelID)
	if err != nil {
		t.Fatal(err)
	}
	res, err := model.DoTranscribe(context.Background(), &provider.TranscriptionOptions{
		Audio: []byte{1, 2, 3}, MimeType: "audio/wav", ProviderOptions: providerOptions,
	})
	if err != nil {
		t.Fatal(err)
	}
	view := &transcribeResultView{Text: res.Text, Language: res.Language}
	if res.DurationInSeconds != nil {
		view.Duration = *res.DurationInSeconds
	}
	for _, s := range res.Segments {
		view.Segments = append(view.Segments, [3]interface{}{s.Text, s.Start, s.End})
	}
	// Metadata holds typed values; compare through its JSON form.
	if raw, err := json.Marshal(res.ProviderMetadata); err == nil {
		_ = json.Unmarshal(raw, &view.Metadata)
	}
	return view, body
}

type transcribeResultView struct {
	Text     string
	Language string
	Duration float64
	Segments [][3]interface{}
	Metadata map[string]interface{}
}

func TestRecordedMAITranscribe2WordDiarization(t *testing.T) {
	view, body := recordedMAITranscribe(t, "mai-transcribe-2", "azure-speech-mai-transcribe-2-word-diarization.1",
		map[string]interface{}{"azure": map[string]interface{}{"timestamps": "word", "diarization": map[string]interface{}{"enabled": true}}})
	var raw struct {
		CombinedPhrases []struct {
			Text string `json:"text"`
		} `json:"combinedPhrases"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatal(err)
	}
	if view.Text != raw.CombinedPhrases[0].Text || view.Language != "en" || view.Duration != 17.579 {
		t.Errorf("text/language/duration = %q/%q/%v", view.Text, view.Language, view.Duration)
	}
	want := [][3]interface{}{
		{"Um, so, uh, did you finish the quarterly report for Versal yet?", 0.12, 4.32},
		{"Yeah, I, uh, I sent it over this morning. It covers the AI gateway numbers.", 4.48, 9.439},
		{"Great. Uh, can you, like, add the transcription pricing too?", 9.68, 13.8},
		{"Sure. I'll, um, update it by Friday.", 14.04, 17.159},
	}
	if len(view.Segments) != len(want) {
		t.Fatalf("segments = %d, want %d: %v", len(view.Segments), len(want), view.Segments)
	}
	for i, w := range want {
		if view.Segments[i] != w {
			t.Errorf("segment %d = %v, want %v", i, view.Segments[i], w)
		}
	}
	azure, _ := view.Metadata["azure"].(map[string]interface{})
	phrases, _ := azure["phrases"].([]interface{})
	if len(phrases) != 4 {
		t.Fatalf("phrases = %v", azure["phrases"])
	}
	var speakers []float64
	for _, p := range phrases {
		pm, _ := p.(map[string]interface{})
		speakers = append(speakers, toFloat(pm["speaker"]))
	}
	if speakers[0] != 0 || speakers[1] != 1 || speakers[2] != 0 || speakers[3] != 1 {
		t.Errorf("speakers = %v, want [0 1 0 1]", speakers)
	}
	first, _ := phrases[0].(map[string]interface{})
	words, _ := first["words"].([]interface{})
	w0, _ := words[0].(map[string]interface{})
	if w0["text"] != "Um," || toFloat(w0["offsetMilliseconds"]) != 120 || toFloat(w0["durationMilliseconds"]) != 220 {
		t.Errorf("first word = %v", w0)
	}
}

func toFloat(v interface{}) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	case int64:
		return float64(n)
	}
	return -1
}

func TestRecordedMAITranscribe2NoTimestampsIsOneSegmentSpanningTheAudio(t *testing.T) {
	view, _ := recordedMAITranscribe(t, "mai-transcribe-2", "azure-speech-mai-transcribe-2-no-timestamps.1",
		map[string]interface{}{"azure": map[string]interface{}{"timestamps": "none"}})
	if len(view.Segments) != 1 || view.Segments[0][1] != 0.0 || view.Segments[0][2] != 17.579 {
		t.Errorf("segments = %v, want one 0..17.579", view.Segments)
	}
}

func TestRecordedMAITranscribe15IsOneTimedSegment(t *testing.T) {
	view, body := recordedMAITranscribe(t, "mai-transcribe-1.5", "azure-speech-mai-transcribe-1-5.1", nil)
	var raw struct {
		CombinedPhrases []struct {
			Text string `json:"text"`
		} `json:"combinedPhrases"`
		Phrases []struct {
			Text string `json:"text"`
		} `json:"phrases"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		t.Fatal(err)
	}
	if view.Text != raw.CombinedPhrases[0].Text || view.Language != "en" || view.Duration != 36.71 {
		t.Errorf("text/language/duration = %q/%q/%v", view.Text, view.Language, view.Duration)
	}
	if len(view.Segments) != 1 || view.Segments[0][0] != raw.Phrases[0].Text || view.Segments[0][1] != 0.0 || view.Segments[0][2] != 36.712 {
		t.Errorf("segments = %v", view.Segments)
	}
}
