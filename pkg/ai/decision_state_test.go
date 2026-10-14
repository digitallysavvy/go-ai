package ai

import (
	"context"
	"encoding/json"
	"errors"
	"math"
	"reflect"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
)

// Ports the multimodal-state tests from TS decide.test.ts (#22229).

func stateTestModel() *mockDecisionModel {
	return &mockDecisionModel{
		providerName: "test", modelID: "m",
		doDecide: func(context.Context, provider.DecisionCallOptions) (*provider.DecisionResult, error) {
			return &provider.DecisionResult{Answers: map[string]provider.DecisionAnswer{"q": {Type: "choice", Choice: "a"}}}, nil
		},
	}
}

func stateTestQuestions() map[string]provider.DecisionQuestion {
	return map[string]provider.DecisionQuestion{
		"q": {Type: "choice", Instructions: "i", Criteria: map[string]interface{}{"a": nil}},
	}
}

func TestExperimentalDecide_PreservesOrderedStatePartsAndInlinesDataURLs(t *testing.T) {
	model := stateTestModel()
	png := []byte{0x89, 0x50, 0x4e, 0x47}
	_, err := ExperimentalDecide(context.Background(), DecideOptions{
		Model: model, Questions: stateTestQuestions(),
		State: []provider.DecisionStatePart{
			provider.DecisionTextPart(""),
			provider.DecisionJSONPart([]interface{}{1.0, nil, map[string]interface{}{"label": "package"}}),
			provider.DecisionFilePart(types.FileContent{
				MediaType: "image/png", Filename: "package.png",
				FileData: types.FileData{Type: types.FileDataTypeData, Data: png},
			}),
			provider.DecisionFilePart(types.FileContent{
				FileData: types.FileData{Type: types.FileDataTypeURL, URL: "data:image/png;base64,iVBORw=="},
			}),
			provider.DecisionTextPart("Inspect both images."),
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	got := model.calls[0].State.([]provider.DecisionStatePart)
	if len(got) != 5 {
		t.Fatalf("parts = %d", len(got))
	}
	types0 := []string{got[0].Type, got[1].Type, got[2].Type, got[3].Type, got[4].Type}
	if !reflect.DeepEqual(types0, []string{"text", "json", "file", "file", "text"}) {
		t.Fatalf("order = %v", types0)
	}
	mt, data, err := got[2].InlineData()
	if err != nil || mt != "image/png" || !reflect.DeepEqual(data, png) || got[2].File.Filename != "package.png" {
		t.Fatalf("file 2 = %q %v %v %+v", mt, data, err, got[2].File)
	}
	mt, data, err = got[3].InlineData()
	if err != nil || mt != "image/png" || len(data) == 0 {
		t.Fatalf("file 3 = %q %v %v", mt, data, err)
	}
}

func TestExperimentalDecide_EmptyPartsStayEmpty(t *testing.T) {
	model := stateTestModel()
	if _, err := ExperimentalDecide(context.Background(), DecideOptions{Model: model, Questions: stateTestQuestions(), State: []provider.DecisionStatePart{}}); err != nil {
		t.Fatal(err)
	}
	if got := model.calls[0].State.([]provider.DecisionStatePart); len(got) != 0 {
		t.Fatalf("state = %v", got)
	}
}

func TestExperimentalDecide_RejectsMalformedStatePartsBeforeIO(t *testing.T) {
	for name, state := range map[string]interface{}{
		"unknown part type": []provider.DecisionStatePart{{Type: "image"}},
		"file without file": []provider.DecisionStatePart{{Type: "file"}},
		"json NaN":          []provider.DecisionStatePart{provider.DecisionJSONPart(map[string]interface{}{"bad": math.NaN()})},
		"unsupported type":  42,
		"non-JSON object":   map[string]interface{}{"bad": make(chan int)},
	} {
		model := stateTestModel()
		_, err := ExperimentalDecide(context.Background(), DecideOptions{Model: model, Questions: stateTestQuestions(), State: state})
		var target *providererrors.InvalidArgumentError
		if !errors.As(err, &target) {
			t.Fatalf("%s: err = %v, want InvalidArgumentError", name, err)
		}
		if len(model.calls) != 0 {
			t.Fatalf("%s: model was called", name)
		}
	}
}

func TestExperimentalDecide_StopsBeforeModelCallWhenImageDownloadFails(t *testing.T) {
	model := stateTestModel()
	_, err := ExperimentalDecide(context.Background(), DecideOptions{
		Model: model, Questions: stateTestQuestions(),
		// Loopback is blocked by the download SSRF guard, so this fails
		// without any network access.
		State: []provider.DecisionStatePart{provider.DecisionFilePart(types.FileContent{
			MediaType: "image/png",
			FileData:  types.FileData{Type: types.FileDataTypeURL, URL: "http://127.0.0.1/missing.png"},
		})},
	})
	if err == nil {
		t.Fatal("expected a download error")
	}
	if len(model.calls) != 0 {
		t.Fatal("model was called")
	}
}

type recordingStateIntegration struct {
	telemetry.NoopTelemetryIntegration
	mu         sync.Mutex
	modelState interface{}
}

func (r *recordingStateIntegration) OnDecisionModelCallStart(_ context.Context, e telemetry.DecisionModelCallStartEvent) {
	r.mu.Lock()
	r.modelState = e.State
	r.mu.Unlock()
}

func TestExperimentalDecide_NormalizesPublicStateIntoProviderParts(t *testing.T) {
	cases := []struct {
		name     string
		state    interface{}
		expected []provider.DecisionStatePart
	}{
		{"string", "Inspect this package.", []provider.DecisionStatePart{provider.DecisionTextPart("Inspect this package.")}},
		{"object", map[string]interface{}{"product": "glass vase"}, []provider.DecisionStatePart{provider.DecisionJSONPart(map[string]interface{}{"product": "glass vase"})}},
		{"parts", []provider.DecisionStatePart{provider.DecisionJSONPart([]interface{}{1.0, nil}), provider.DecisionTextPart("Inspect.")}, []provider.DecisionStatePart{provider.DecisionJSONPart([]interface{}{1.0, nil}), provider.DecisionTextPart("Inspect.")}},
		// Pre-multimodal JSON arrays keep working as a single json part.
		{"legacy JSON array", []interface{}{"a", 1.0}, []provider.DecisionStatePart{provider.DecisionJSONPart([]interface{}{"a", 1.0})}},
	}
	for _, tc := range cases {
		model := stateTestModel()
		integ := &recordingStateIntegration{}
		var started interface{}
		_, err := ExperimentalDecide(context.Background(), DecideOptions{
			Model: model, Questions: stateTestQuestions(), State: tc.state,
			OnStart:               func(e DecideOnStartEvent) { started = e.State },
			ExperimentalTelemetry: &telemetry.Options{IsEnabled: telemetry.Bool(true), Integrations: []telemetry.TelemetryIntegration{integ}},
		})
		if err != nil {
			t.Fatalf("%s: %v", tc.name, err)
		}
		if !reflect.DeepEqual(model.calls[0].State, tc.expected) {
			t.Fatalf("%s: model state = %#v", tc.name, model.calls[0].State)
		}
		if !reflect.DeepEqual(started, tc.state) {
			t.Fatalf("%s: onStart state = %#v, want the public state", tc.name, started)
		}
		integ.mu.Lock()
		if !reflect.DeepEqual(integ.modelState, tc.expected) {
			t.Fatalf("%s: model-call event state = %#v", tc.name, integ.modelState)
		}
		integ.mu.Unlock()
	}
}

// Ports TS stringifyDecisionStateForTelemetry: files serialize as base64,
// URLs and references as-is.
func TestDecisionStatePart_MarshalJSONMatchesTelemetryShape(t *testing.T) {
	bytes := []byte{0x89, 0x50, 0x4e, 0x47, 0xff, 0xff}
	parts := []provider.DecisionStatePart{
		provider.DecisionTextPart("Inspect."),
		provider.DecisionFilePart(types.FileContent{MediaType: "image/png", Filename: "image.png", FileData: types.FileData{Type: types.FileDataTypeData, Data: bytes}}),
		provider.DecisionFilePart(types.FileContent{MediaType: "image/png", Filename: "image.png", FileData: types.FileData{Type: types.FileDataTypeData, DataString: "iVBOR///"}}),
		provider.DecisionFilePart(types.FileContent{MediaType: "image/png", FileData: types.FileData{Type: types.FileDataTypeURL, URL: "https://example.com/image.png"}}),
		provider.DecisionFilePart(types.FileContent{MediaType: "image/png", FileData: types.FileData{Type: types.FileDataTypeText, Text: "Inline text"}}),
		provider.DecisionJSONPart([]interface{}{1.0, nil}),
	}
	raw, err := json.Marshal(parts)
	if err != nil {
		t.Fatal(err)
	}
	var got []map[string]interface{}
	if err := json.Unmarshal(raw, &got); err != nil {
		t.Fatal(err)
	}
	if got[1]["data"] != "iVBOR///" || got[2]["data"] != "iVBOR///" || got[1]["filename"] != "image.png" {
		t.Fatalf("inline data = %v / %v", got[1], got[2])
	}
	if got[3]["data"] != "https://example.com/image.png" || got[4]["data"] != "Inline text" {
		t.Fatalf("url/text data = %v / %v", got[3], got[4])
	}
	if got[0]["type"] != "text" || got[5]["type"] != "json" {
		t.Fatalf("shape = %v", got)
	}
}
