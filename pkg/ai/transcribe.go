package ai

import (
	"context"
	"fmt"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TranscribeOptions contains options for speech-to-text transcription.
type TranscribeOptions struct {
	Model provider.TranscriptionModel

	Audio      []byte
	MimeType   string
	Language   string
	Timestamps bool
	Headers    map[string]string
}

// TranscribeResult contains speech transcription output.
type TranscribeResult struct {
	Text              string                         `json:"text"`
	Segments          []TranscriptionSegment         `json:"segments"`
	Language          string                         `json:"language,omitempty"`
	DurationInSeconds *float64                       `json:"durationInSeconds,omitempty"`
	Timestamps        []types.TranscriptionTimestamp `json:"timestamps,omitempty"`
	Warnings          []types.Warning                `json:"warnings,omitempty"`
	Responses         []*types.ResponseMetadata      `json:"responses,omitempty"`
	ProviderMetadata  map[string]interface{}         `json:"providerMetadata,omitempty"`
	Usage             types.TranscriptionUsage       `json:"usage"`
}

type TranscriptionSegment struct {
	Text        string  `json:"text"`
	StartSecond float64 `json:"startSecond"`
	EndSecond   float64 `json:"endSecond"`
}

// Transcribe converts audio bytes to text.
func Transcribe(ctx context.Context, opts TranscribeOptions) (*TranscribeResult, error) {
	if opts.Model == nil {
		return nil, fmt.Errorf("model is required")
	}
	if len(opts.Audio) == 0 {
		return nil, fmt.Errorf("audio is required")
	}
	raw, err := opts.Model.DoTranscribe(ctx, &provider.TranscriptionOptions{
		Audio:      opts.Audio,
		MimeType:   opts.MimeType,
		Language:   opts.Language,
		Timestamps: opts.Timestamps,
		Headers:    opts.Headers,
	})
	if err != nil {
		return nil, err
	}
	if raw == nil || raw.Text == "" {
		return nil, fmt.Errorf("no transcript generated")
	}
	segments := make([]TranscriptionSegment, 0, len(raw.Timestamps))
	for _, ts := range raw.Timestamps {
		segments = append(segments, TranscriptionSegment{
			Text:        ts.Text,
			StartSecond: ts.Start,
			EndSecond:   ts.End,
		})
	}
	return &TranscribeResult{
		Text:       raw.Text,
		Segments:   segments,
		Language:   opts.Language,
		Timestamps: raw.Timestamps,
		Warnings:   []types.Warning{},
		Responses: []*types.ResponseMetadata{{
			ID:        newCallID(),
			Timestamp: time.Now(),
			ModelID:   opts.Model.ModelID(),
		}},
		ProviderMetadata: map[string]interface{}{},
		Usage:            raw.Usage,
	}, nil
}
