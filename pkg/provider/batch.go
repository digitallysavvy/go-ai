package provider

import (
	"context"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// BatchRequestType discriminates the modality of a batch request.
// Mirrors the TypeScript BatchV4Request union discriminant.
type BatchRequestType string

const (
	BatchRequestTypeText  BatchRequestType = "text"
	BatchRequestTypeImage BatchRequestType = "image"
)

// BatchStatusValue is the normalized lifecycle status of a batch.
type BatchStatusValue string

const (
	BatchStatusPending   BatchStatusValue = "pending"
	BatchStatusCompleted BatchStatusValue = "completed"
	BatchStatusFailed    BatchStatusValue = "failed"
)

// BatchItemStatus is the terminal status of one batch item result.
type BatchItemStatus string

const (
	BatchItemSucceeded BatchItemStatus = "succeeded"
	BatchItemFailed    BatchItemStatus = "failed"
	BatchItemCancelled BatchItemStatus = "cancelled"
	BatchItemExpired   BatchItemStatus = "expired"
)

// BatchError is serializable error information for a batch or batch item.
// Mirrors TypeScript's Experimental_BatchV4Error.
type BatchError struct {
	Message    string `json:"message"`
	Type       string `json:"type,omitempty"`
	Code       string `json:"code,omitempty"`
	StatusCode int    `json:"statusCode,omitempty"`
}

// BatchRequestCounts is the normalized lifecycle counters for a batch.
type BatchRequestCounts struct {
	Total     int `json:"total"`
	Pending   int `json:"pending"`
	Completed int `json:"completed"`
	Failed    int `json:"failed"`
}

// BatchV4Status is the normalized lifecycle status for a batch. Mirrors
// TypeScript's Experimental_BatchV4Status.
type BatchV4Status struct {
	Status           BatchStatusValue
	RawStatus        string
	RequestCounts    *BatchRequestCounts
	Error            *BatchError
	CreatedAt        string
	ExpiresAt        string
	ProviderMetadata map[string]interface{}
}

// TextBatchV4Request is one normalized text generation request within a
// batch. Options reuses GenerateOptions, the same call-options shape used by
// LanguageModel.DoGenerate/DoStream.
type TextBatchV4Request struct {
	// ID application-provided identifier used to correlate the request with
	// its result.
	ID string
	// ModelID is the provider-specific model ID for this request.
	ModelID string
	Options GenerateOptions
}

// ImageBatchV4Request is one image generation request within a batch.
// Options reuses ImageGenerateOptions, the same call-options shape used by
// ImageModel.DoGenerate.
type ImageBatchV4Request struct {
	ID      string
	ModelID string
	Options ImageGenerateOptions
}

// BatchV4Request discriminates between text and image requests by Type.
// Exactly one of Text or Image is populated.
type BatchV4Request struct {
	Type  BatchRequestType
	Text  *TextBatchV4Request
	Image *ImageBatchV4Request
}

// RequestID returns the application-provided ID of the underlying request.
func (r BatchV4Request) RequestID() string {
	switch r.Type {
	case BatchRequestTypeText:
		if r.Text != nil {
			return r.Text.ID
		}
	case BatchRequestTypeImage:
		if r.Image != nil {
			return r.Image.ID
		}
	}
	return ""
}

// ModelID returns the provider-specific model ID of the underlying request.
func (r BatchV4Request) RequestModelID() string {
	switch r.Type {
	case BatchRequestTypeText:
		if r.Text != nil {
			return r.Text.ModelID
		}
	case BatchRequestTypeImage:
		if r.Image != nil {
			return r.Image.ModelID
		}
	}
	return ""
}

// BatchV4Warning pairs a warning with the request it applies to. RequestID is
// empty when the warning does not apply to a specific request.
type BatchV4Warning struct {
	RequestID string
	Warning   types.Warning
}

// BatchV4StartOptions are options for starting a batch of requests,
// discriminated by modality.
type BatchV4StartOptions struct {
	Requests        []BatchV4Request
	ProviderOptions map[string]interface{}
	Headers         map[string]string

	// WebhookURL, when set, is the URL the provider notifies when the batch
	// reaches a terminal state. Providers that do not support completion
	// webhooks should return an unsupported-functionality warning instead of
	// erroring.
	WebhookURL string
}

// BatchV4StartResult is the result of starting a batch.
type BatchV4StartResult struct {
	BatchV4Status
	BatchID  string
	Warnings []BatchV4Warning
}

// BatchV4OperationOptions are options for a batch status/results/cancel
// operation.
type BatchV4OperationOptions struct {
	BatchID         string
	ProviderOptions map[string]interface{}
	Headers         map[string]string
}

// BatchV4CancelResult is the result of requesting cancellation of a batch.
type BatchV4CancelResult struct {
	ProviderMetadata map[string]interface{}
}

// BatchV4ListOptions are options for listing batches.
type BatchV4ListOptions struct {
	ProviderOptions map[string]interface{}
	Headers         map[string]string
	Limit           int
	Cursor          string
}

// BatchV4ListItem is a batch returned by a list operation.
type BatchV4ListItem struct {
	BatchV4Status
	BatchID string
}

// BatchV4ListResult is the result of listing batches.
type BatchV4ListResult struct {
	Batches          []BatchV4ListItem
	NextCursor       string
	ProviderMetadata map[string]interface{}
}

// BatchV4ItemResult is a complete terminal result for one request in a
// batch, discriminated by Type. Exactly one of TextResult/ImageResult is set
// when Status is BatchItemSucceeded.
type BatchV4ItemResult struct {
	Type             BatchRequestType
	ID               string
	Status           BatchItemStatus
	TextResult       *types.GenerateResult
	ImageResult      *types.ImageResult
	Error            *BatchError
	ProviderMetadata map[string]interface{}
}

// BatchV4ItemResultStream streams terminal batch item results. Mirrors the
// Next/Err/Close shape of TextStream: Next returns io.EOF when the stream is
// complete.
type BatchV4ItemResultStream interface {
	// Next returns the next item result in the stream. Returns io.EOF when
	// the stream is complete.
	Next() (*BatchV4ItemResult, error)

	// Err returns any error that occurred during streaming (nil on a clean
	// io.EOF completion).
	Err() error

	// Close releases resources held by the stream.
	Close() error
}

// BatchV4 is the experimental batch processing interface (V4). May change in
// patch releases. Mirrors TypeScript's Experimental_BatchV4.
type BatchV4 interface {
	SpecificationVersion() string

	// Provider returns the batch interface's provider ID, e.g. "gateway.batch".
	Provider() string

	// SupportedURLs returns supported URL patterns (regexp, per media type)
	// for file/image parts in requests handled by this batch interface.
	SupportedURLs() map[string][]string

	DoStartBatch(ctx context.Context, opts BatchV4StartOptions) (*BatchV4StartResult, error)
	DoGetBatchStatus(ctx context.Context, opts BatchV4OperationOptions) (*BatchV4Status, error)
	DoGetBatchResults(ctx context.Context, opts BatchV4OperationOptions) (BatchV4ItemResultStream, error)
}

// BatchV4Canceller is an optional BatchV4 capability for requesting
// cancellation of a batch.
type BatchV4Canceller interface {
	DoCancelBatch(ctx context.Context, opts BatchV4OperationOptions) (*BatchV4CancelResult, error)
}

// BatchV4Lister is an optional BatchV4 capability for listing batches.
type BatchV4Lister interface {
	DoListBatches(ctx context.Context, opts BatchV4ListOptions) (*BatchV4ListResult, error)
}

// BatchProvider is a structural extension a Provider may implement to expose
// a BatchV4 interface. Batch processing is experimental and not part of the
// stable Provider contract, mirroring TypeScript's `experimental_batch()`.
type BatchProvider interface {
	ExperimentalBatch() BatchV4
}
