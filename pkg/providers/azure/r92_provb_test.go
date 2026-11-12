package azure

import (
	"testing"
)

// The exported Azure option structs are read through their JSON form (round
// 91 typed-literal readers), so a typed literal passed as
// providerOptions.azure must carry the TS option keys. Without JSON tags the
// image options were silently ignored (keys "API", "WebGrounding") and the
// strict speech / transcription schemas rejected them as unrecognized keys.
func TestR92provbAzureTypedOptionStructs(t *testing.T) {
	yes := true
	t.Run("image", func(t *testing.T) {
		got, err := parseAzureImageProviderOptions(map[string]interface{}{"azure": AzureImageModelOptions{API: "mai", WebGrounding: &yes}})
		if err != nil || got.API != "mai" || got.WebGrounding == nil || !*got.WebGrounding || got.AutoAspectRatio != nil {
			t.Fatalf("got %+v, %v", got, err)
		}
		got, err = parseAzureImageProviderOptions(map[string]interface{}{"azure": &AzureImageModelOptions{AutoAspectRatio: &yes}})
		if err != nil || got.AutoAspectRatio == nil || !*got.AutoAspectRatio {
			t.Fatalf("pointer: got %+v, %v", got, err)
		}
	})
	t.Run("speech", func(t *testing.T) {
		degree := 1.5
		got, err := parseAzureSpeechProviderOptions(map[string]interface{}{"azure": AzureSpeechModelOptions{
			API: AzureTranscriptionAPISpeech, AzureSpeechModelSpeechOptions: AzureSpeechModelSpeechOptions{Style: "excited", StyleDegree: &degree},
		}})
		if err != nil || got.API != AzureTranscriptionAPISpeech || got.Style != "excited" || got.StyleDegree == nil || *got.StyleDegree != 1.5 {
			t.Fatalf("got %+v, %v", got, err)
		}
	})
	t.Run("transcription", func(t *testing.T) {
		got, err := parseAzureTranscriptionProviderOptions(map[string]interface{}{"azure": AzureTranscriptionModelOptions{
			API: AzureTranscriptionAPISpeech,
			AzureSpeechTranscriptionOptions: AzureSpeechTranscriptionOptions{
				Timestamps: "word", TranscribeStyle: "clean", Locales: []string{"en"},
				Diarization: &AzureDiarizationOptions{Enabled: true}, PhraseList: &AzurePhraseListOptions{Phrases: []string{"go-ai"}},
			},
		}})
		if err != nil || got.API != AzureTranscriptionAPISpeech || got.Timestamps != "word" || got.TranscribeStyle != "clean" ||
			len(got.Locales) != 1 || got.Diarization == nil || !got.Diarization.Enabled || got.PhraseList == nil || got.PhraseList.Phrases[0] != "go-ai" {
			t.Fatalf("got %+v, %v", got, err)
		}
		got, err = parseAzureTranscriptionProviderOptions(map[string]interface{}{"azure": &AzureTranscriptionModelOptions{API: AzureTranscriptionAPIMai, Language: "de"}})
		if err != nil || got.API != AzureTranscriptionAPIMai || got.Language != "de" {
			t.Fatalf("mai: got %+v, %v", got, err)
		}
	})
}
