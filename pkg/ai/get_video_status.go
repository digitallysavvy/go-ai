package ai

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// GetVideoStatusOptions configures ExperimentalGetVideoStatus.
type GetVideoStatusOptions struct {
	// Operation is the opaque reference returned by ExperimentalStartVideo.
	Operation json.RawMessage

	// Additional HTTP headers.
	Headers map[string]string

	// Maximum retries for the status call (default: 2). Set to 0 to disable.
	MaxRetries *int
}

// GetVideoStatusResult is the spec-level status payload for an asynchronous
// video generation, discriminated by Status
// (provider.VideoOperationStatusPending / Completed / Error). Unlike
// GenerateVideo, video data is returned as-is (URL/base64/binary) and is not
// downloaded, mirroring TS GetVideoStatusResult (a direct alias of
// Experimental_VideoModelV4OperationStatusResult).
type GetVideoStatusResult struct {
	Status string `json:"status"`

	// Videos is set when Status is provider.VideoOperationStatusCompleted.
	Videos []provider.VideoModelV3VideoData `json:"videos,omitempty"`

	// Error is set when Status is provider.VideoOperationStatusError.
	Error string `json:"error,omitempty"`

	Warnings         []types.Warning            `json:"warnings,omitempty"`
	ProviderMetadata map[string]interface{}     `json:"providerMetadata,omitempty"`
	Response         VideoModelResponseMetadata `json:"response"`
}

// ExperimentalGetVideoStatus checks the status of an asynchronous video
// generation started with ExperimentalStartVideo.
//
// A single check, no polling loop. Poll by calling this on your own
// schedule, or skip polling entirely when the start used WebhookURL and your
// receiver fetches the result after the terminal notification arrives.
func ExperimentalGetVideoStatus(ctx context.Context, model provider.VideoModelV3, opts GetVideoStatusOptions) (*GetVideoStatusResult, error) {
	if model == nil {
		return nil, fmt.Errorf("model is required")
	}
	checker, ok := model.(provider.VideoModelStatusChecker)
	if !ok {
		return nil, fmt.Errorf("Video model %s does not implement doStatus.", model.ModelID())
	}

	statusOpts := &provider.VideoModelV3StatusOptions{
		Operation: opts.Operation,
		Headers:   videoHeadersWithUserAgent(opts.Headers),
	}

	raw, err := doStatusVideoWithRetry(ctx, checker, statusOpts, opts.MaxRetries)
	if err != nil {
		return nil, err
	}

	result := &GetVideoStatusResult{
		Status:           raw.Status,
		Error:            raw.Error,
		Warnings:         raw.Warnings,
		ProviderMetadata: raw.ProviderMetadata,
		Response:         convertResponseInfo(raw.Response, raw.ProviderMetadata, model),
	}
	if raw.Status == provider.VideoOperationStatusCompleted {
		result.Videos = raw.Videos
	}

	return result, nil
}
