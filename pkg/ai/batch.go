package ai

import (
	"context"
	"fmt"
	"reflect"
	"strings"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/gateway"
	promptutils "github.com/digitallysavvy/go-ai/pkg/providerutils/prompt"
	"github.com/digitallysavvy/go-ai/pkg/version"
)

// BatchTextRequest is one normalized text generation request within a batch.
// Mirrors TypeScript's TextBatchRequest.
type BatchTextRequest struct {
	// ID is an application-provided identifier used to correlate the
	// request with its result. Must be nonempty and unique within the batch.
	ID string

	// Model is the provider-specific model ID for this request.
	Model string

	Prompt   string
	Messages []types.Message
	System   string

	Temperature      *float64
	MaxTokens        *int
	TopP             *float64
	TopK             *int
	FrequencyPenalty *float64
	PresencePenalty  *float64
	StopSequences    []string
	Seed             *int

	// Tools available for the model to call in this request. Their Execute
	// functions are never invoked by the batch APIs; only the definitions
	// are sent to the provider. Every request in a batch that names the same
	// tool must give it an identical definition.
	Tools      []types.Tool
	ToolChoice types.ToolChoice

	// ToolOrder controls the order tools are sent to the provider for this
	// request. Mirrors TypeScript's TextBatchRequest.toolOrder.
	ToolOrder []string

	// ToolsContext resolves per-tool dynamic descriptions (Tool.DescriptionFunc)
	// before the request is sent. Mirrors TypeScript's
	// TextBatchRequest.toolsContext, threaded through prepareTools.
	ToolsContext map[string]interface{}

	Reasoning      *types.ReasoningLevel
	ResponseFormat *provider.ResponseFormat

	ProviderOptions map[string]interface{}
}

// BatchImageRequest is one image generation request within a batch. Mirrors
// TypeScript's ImageBatchRequest.
type BatchImageRequest struct {
	ID    string
	Model string

	Prompt      string
	N           *int
	Size        string
	AspectRatio string
	Seed        *int
	Files       []provider.ImageFile
	Mask        *provider.ImageFile

	ProviderOptions map[string]interface{}
}

// BatchRequest is one request within a batch, discriminated by which of Text
// or Image is set. Exactly one must be non-nil.
type BatchRequest struct {
	Text  *BatchTextRequest
	Image *BatchImageRequest
}

func (r BatchRequest) requestType() (provider.BatchRequestType, error) {
	switch {
	case r.Text != nil && r.Image == nil:
		return provider.BatchRequestTypeText, nil
	case r.Image != nil && r.Text == nil:
		return provider.BatchRequestTypeImage, nil
	default:
		return "", &providererrors.InvalidArgumentError{
			Field:   "requests",
			Message: "each request must set exactly one of Text or Image",
		}
	}
}

func (r BatchRequest) id() string {
	if r.Text != nil {
		return r.Text.ID
	}
	if r.Image != nil {
		return r.Image.ID
	}
	return ""
}

func (r BatchRequest) model() string {
	if r.Text != nil {
		return r.Text.Model
	}
	if r.Image != nil {
		return r.Image.Model
	}
	return ""
}

// BatchReference is the persisted reference for a batch: enough information
// to resume status/results/cancel operations later without holding a live
// provider.BatchV4 handle. Mirrors TypeScript's BatchReference.
type BatchReference struct {
	Version  int
	ID       string
	Provider string
}

// Batch is a batch reference plus its latest known lifecycle status. Mirrors
// TypeScript's `Batch = BatchReference & BatchStatus`.
type Batch struct {
	BatchReference
	provider.BatchV4Status
}

// StartBatchOptions are options for starting a batch of requests.
type StartBatchOptions struct {
	// Provider is a provider.BatchV4 instance, a provider.BatchProvider (a
	// Provider exposing ExperimentalBatch()), or nil. When nil, batches are
	// processed through the AI Gateway (mirrors TypeScript:
	// `globalThis.AI_SDK_DEFAULT_PROVIDER ?? gateway`).
	Provider interface{}

	Requests        []BatchRequest
	ProviderOptions map[string]interface{}

	// WebhookURL, when set, is the URL the provider notifies when the batch
	// reaches a terminal state. Providers that do not support completion
	// webhooks return an unsupported-functionality warning instead of
	// erroring.
	WebhookURL string

	MaxRetries *int
	Headers    map[string]string
	Timeout    *time.Duration
}

// StartBatchResult is the acknowledged batch and warnings produced while
// starting it.
type StartBatchResult struct {
	Batch
	Warnings []provider.BatchV4Warning
}

// ExperimentalStartBatch starts a batch of text and/or image generation
// requests. Experimental: may change in patch releases.
func ExperimentalStartBatch(ctx context.Context, opts StartBatchOptions) (*StartBatchResult, error) {
	if err := validateBatchRequests(opts.Requests); err != nil {
		return nil, err
	}
	if err := validateBatchToolCompatibility(opts.Requests); err != nil {
		return nil, err
	}
	if err := validateMaxRetries(opts.MaxRetries); err != nil {
		return nil, err
	}

	batchAPI, err := resolveBatchAPI(opts.Provider)
	if err != nil {
		return nil, err
	}

	callCtx, cancel := withOptionalTimeout(ctx, opts.Timeout)
	defer cancel()

	// Mirrors TypeScript's `const supportedUrls = await batchApi.supportedUrls`
	// in packages/ai/src/batch/batch.ts: URL support is queried from the
	// batch interface itself (requests may target arbitrary per-request
	// models), not from a single provider.LanguageModel.
	urlChecker := SupportedURLCheckerFromPatterns(batchAPI.SupportedURLs())

	providerRequests := make([]provider.BatchV4Request, 0, len(opts.Requests))
	for _, req := range opts.Requests {
		if err := callCtx.Err(); err != nil {
			return nil, err
		}
		pr, buildErr := buildBatchProviderRequest(callCtx, req, urlChecker)
		if buildErr != nil {
			return nil, buildErr
		}
		providerRequests = append(providerRequests, pr)
	}

	result, err := batchAPI.DoStartBatch(callCtx, provider.BatchV4StartOptions{
		Requests:        providerRequests,
		ProviderOptions: opts.ProviderOptions,
		Headers:         version.WithUserAgentSuffix(opts.Headers, version.UserAgent()),
		WebhookURL:      opts.WebhookURL,
	})
	if err != nil {
		return nil, err
	}

	modelByRequestID := make(map[string]string, len(opts.Requests))
	for _, req := range opts.Requests {
		modelByRequestID[req.id()] = req.model()
	}
	for _, w := range result.Warnings {
		model := ""
		if w.RequestID != "" {
			model = modelByRequestID[w.RequestID]
		}
		logModelWarnings([]types.Warning{w.Warning}, batchAPI.Provider(), model)
	}

	return &StartBatchResult{
		Batch: Batch{
			BatchReference: BatchReference{Version: 2, ID: result.BatchID, Provider: batchAPI.Provider()},
			BatchV4Status:  result.BatchV4Status,
		},
		Warnings: result.Warnings,
	}, nil
}

// GetBatchStatusOptions are options for retrieving batch status.
type GetBatchStatusOptions struct {
	Provider        interface{}
	Batch           BatchReference
	ProviderOptions map[string]interface{}
	MaxRetries      *int
	Headers         map[string]string
	Timeout         *time.Duration
}

// ExperimentalGetBatchStatus retrieves the latest normalized status for a
// batch. Experimental: may change in patch releases.
func ExperimentalGetBatchStatus(ctx context.Context, opts GetBatchStatusOptions) (*Batch, error) {
	batchAPI, err := resolveBatchAPI(opts.Provider)
	if err != nil {
		return nil, err
	}
	if err := validateBatchReference(batchAPI, opts.Batch); err != nil {
		return nil, err
	}
	if err := validateMaxRetries(opts.MaxRetries); err != nil {
		return nil, err
	}

	callCtx, cancel := withOptionalTimeout(ctx, opts.Timeout)
	defer cancel()

	headers := version.WithUserAgentSuffix(opts.Headers, version.UserAgent())
	maxRetries := preparedMaxRetries(opts.MaxRetries)

	var status *provider.BatchV4Status
	err = withEmbedRetry(callCtx, maxRetries, func(retryCtx context.Context) error {
		s, callErr := batchAPI.DoGetBatchStatus(retryCtx, provider.BatchV4OperationOptions{
			BatchID:         opts.Batch.ID,
			ProviderOptions: opts.ProviderOptions,
			Headers:         headers,
		})
		if callErr != nil {
			return callErr
		}
		status = s
		return nil
	})
	if err != nil {
		return nil, err
	}

	return &Batch{BatchReference: opts.Batch, BatchV4Status: *status}, nil
}

// GetBatchResultsOptions are options for streaming batch results.
type GetBatchResultsOptions struct {
	Provider        interface{}
	Batch           BatchReference
	ProviderOptions map[string]interface{}
	MaxRetries      *int
	Headers         map[string]string
	Timeout         *time.Duration
}

// BatchItemResult is a complete terminal result for one request in a batch.
type BatchItemResult struct {
	Type   provider.BatchRequestType
	ID     string
	Status provider.BatchItemStatus

	// Text is populated when Type is text and Status is succeeded.
	Text *types.GenerateResult

	// Image is populated when Type is image and Status is succeeded.
	Image *types.ImageResult

	Error            *provider.BatchError
	ProviderMetadata map[string]interface{}
}

// BatchResultsStream streams terminal results for the requests in a batch.
// Mirrors the Next/Err/Close shape of provider.TextStream: Next returns
// io.EOF when the stream is complete.
type BatchResultsStream struct {
	inner  provider.BatchV4ItemResultStream
	cancel context.CancelFunc
}

// Next returns the next item result in the stream. Returns io.EOF when the
// stream is complete.
func (s *BatchResultsStream) Next() (*BatchItemResult, error) {
	item, err := s.inner.Next()
	if err != nil {
		return nil, err
	}
	return &BatchItemResult{
		Type:             item.Type,
		ID:               item.ID,
		Status:           item.Status,
		Text:             item.TextResult,
		Image:            item.ImageResult,
		Error:            item.Error,
		ProviderMetadata: item.ProviderMetadata,
	}, nil
}

// Err returns any error that occurred during streaming.
func (s *BatchResultsStream) Err() error {
	return s.inner.Err()
}

// Close releases resources held by the stream, including the operation
// timeout context (if any).
func (s *BatchResultsStream) Close() error {
	err := s.inner.Close()
	if s.cancel != nil {
		s.cancel()
	}
	return err
}

// ExperimentalGetBatchResults streams complete terminal results for the
// requests in a batch. Experimental: may change in patch releases.
func ExperimentalGetBatchResults(ctx context.Context, opts GetBatchResultsOptions) (*BatchResultsStream, error) {
	batchAPI, err := resolveBatchAPI(opts.Provider)
	if err != nil {
		return nil, err
	}
	if err := validateBatchReference(batchAPI, opts.Batch); err != nil {
		return nil, err
	}
	if err := validateMaxRetries(opts.MaxRetries); err != nil {
		return nil, err
	}

	callCtx, cancel := withOptionalTimeout(ctx, opts.Timeout)

	headers := version.WithUserAgentSuffix(opts.Headers, version.UserAgent())
	maxRetries := preparedMaxRetries(opts.MaxRetries)

	var inner provider.BatchV4ItemResultStream
	err = withEmbedRetry(callCtx, maxRetries, func(retryCtx context.Context) error {
		s, callErr := batchAPI.DoGetBatchResults(retryCtx, provider.BatchV4OperationOptions{
			BatchID:         opts.Batch.ID,
			ProviderOptions: opts.ProviderOptions,
			Headers:         headers,
		})
		if callErr != nil {
			return callErr
		}
		inner = s
		return nil
	})
	if err != nil {
		cancel()
		return nil, err
	}

	return &BatchResultsStream{inner: inner, cancel: cancel}, nil
}

// CancelBatchOptions are options for requesting cancellation of a batch.
type CancelBatchOptions struct {
	Provider        interface{}
	Batch           BatchReference
	ProviderOptions map[string]interface{}
	Headers         map[string]string
	Timeout         *time.Duration
}

// CancelBatchResult is the result of requesting cancellation of a batch.
type CancelBatchResult struct {
	ProviderMetadata map[string]interface{}
}

// ExperimentalCancelBatch requests cancellation of a batch. Experimental: may
// change in patch releases.
func ExperimentalCancelBatch(ctx context.Context, opts CancelBatchOptions) (*CancelBatchResult, error) {
	batchAPI, err := resolveBatchAPI(opts.Provider)
	if err != nil {
		return nil, err
	}
	if err := validateBatchReference(batchAPI, opts.Batch); err != nil {
		return nil, err
	}

	canceller, ok := batchAPI.(provider.BatchV4Canceller)
	if !ok {
		return nil, &providererrors.UnsupportedFunctionalityError{
			Functionality: "batch cancellation",
			Message:       "The provider does not support batch cancellation.",
		}
	}

	callCtx, cancel := withOptionalTimeout(ctx, opts.Timeout)
	defer cancel()

	result, err := canceller.DoCancelBatch(callCtx, provider.BatchV4OperationOptions{
		BatchID:         opts.Batch.ID,
		ProviderOptions: opts.ProviderOptions,
		Headers:         version.WithUserAgentSuffix(opts.Headers, version.UserAgent()),
	})
	if err != nil {
		return nil, err
	}
	return &CancelBatchResult{ProviderMetadata: result.ProviderMetadata}, nil
}

// ListBatchesOptions are options for listing batches.
type ListBatchesOptions struct {
	Provider        interface{}
	ProviderOptions map[string]interface{}
	// Limit is optional (nil means unset), mirroring TypeScript's
	// `limit?: number`. An explicit 0 is forwarded to the provider rather
	// than treated as "not set".
	Limit      *int
	Cursor     string
	MaxRetries *int
	Headers    map[string]string
	Timeout    *time.Duration
}

// ListBatchesResult is one page of listed batches.
type ListBatchesResult struct {
	Batches          []Batch
	NextCursor       string
	ProviderMetadata map[string]interface{}
}

// ExperimentalListBatches lists a page of batches. Experimental: may change
// in patch releases.
func ExperimentalListBatches(ctx context.Context, opts ListBatchesOptions) (*ListBatchesResult, error) {
	batchAPI, err := resolveBatchAPI(opts.Provider)
	if err != nil {
		return nil, err
	}

	lister, ok := batchAPI.(provider.BatchV4Lister)
	if !ok {
		return nil, &providererrors.UnsupportedFunctionalityError{
			Functionality: "batch listing",
			Message:       "The provider does not support listing batches.",
		}
	}
	if err := validateMaxRetries(opts.MaxRetries); err != nil {
		return nil, err
	}

	callCtx, cancel := withOptionalTimeout(ctx, opts.Timeout)
	defer cancel()

	headers := version.WithUserAgentSuffix(opts.Headers, version.UserAgent())
	maxRetries := preparedMaxRetries(opts.MaxRetries)

	var result *provider.BatchV4ListResult
	err = withEmbedRetry(callCtx, maxRetries, func(retryCtx context.Context) error {
		r, callErr := lister.DoListBatches(retryCtx, provider.BatchV4ListOptions{
			ProviderOptions: opts.ProviderOptions,
			Headers:         headers,
			Limit:           opts.Limit,
			Cursor:          opts.Cursor,
		})
		if callErr != nil {
			return callErr
		}
		result = r
		return nil
	})
	if err != nil {
		return nil, err
	}

	batches := make([]Batch, 0, len(result.Batches))
	for _, item := range result.Batches {
		batches = append(batches, Batch{
			BatchReference: BatchReference{Version: 2, ID: item.BatchID, Provider: batchAPI.Provider()},
			BatchV4Status:  item.BatchV4Status,
		})
	}
	return &ListBatchesResult{
		Batches:          batches,
		NextCursor:       result.NextCursor,
		ProviderMetadata: result.ProviderMetadata,
	}, nil
}

// resolveBatchAPI resolves p (nil, a provider.BatchV4, or a
// provider.BatchProvider) to a provider.BatchV4. nil falls back to the AI
// Gateway, mirroring TypeScript's
// `globalThis.AI_SDK_DEFAULT_PROVIDER ?? gateway`.
func resolveBatchAPI(p interface{}) (provider.BatchV4, error) {
	switch v := p.(type) {
	case nil:
		gw, err := gateway.Gateway()
		if err != nil {
			return nil, fmt.Errorf("failed to resolve the default AI Gateway provider for batch processing: %w", err)
		}
		return gw.ExperimentalBatch(), nil
	case provider.BatchV4:
		return v, nil
	case provider.BatchProvider:
		return v.ExperimentalBatch(), nil
	default:
		return nil, &providererrors.UnsupportedFunctionalityError{
			Functionality: "batch processing",
			Message:       "The provider does not support batch processing. Make sure it exposes an ExperimentalBatch() method.",
		}
	}
}

func validateBatchRequests(requests []BatchRequest) error {
	if len(requests) == 0 {
		return &providererrors.InvalidArgumentError{
			Field:   "requests",
			Message: "requests must not be empty",
		}
	}

	seen := make(map[string]struct{}, len(requests))
	for _, req := range requests {
		if _, err := req.requestType(); err != nil {
			return err
		}
		id := req.id()
		if strings.TrimSpace(id) == "" {
			return &providererrors.InvalidArgumentError{
				Field:   "requests",
				Message: "request IDs must not be empty",
			}
		}
		if _, ok := seen[id]; ok {
			return &providererrors.InvalidArgumentError{
				Field:   "requests",
				Message: fmt.Sprintf("request IDs must be unique; duplicate ID %q", id),
			}
		}
		seen[id] = struct{}{}
	}
	return nil
}

// validateBatchToolCompatibility rejects batches where the same tool name is
// given different definitions across requests. Mirrors TypeScript's
// validateCompatibleTools (compares definitions, not the Execute closures).
func validateBatchToolCompatibility(requests []BatchRequest) error {
	byName := make(map[string]types.Tool)
	for _, req := range requests {
		if req.Text == nil {
			continue
		}
		for _, tool := range req.Text.Tools {
			if prev, ok := byName[tool.Name]; ok {
				if !toolDefinitionsEqual(prev, tool) {
					return &providererrors.InvalidArgumentError{
						Field:   "requests",
						Message: fmt.Sprintf("tool %q must have the same definition in every batch request", tool.Name),
					}
				}
				continue
			}
			byName[tool.Name] = tool
		}
	}
	return nil
}

// toolDefinitionsEqual compares the provider-facing definition of two tools,
// ignoring Execute/approval/context closures that cannot be meaningfully
// compared with reflect.DeepEqual.
func toolDefinitionsEqual(a, b types.Tool) bool {
	return a.Name == b.Name &&
		a.Description == b.Description &&
		a.Title == b.Title &&
		a.Strict == b.Strict &&
		a.ProviderExecuted == b.ProviderExecuted &&
		reflect.DeepEqual(a.Parameters, b.Parameters) &&
		reflect.DeepEqual(a.OutputSchema, b.OutputSchema)
}

func validateBatchReference(batchAPI provider.BatchV4, batch BatchReference) error {
	if batch.Version != 2 {
		return &providererrors.InvalidArgumentError{
			Field:   "batch",
			Message: "batch must be a supported batch reference",
		}
	}
	if batch.Provider != batchAPI.Provider() {
		return &providererrors.InvalidArgumentError{
			Field:   "provider",
			Message: fmt.Sprintf("provider %s is not compatible with batch provider %s", batchAPI.Provider(), batch.Provider),
		}
	}
	return nil
}

func buildBatchProviderRequest(ctx context.Context, req BatchRequest, urlChecker promptutils.URLSupportChecker) (provider.BatchV4Request, error) {
	switch {
	case req.Text != nil:
		t := req.Text
		// Mirrors TypeScript's `convertToLanguageModelPrompt({ prompt: standardizedPrompt,
		// supportedUrls, download: undefined, ... })` in packages/ai/src/batch/batch.ts:
		// batch prompts go through the same download-normalization path as
		// generateText/streamText (using the SDK's default download function,
		// since batch does not expose an experimental_download option), so
		// remote file URLs the batch interface can't consume directly are
		// inlined before the request is sent.
		normalizedPrompt, normErr := promptutils.NormalizePromptWithDownloadSupport(
			ctx,
			buildPrompt(t.Prompt, t.Messages, t.System),
			false,
			DefaultDownload,
			urlChecker,
		)
		if normErr != nil {
			return provider.BatchV4Request{}, normErr
		}
		// Mirrors TypeScript's `prepareTools({ tools, toolOrder, toolsContext })`
		// in packages/ai/src/batch/batch.ts: dynamic tool descriptions are
		// resolved via toolsContext, then tools are reordered per toolOrder.
		resolvedTools := resolveStepTools(ctx, t.Tools, t.ToolsContext, nil)
		resolvedTools = orderStepTools(resolvedTools, t.ToolOrder)
		genOpts := provider.GenerateOptions{
			Prompt:           normalizedPrompt,
			Temperature:      t.Temperature,
			MaxTokens:        t.MaxTokens,
			TopP:             t.TopP,
			TopK:             t.TopK,
			FrequencyPenalty: t.FrequencyPenalty,
			PresencePenalty:  t.PresencePenalty,
			StopSequences:    t.StopSequences,
			Seed:             t.Seed,
			Tools:            resolvedTools,
			ToolChoice:       t.ToolChoice,
			Reasoning:        t.Reasoning,
			ResponseFormat:   t.ResponseFormat,
			ProviderOptions:  t.ProviderOptions,
		}
		return provider.BatchV4Request{
			Type: provider.BatchRequestTypeText,
			Text: &provider.TextBatchV4Request{ID: t.ID, ModelID: t.Model, Options: genOpts},
		}, nil
	case req.Image != nil:
		i := req.Image
		imgOpts := provider.ImageGenerateOptions{
			Prompt:          i.Prompt,
			N:               i.N,
			Size:            i.Size,
			AspectRatio:     i.AspectRatio,
			Seed:            i.Seed,
			Files:           i.Files,
			Mask:            i.Mask,
			ProviderOptions: i.ProviderOptions,
		}
		return provider.BatchV4Request{
			Type:  provider.BatchRequestTypeImage,
			Image: &provider.ImageBatchV4Request{ID: i.ID, ModelID: i.Model, Options: imgOpts},
		}, nil
	default:
		return provider.BatchV4Request{}, &providererrors.InvalidArgumentError{
			Field:   "requests",
			Message: "each request must set exactly one of Text or Image",
		}
	}
}

// withOptionalTimeout derives a child context bounded by timeout when
// non-nil. The returned cancel func is always safe to call (and must be
// called, typically via defer, to release resources even when timeout is
// nil).
func withOptionalTimeout(ctx context.Context, timeout *time.Duration) (context.Context, context.CancelFunc) {
	if timeout == nil {
		return ctx, func() {}
	}
	return context.WithTimeout(ctx, *timeout)
}
