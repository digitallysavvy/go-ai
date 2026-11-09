package googlevertex

import (
	"errors"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// provc CR (TS generate-video.ts useStartStatus): TS GoogleVertexVideoModel
// has no doGenerate, so the model marks itself start/status only for
// ai.GenerateVideo, returning the v0.5.2 poll provider options (0 = core default).
func TestR89provgVideoStartStatusPollDefaults(t *testing.T) {
	p, err := New(Config{APIKey: "k"})
	if err != nil {
		t.Fatal(err)
	}
	var model interface {
		VideoStartStatusPollDefaults(map[string]interface{}) (int, int)
	} = NewVideoModel(p, "veo-3.0-generate-001")
	if i, to := model.VideoStartStatusPollDefaults(map[string]interface{}{}); i != 0 || to != 0 {
		t.Fatalf("defaults = %d, %d; want 0, 0", i, to)
	}
	for _, key := range []string{"googleVertex", "vertex"} {
		i, to := model.VideoStartStatusPollDefaults(map[string]interface{}{key: map[string]interface{}{"pollIntervalMs": float64(250), "pollTimeoutMs": float64(90000)}})
		if i != 250 || to != 90000 {
			t.Fatalf("%s: defaults = %d, %d; want 250, 90000", key, i, to)
		}
	}
}

// The exported VertexEmbeddingProviderOptions given as the options entry is
// read like the TS options object (it used to fail with "value must be an
// object" and was never applied); option errors carry the TS
// parseProviderOptions message "invalid <key> provider options".
func TestR89provgVertexEmbeddingTypedOptions(t *testing.T) {
	dims := 64
	typed := VertexEmbeddingProviderOptions{TaskType: "RETRIEVAL_QUERY", OutputDimensionality: &dims}
	for _, v := range []interface{}{typed, &typed} {
		opts := &provider.EmbedModelOptions{ProviderOptions: map[string]interface{}{"googleVertex": v}}
		if err := validateVertexEmbeddingOptions(opts); err != nil {
			t.Fatalf("typed options rejected: %v", err)
		}
		got := vertexEmbeddingOptions(opts)
		if got.TaskType != "RETRIEVAL_QUERY" || got.OutputDimensionality == nil || *got.OutputDimensionality != 64 {
			t.Fatalf("typed options not applied: %+v", got)
		}
	}
	err := validateVertexEmbeddingOptions(&provider.EmbedModelOptions{ProviderOptions: map[string]interface{}{
		"vertex": map[string]interface{}{"taskType": "NOPE"},
	}})
	var ae *providererrors.InvalidArgumentError
	if !errors.As(err, &ae) || ae.Field != "providerOptions" || ae.Message != "invalid vertex provider options" || ae.Cause == nil {
		t.Fatalf("error = %#v", err)
	}
}

// prova CR (TS provider-utils load-setting.ts): a missing project or location
// is a typed LoadSettingError with the TS text.
func TestR89provgVertexLoadSettingErrors(t *testing.T) {
	t.Setenv("GOOGLE_VERTEX_PROJECT", "")
	t.Setenv("GOOGLE_VERTEX_LOCATION", "")
	check := func(err error, want string) {
		t.Helper()
		var lse *providererrors.LoadSettingError
		if !errors.As(err, &lse) || err.Error() != want {
			t.Fatalf("error = %T %v", err, err)
		}
	}
	p := &Provider{}
	_, err := p.loadProject()
	check(err, "Google Vertex project setting is missing. Pass it using the 'project' parameter or the GOOGLE_VERTEX_PROJECT environment variable.")
	_, err = p.loadLocation()
	check(err, "Google Vertex location setting is missing. Pass it using the 'location' parameter or the GOOGLE_VERTEX_LOCATION environment variable.")
	check(NewMaaS(MaaSConfig{}).init(), "Google Vertex project setting is missing. Pass it using the 'project' parameter or the GOOGLE_VERTEX_PROJECT environment variable.")
}
