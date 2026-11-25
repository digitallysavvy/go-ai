package providers_test

import (
	"encoding/json"
	"reflect"
	"sort"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/providers/anthropic"
	"github.com/digitallysavvy/go-ai/pkg/providers/gateway"
	"github.com/digitallysavvy/go-ai/pkg/providers/google"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
	"github.com/digitallysavvy/go-ai/pkg/providers/scaledown"
)

// TestIntegPort2ExtractionRegistryKeys pins which extraction models are
// restorable: the ones TS gives [WORKFLOW_SERIALIZE] (ScaleDownExtractionModel
// and the provider-utils ExtractionLanguageModel behind the anthropic, openai
// and google factories). GatewayExtractionModel has no workflow serialization
// in TS, so no gateway key is registered.
func TestIntegPort2ExtractionRegistryKeys(t *testing.T) {
	got := provider.RegisteredExtractionModelDeserializerKeys()
	sort.Strings(got)
	want := []string{"anthropic.extraction", "google.extraction", "openai.extraction", "scaledown.extraction"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("extraction deserializer keys = %v, want %v", got, want)
	}
}

// TestIntegPort2ExtractionFactoriesRoundTripThroughJSON serializes every
// provider factory's extraction model, sends it through JSON (a workflow
// boundary), restores it with provider.DeserializeExtractionModel, and checks
// identity, maxFieldsPerCall and that no API key travels.
func TestIntegPort2ExtractionFactoriesRoundTripThroughJSON(t *testing.T) {
	const secret = "sk-integ-port2-extraction-secret"
	gw, err := gateway.New(gateway.Config{APIKey: secret})
	if err != nil {
		t.Fatal(err)
	}
	gwModel, err := gw.ExtractionModel("scaledown/extract-1")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := provider.SerializeExtractionModel(gwModel); err == nil {
		t.Fatal("gateway extraction model serialized; TS GatewayExtractionModel has no [WORKFLOW_SERIALIZE]")
	}

	cases := []struct {
		name      string
		build     func() (provider.ExtractionModel, error)
		provider  string
		maxFields interface{}
	}{
		{"anthropic", func() (provider.ExtractionModel, error) {
			return anthropic.New(anthropic.Config{APIKey: secret}).ExtractionModel("claude-sonnet-4-6")
		}, "anthropic.extraction", float64(16)},
		{"openai", func() (provider.ExtractionModel, error) {
			return openai.New(openai.Config{APIKey: secret}).ExtractionModel("gpt-5")
		}, "openai.extraction", nil},
		{"google", func() (provider.ExtractionModel, error) {
			return google.New(google.Config{APIKey: secret}).ExtractionModel("gemini-2.5-flash")
		}, "google.extraction", nil},
		{"scaledown", func() (provider.ExtractionModel, error) {
			return scaledown.New(scaledown.Config{APIKey: secret}).ExtractionModel("extract-1")
		}, "scaledown.extraction", nil},
	}
	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			model, err := tc.build()
			if err != nil {
				t.Fatal(err)
			}
			serialized, err := provider.SerializeExtractionModel(model)
			if err != nil {
				t.Fatalf("SerializeExtractionModel: %v", err)
			}
			if serialized.Provider != tc.provider {
				t.Fatalf("serialized provider = %q, want %q", serialized.Provider, tc.provider)
			}
			data, err := json.Marshal(serialized)
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(data), secret) {
				t.Fatalf("serialized extraction model carries the API key: %s", data)
			}
			var loaded provider.SerializedModel
			if err := json.Unmarshal(data, &loaded); err != nil {
				t.Fatal(err)
			}
			if got := loaded.Config["maxFieldsPerCall"]; !reflect.DeepEqual(got, tc.maxFields) {
				t.Fatalf("config.maxFieldsPerCall = %v, want %v", got, tc.maxFields)
			}
			restored, err := provider.DeserializeExtractionModel(loaded)
			if err != nil {
				t.Fatalf("DeserializeExtractionModel: %v", err)
			}
			if restored.Provider() != model.Provider() || restored.ModelID() != model.ModelID() {
				t.Fatalf("restored %s/%s, want %s/%s", restored.Provider(), restored.ModelID(), model.Provider(), model.ModelID())
			}
			again, err := provider.SerializeExtractionModel(restored)
			if err != nil {
				t.Fatal(err)
			}
			againData, _ := json.Marshal(again)
			if string(againData) != string(data) {
				t.Fatalf("second serialization differs:\n got %s\nwant %s", againData, data)
			}
		})
	}
}

// TestIntegPort2ExtractionLanguageModelRejectsMissingInnerModel: a payload
// without the wrapped language model cannot be restored.
func TestIntegPort2ExtractionLanguageModelRejectsMissingInnerModel(t *testing.T) {
	for _, key := range []string{"anthropic.extraction", "openai.extraction", "google.extraction"} {
		if _, err := provider.DeserializeExtractionModel(provider.SerializedModel{Provider: key, ModelID: "m", Config: map[string]interface{}{}}); err == nil {
			t.Errorf("%s: restored without config.model", key)
		}
	}
}
