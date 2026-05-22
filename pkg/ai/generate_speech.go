package ai

import (
	"context"
	"fmt"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// GenerateSpeechOptions contains options for speech generation.
type GenerateSpeechOptions struct {
	Model provider.SpeechModel
	Text  string
	Voice string
	Speed *float64

	OutputFormat    string
	Instructions    string
	Language        string
	ProviderOptions map[string]interface{}
	Headers         map[string]string
}

// GenerateSpeechResult contains generated speech audio.
type GenerateSpeechResult struct {
	Audio            types.GeneratedFile       `json:"audio"`
	Warnings         []types.Warning           `json:"warnings,omitempty"`
	Responses        []*types.ResponseMetadata `json:"responses,omitempty"`
	ProviderMetadata map[string]interface{}    `json:"providerMetadata,omitempty"`
	Usage            types.SpeechUsage         `json:"usage"`
}

// GenerateSpeech converts text to speech audio.
func GenerateSpeech(ctx context.Context, opts GenerateSpeechOptions) (*GenerateSpeechResult, error) {
	if opts.Model == nil {
		return nil, fmt.Errorf("model is required")
	}
	if opts.Text == "" {
		return nil, fmt.Errorf("text is required")
	}
	raw, err := opts.Model.DoGenerate(ctx, &provider.SpeechGenerateOptions{
		Text:            opts.Text,
		Voice:           opts.Voice,
		Speed:           opts.Speed,
		OutputFormat:    opts.OutputFormat,
		Instructions:    opts.Instructions,
		Language:        opts.Language,
		ProviderOptions: opts.ProviderOptions,
		Headers:         opts.Headers,
	})
	if err != nil {
		return nil, err
	}
	if raw == nil || len(raw.Audio) == 0 {
		return nil, fmt.Errorf("no speech generated")
	}
	return &GenerateSpeechResult{
		Audio: types.GeneratedFile{
			Data:      raw.Audio,
			MediaType: raw.MimeType,
		},
		Warnings: raw.Warnings,
		Responses: []*types.ResponseMetadata{{
			ID:        newCallID(),
			Timestamp: time.Now(),
			ModelID:   opts.Model.ModelID(),
		}},
		ProviderMetadata: raw.ProviderMetadata,
		Usage:            raw.Usage,
	}, nil
}
