package deepgram

import (
	"testing"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TestDeepgramRedactArraySerialization guards against a Go fmt.Sprint
// formatting of a []interface{} ("[a b]") leaking into the redact query
// param; TS's `String(value)` on a JS array joins elements with commas, no
// brackets or spaces (deepgram-transcription-model.ts:88-94).
func TestDeepgramRedactArraySerialization(t *testing.T) {
	m := NewTranscriptionModel(New(Config{APIKey: "k"}), "nova-2")
	query := m.buildQueryParams(&provider.TranscriptionOptions{
		ProviderOptions: map[string]interface{}{
			"deepgram": map[string]interface{}{
				"redact": []interface{}{"pci", "ssn"},
			},
		},
	})
	if query["redact"] != "pci,ssn" {
		t.Fatalf("redact = %q, want %q", query["redact"], "pci,ssn")
	}
}

func TestDeepgramTranscriptionModelMetadata(t *testing.T) {
	p := New(Config{APIKey: "k"})
	m := NewTranscriptionModel(p, "nova-2")
	if m.SpecificationVersion() != "v4" || m.Provider() != "deepgram" || m.ModelID() != "nova-2" {
		t.Fatalf("metadata mismatch: spec=%s provider=%s model=%s", m.SpecificationVersion(), m.Provider(), m.ModelID())
	}
}

func TestDeepgramConvertResponseHandlesEmptyAndWords(t *testing.T) {
	m := NewTranscriptionModel(New(Config{APIKey: "k"}), "nova-2")

	empty := m.convertResponse(deepgramTranscriptionResponse{
		Metadata: &struct {
			TransactionKey string  `json:"transaction_key"`
			RequestID      string  `json:"request_id"`
			Duration       float64 `json:"duration"`
		}{Duration: 3.2},
	}, &internalhttp.Response{})
	if empty.Text != "" || empty.Usage.DurationSeconds != 3.2 || len(empty.Timestamps) != 0 {
		t.Fatalf("empty response mismatch: %#v", empty)
	}
	// Ported from TS deepgram-transcription-model.test.ts "should return
	// audio duration as usage in seconds" (TS 8c659885c5 / #21427).
	if got, want := empty.ProviderUsage["seconds"], 3.2; got != want {
		t.Fatalf("empty.ProviderUsage[seconds] = %v, want %v", got, want)
	}

	var resp deepgramTranscriptionResponse
	resp.Metadata = &struct {
		TransactionKey string  `json:"transaction_key"`
		RequestID      string  `json:"request_id"`
		Duration       float64 `json:"duration"`
	}{Duration: 5.5}
	resp.Results.Channels = []struct {
		DetectedLanguage string `json:"detected_language"`
		Alternatives     []struct {
			Transcript string  `json:"transcript"`
			Confidence float64 `json:"confidence"`
			Words      []struct {
				Word       string  `json:"word"`
				Start      float64 `json:"start"`
				End        float64 `json:"end"`
				Confidence float64 `json:"confidence"`
			} `json:"words"`
		} `json:"alternatives"`
	}{
		{
			DetectedLanguage: "en",
			Alternatives: []struct {
				Transcript string  `json:"transcript"`
				Confidence float64 `json:"confidence"`
				Words      []struct {
					Word       string  `json:"word"`
					Start      float64 `json:"start"`
					End        float64 `json:"end"`
					Confidence float64 `json:"confidence"`
				} `json:"words"`
			}{
				{
					Transcript: "hello world",
					Words: []struct {
						Word       string  `json:"word"`
						Start      float64 `json:"start"`
						End        float64 `json:"end"`
						Confidence float64 `json:"confidence"`
					}{
						{Word: "hello", Start: 0, End: 0.4},
						{Word: "world", Start: 0.5, End: 1},
					},
				},
			},
		},
	}
	converted := m.convertResponse(resp, &internalhttp.Response{})
	if converted.Text != "hello world" || len(converted.Timestamps) != 2 || converted.Usage.DurationSeconds != 5.5 {
		t.Fatalf("converted mismatch: %#v", converted)
	}
	if converted.Language != "en" {
		t.Fatalf("language = %q, want en", converted.Language)
	}
	if got, want := converted.ProviderUsage["seconds"], 5.5; got != want {
		t.Fatalf("converted.ProviderUsage[seconds] = %v, want %v", got, want)
	}
}
