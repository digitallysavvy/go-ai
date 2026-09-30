package minimax

import (
	"encoding/json"
	"fmt"
)

// VideoModelOptions contains MiniMax-specific options for video generation
// (TS MiniMaxVideoModelOptions).
type VideoModelOptions struct {
	// Resolution is the output resolution ("480P", "768P", or "2K").
	Resolution *string `json:"resolution,omitempty"`

	// Ratio is the aspect ratio of the generated video, overriding the
	// top-level AspectRatio call option.
	Ratio *string `json:"ratio,omitempty"`

	// ReferenceAudioUrls are reference audio URLs for reference-to-video
	// generation.
	ReferenceAudioUrls []string `json:"referenceAudioUrls,omitempty"`

	// AigcWatermark controls whether to embed an AIGC watermark in the
	// output. Defaults to false.
	AigcWatermark *bool `json:"aigcWatermark,omitempty"`

	// PollIntervalMs is the interval in milliseconds between task status
	// polls. Default: 10000.
	PollIntervalMs *int `json:"pollIntervalMs,omitempty"`

	// PollTimeoutMs is the maximum time in milliseconds to poll before
	// timing out. Default: 600000.
	PollTimeoutMs *int `json:"pollTimeoutMs,omitempty"`
}

// extractVideoProviderOptions extracts MiniMax video provider options from
// the generic options map.
func extractVideoProviderOptions(opts map[string]interface{}) (*VideoModelOptions, error) {
	if opts == nil {
		return &VideoModelOptions{}, nil
	}

	mmOpts, ok := opts["minimax"]
	if !ok {
		return &VideoModelOptions{}, nil
	}

	jsonData, err := json.Marshal(mmOpts)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal minimax video provider options: %w", err)
	}

	var provOpts VideoModelOptions
	if err := json.Unmarshal(jsonData, &provOpts); err != nil {
		return nil, fmt.Errorf("failed to unmarshal minimax video provider options: %w", err)
	}
	if provOpts.PollIntervalMs != nil && *provOpts.PollIntervalMs <= 0 {
		return nil, fmt.Errorf("invalid minimax provider option pollIntervalMs: must be positive")
	}
	if provOpts.PollTimeoutMs != nil && *provOpts.PollTimeoutMs <= 0 {
		return nil, fmt.Errorf("invalid minimax provider option pollTimeoutMs: must be positive")
	}

	return &provOpts, nil
}
