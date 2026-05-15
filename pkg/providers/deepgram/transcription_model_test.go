package deepgram

import "testing"

func TestDeepgramTranscriptionModelMetadata(t *testing.T) {
	p := New(Config{APIKey: "k"})
	m := NewTranscriptionModel(p, "nova-2")
	if m.SpecificationVersion() != "v3" || m.Provider() != "deepgram" || m.ModelID() != "nova-2" {
		t.Fatalf("metadata mismatch: spec=%s provider=%s model=%s", m.SpecificationVersion(), m.Provider(), m.ModelID())
	}
}

func TestDeepgramConvertResponseHandlesEmptyAndWords(t *testing.T) {
	m := NewTranscriptionModel(New(Config{APIKey: "k"}), "nova-2")

	empty := m.convertResponse(deepgramTranscriptionResponse{
		Metadata: struct {
			TransactionKey string  `json:"transaction_key"`
			RequestID      string  `json:"request_id"`
			Duration       float64 `json:"duration"`
		}{Duration: 3.2},
	})
	if empty.Text != "" || empty.Usage.DurationSeconds != 3.2 || len(empty.Timestamps) != 0 {
		t.Fatalf("empty response mismatch: %#v", empty)
	}

	resp := deepgramTranscriptionResponse{}
	resp.Metadata.Duration = 5.5
	resp.Results.Channels = []struct {
		Alternatives []struct {
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
	converted := m.convertResponse(resp)
	if converted.Text != "hello world" || len(converted.Timestamps) != 2 || converted.Usage.DurationSeconds != 5.5 {
		t.Fatalf("converted mismatch: %#v", converted)
	}
}
