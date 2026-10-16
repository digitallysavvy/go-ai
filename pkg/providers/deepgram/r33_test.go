package deepgram

import (
	"errors"
	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"net/http"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS Number("high") is NaN: opus/aac range checks are all false so the value is
// sent verbatim without a warning, while mp3's list check fails and warns.
func TestR33_NonNumericBitRateMatchesNaNSemantics(t *testing.T) {
	m := NewSpeechModel(New(Config{APIKey: "k"}), "aura-2-thalia-en")
	for _, c := range []struct {
		enc      string
		wantRate string
		wantWarn bool
	}{{"opus", "high", false}, {"aac", "high", false}, {"mp3", "", true}} {
		q, warnings, err := m.buildArgs(&provider.SpeechGenerateOptions{Text: "x", ProviderOptions: map[string]interface{}{
			"deepgram": map[string]interface{}{"encoding": c.enc, "bitRate": "high"},
		}})
		if err != nil {
			t.Fatal(err)
		}
		if q["bit_rate"] != c.wantRate || (len(warnings) > 0) != c.wantWarn {
			t.Errorf("%s: query=%v warnings=%+v", c.enc, q, warnings)
		}
	}
	// Numeric strings (Number("48000"), hex) still convert.
	q, _, _ := m.buildArgs(&provider.SpeechGenerateOptions{Text: "x", ProviderOptions: map[string]interface{}{
		"deepgram": map[string]interface{}{"encoding": "mp3", "bitRate": " 48000 "},
	}})
	if q["bit_rate"] != " 48000 " {
		t.Errorf("query=%v", q)
	}
}

// parseInt of a huge digit run is a finite double in JS, not NaN.
func TestR33_HugeSampleRateInFormatSuffix(t *testing.T) {
	q := map[string]string{}
	applyOutputFormat(q, "wav_99999999999999999999")
	if q["sample_rate"] != "100000000000000000000" || !strings.Contains(q["container"], "wav") {
		t.Fatalf("query=%v", q)
	}
}

// TS headerNumber is Number(value): an empty header is 0, a fractional one is
// kept, and an empty dg-additional-model-uuids splits to [""].
func TestR33_SpeechHeaderNumbers(t *testing.T) {
	h := http.Header{}
	h.Set("Dg-Char-Count", "")
	h.Set("Dg-Breaks-Applied", "2.5")
	h.Set("Dg-Pronunciations-Applied", "abc")
	h.Set("Dg-Additional-Model-Uuids", "")
	meta := buildSpeechProviderMetadata(h)
	if meta["charCount"] != 0 || meta["breaksApplied"] != 2.5 {
		t.Errorf("meta = %v", meta)
	}
	if _, ok := meta["pronunciationsApplied"]; ok {
		t.Errorf("NaN header kept: %v", meta)
	}
	if u, _ := meta["additionalModelUuids"].([]string); len(u) != 1 || u[0] != "" {
		t.Errorf("uuids = %v", meta["additionalModelUuids"])
	}
}

// z.string().optional() rejects a null request_id, so the body falls back to
// the HTTP status text.
func TestR33_NullRequestIDFailsSchema(t *testing.T) {
	err := handleError("deepgram", &internalhttp.HTTPStatusError{StatusCode: 400, Body: []byte(`{"err_code":"X","err_msg":"bad","request_id":null}`)})
	var perr *providererrors.ProviderError
	if !errors.As(err, &perr) || perr.Message != "Bad Request" {
		t.Fatalf("got %v", err)
	}
	err = handleError("deepgram", &internalhttp.HTTPStatusError{StatusCode: 400, Body: []byte(`{"err_code":"X","err_msg":"bad","request_id":"r"}`)})
	if !errors.As(err, &perr) || perr.Message != "bad" {
		t.Fatalf("got %v", err)
	}
}

// voice?.trim() is JS trim: a BOM-only voice is empty, a NEL-only voice is not.
func TestR33_VoiceTrimIsJSTrim(t *testing.T) {
	m := NewSpeechModel(New(Config{APIKey: "k"}), "aura-2")
	if _, _, err := m.buildArgs(&provider.SpeechGenerateOptions{Text: "x", Voice: "\ufeff"}); err == nil {
		t.Error("BOM-only voice accepted")
	}
	q, _, err := m.buildArgs(&provider.SpeechGenerateOptions{Text: "x", Voice: "\u0085"})
	if err != nil || q["model"] != "aura-2-\u0085-en" {
		t.Errorf("NEL voice: %v %v", q, err)
	}
}
