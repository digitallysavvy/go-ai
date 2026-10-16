package revai

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

func revaiSeenConfig(t *testing.T, opts map[string]interface{}) map[string]interface{} {
	t.Helper()
	var seen map[string]interface{}
	p := newRevaiTestServer(t, func(path string, r *http.Request) {
		if path != "/speechtotext/v1/jobs" {
			return
		}
		if err := r.ParseMultipartForm(10 << 20); err != nil {
			t.Fatalf("ParseMultipartForm: %v", err)
		}
		_ = json.Unmarshal([]byte(r.FormValue("config")), &seen)
	})
	model, _ := p.TranscriptionModel(ModelMachine)
	if _, err := model.DoTranscribe(context.Background(), &provider.TranscriptionOptions{
		Audio: revaiTestAudio, MimeType: "audio/wav", ProviderOptions: opts,
	}); err != nil {
		t.Fatalf("DoTranscribe: %v", err)
	}
	return seen
}

// TS delete_after_seconds is z.number(): a fractional value is valid and must
// not drop every other option.
func TestR29_FractionalNumberKeepsOtherOptions(t *testing.T) {
	cfg := revaiSeenConfig(t, map[string]interface{}{"revai": map[string]interface{}{
		"delete_after_seconds": 1.5, "metadata": "m", "speakers_count": 2.5,
	}})
	if cfg["delete_after_seconds"] != 1.5 || cfg["metadata"] != "m" || cfg["speakers_count"] != 2.5 {
		t.Fatalf("config = %#v", cfg)
	}
}

// An explicit null is not defaulted (zod default only fires on undefined) and
// is then dropped by `?? undefined`.
func TestR29_ExplicitNullOmitted(t *testing.T) {
	cfg := revaiSeenConfig(t, map[string]interface{}{"revai": map[string]interface{}{
		"rush": nil, "language": nil, "metadata": "x",
	}})
	if _, ok := cfg["rush"]; ok {
		t.Fatalf("rush should be omitted: %#v", cfg)
	}
	if _, ok := cfg["language"]; ok {
		t.Fatalf("language should be omitted: %#v", cfg)
	}
	if cfg["test_mode"] != false || cfg["metadata"] != "x" {
		t.Fatalf("config = %#v", cfg)
	}
}

// `currentSegmentText += element.value` renders null as "null"; Array.join
// renders it as "".
func TestR29_NullElementValue(t *testing.T) {
	var tr revaiTranscriptResponse
	if err := json.Unmarshal([]byte(`{"monologues":[{"elements":[
		{"type":"text","value":"a","ts":0,"end_ts":1},
		{"type":"text","value":null,"ts":1,"end_ts":2}]}]}`), &tr); err != nil {
		t.Fatal(err)
	}
	text, segs, _ := convertRevaiTranscript(tr)
	if text != "a" {
		t.Fatalf("text = %q", text)
	}
	if len(segs) != 2 || segs[1].Text != "null" {
		t.Fatalf("segments = %#v", segs)
	}
}
