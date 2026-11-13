package provider_test

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

type batchOnlySpeechModel struct{}

func (batchOnlySpeechModel) SpecificationVersion() string { return "v4" }
func (batchOnlySpeechModel) Provider() string             { return "test" }
func (batchOnlySpeechModel) ModelID() string              { return "m" }
func (batchOnlySpeechModel) DoGenerate(context.Context, *provider.SpeechGenerateOptions) (*types.SpeechResult, error) {
	return nil, nil
}

type streamingSpeechModel struct{ batchOnlySpeechModel }

func (streamingSpeechModel) DoStreamSpeech(context.Context, *provider.SpeechStreamOptions) (*provider.SpeechStreamResult, error) {
	return nil, nil
}

// Streaming speech is an optional capability: the SpeechModel interface is
// unchanged, and a model opts in by also implementing SpeechStreamer.
func TestSpeechStreamerIsOptional(t *testing.T) {
	var batch provider.SpeechModel = batchOnlySpeechModel{}
	if _, ok := batch.(provider.SpeechStreamer); ok {
		t.Fatal("a batch-only speech model must not be a SpeechStreamer")
	}
	var streaming provider.SpeechModel = streamingSpeechModel{}
	if _, ok := streaming.(provider.SpeechStreamer); !ok {
		t.Fatal("a model with DoStreamSpeech must be a SpeechStreamer")
	}
}
