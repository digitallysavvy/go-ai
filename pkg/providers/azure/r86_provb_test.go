package azure

import (
	"reflect"
	"testing"
)

// TestR86provbTranscriptionOptionsGoTypedValues: Go-typed option values
// ([]string locales and phrases, map[string]bool diarization) parse like their
// JSON form (TS parses the JSON value); they failed with "must be an array"
// / "must be an object".
func TestR86provbTranscriptionOptionsGoTypedValues(t *testing.T) {
	typed := map[string]interface{}{"azure": map[string]interface{}{
		"api":         "speech",
		"locales":     []string{"en-US"},
		"diarization": map[string]bool{"enabled": true},
		"phraseList":  map[string]interface{}{"phrases": []string{"Contoso", "Jessie"}},
	}}
	jsonForm := map[string]interface{}{"azure": map[string]interface{}{
		"api":         "speech",
		"locales":     []interface{}{"en-US"},
		"diarization": map[string]interface{}{"enabled": true},
		"phraseList":  map[string]interface{}{"phrases": []interface{}{"Contoso", "Jessie"}},
	}}
	got, err := parseAzureTranscriptionProviderOptions(typed)
	if err != nil {
		t.Fatalf("typed options: %v", err)
	}
	want, err := parseAzureTranscriptionProviderOptions(jsonForm)
	if err != nil {
		t.Fatalf("json options: %v", err)
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("typed = %+v, json = %+v", got, want)
	}
	if len(got.Locales) != 1 || got.PhraseList == nil || len(got.PhraseList.Phrases) != 2 || got.Diarization == nil || !got.Diarization.Enabled {
		t.Fatalf("parsed = %+v", got)
	}
}
