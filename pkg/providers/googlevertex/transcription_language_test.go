package googlevertex

import (
	"context"
	"net/http"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TestIso6391MatchesIntlLocale pins the values produced by TS
// `new Intl.Locale(value).language` (Node 24), kept only when two letters.
func TestIso6391MatchesIntlLocale(t *testing.T) {
	cases := map[string]string{
		"en-US": "en", "EN-us": "en", "cmn-Hans-CN": "zh", "cmn": "zh", "zh-CN": "zh",
		"eng": "en", "iw-IL": "he", "in-ID": "id", "tl-PH": "", "fil-PH": "",
		"yue-Hant-HK": "", "und": "", "en_US": "", "e": "", "en-": "", "": "",
		"en-u-ca-gregory": "en", "x-foo": "",
	}
	for in, want := range cases {
		if got := iso6391(in); got != want {
			t.Errorf("iso6391(%q) = %q, want %q", in, got, want)
		}
	}
}

// TestVertexTranscriptionKeepsEmptyStringWord: TS skips only a null word.
func TestVertexTranscriptionKeepsEmptyStringWord(t *testing.T) {
	p, err := New(Config{
		Project: "p", Location: "us", AccessToken: "t",
		HTTPClient: &http.Client{Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			return jsonResponse(200, `{"results":[{"alternatives":[{"transcript":"a","words":[
				{"word":"","startOffset":"0s","endOffset":"1s"},
				{"word":null,"startOffset":"1s","endOffset":"2s"},
				{"word":"a","startOffset":"2s","endOffset":"3s"}]}]}]}`), nil
		})},
	})
	if err != nil {
		t.Fatal(err)
	}
	m, err := p.TranscriptionModel("chirp_2")
	if err != nil {
		t.Fatal(err)
	}
	res, err := m.DoTranscribe(context.Background(), &provider.TranscriptionOptions{AudioBase64: "AQ=="})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Segments) != 2 || res.Segments[0].Text != "" || res.Segments[1].Text != "a" {
		t.Fatalf("segments = %#v", res.Segments)
	}
}
