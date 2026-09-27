package xai

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	stdhttp "net/http"
	"net/textproto"
	"net/url"
	"strconv"
	"strings"
	"time"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// xaiBatchEndpoint is the sub-request URL embedded in each JSONL line for a
// text batch request, mirroring TS xaiBatchEndpoint.
const xaiBatchEndpoint = "/v1/responses"

// xaiBatchName is the fixed batch job name xAI expects, mirroring TS
// xaiBatchName.
const xaiBatchName = "ai-sdk-text-batch"

// xaiBatchResultsPageSize is the page size used when paginating batch
// results, mirroring TS xaiBatchResultsPageSize.
const xaiBatchResultsPageSize = 1000

// Batch implements provider.BatchV4 (plus BatchV4Canceller/BatchV4Lister) for
// the xAI Batch API. Mirrors TypeScript's XaiBatch
// (packages/xai/src/xai-batch.ts). Text requests are submitted through the
// Responses API endpoint; image requests through the image generation/edit
// endpoints. Both are bundled into a single JSONL input file uploaded via
// xAI's Files API, then referenced by a created batch job.
type Batch struct {
	provider *Provider
}

// NewBatch creates a new xAI batch interface.
func NewBatch(p *Provider) *Batch { return &Batch{provider: p} }

// ExperimentalBatch implements provider.BatchProvider for Provider, exposing
// the xAI Batch API. Mirrors TS `provider.experimental_batch = createBatch`.
func (p *Provider) ExperimentalBatch() provider.BatchV4 { return NewBatch(p) }

// SpecificationVersion returns the batch interface specification version.
func (b *Batch) SpecificationVersion() string { return "v4" }

// Provider returns the batch interface's provider ID, e.g. "xai.batch".
func (b *Batch) Provider() string { return b.provider.Name() + ".batch" }

// xaiResponsesSupportedURLs mirrors TS xaiResponsesSupportedUrls: xAI's
// Responses API accepts these URLs directly (images, PDFs, and other
// documents as `input_file`/`file_url`) instead of requiring the SDK to
// download them to bytes first.
var xaiResponsesSupportedURLs = map[string][]string{
	"image/*":         {`^https?://.*$`},
	"application/pdf": {`^https?://.*$`},
	"text/*":          {`^https?://.*$`},
}

// SupportedURLs returns the same URL patterns the Responses API accepts
// directly, mirroring TS `supportedUrls: xaiResponsesSupportedUrls`.
func (b *Batch) SupportedURLs() map[string][]string {
	return xaiResponsesSupportedURLs
}

// assertXAISupportedBatchRequests mirrors TS assertSupportedBatchRequests:
// only text and image batch requests are supported.
func assertXAISupportedBatchRequests(requests []provider.BatchV4Request) error {
	for _, req := range requests {
		if req.Type != provider.BatchRequestTypeText && req.Type != provider.BatchRequestTypeImage {
			return &providererrors.UnsupportedFunctionalityError{
				Functionality: fmt.Sprintf("batch request type: %s", req.Type),
				Message:       fmt.Sprintf("The xAI Batch API does not support batch requests with type %q.", req.Type),
			}
		}
	}
	return nil
}

// xaiBatchProviderOptions mirrors TS xaiBatchProviderOptionsSchema.
type xaiBatchProviderOptions struct {
	InputFileExpiresAfter *int `json:"inputFileExpiresAfter,omitempty"`
}

func extractXAIBatchProviderOptions(providerOptions map[string]interface{}) (xaiBatchProviderOptions, error) {
	var opts xaiBatchProviderOptions
	raw, ok := providerOptions["xai"]
	if !ok || raw == nil {
		return opts, nil
	}
	b, err := json.Marshal(raw)
	if err != nil {
		return opts, invalidXAIProviderOptions(err)
	}
	if err := json.Unmarshal(b, &opts); err != nil {
		return opts, invalidXAIProviderOptions(err)
	}
	if opts.InputFileExpiresAfter != nil && (*opts.InputFileExpiresAfter < 3600 || *opts.InputFileExpiresAfter > 2_592_000) {
		return opts, invalidXAIProviderOptions(fmt.Errorf("inputFileExpiresAfter must be between 3600 and 2592000"))
	}
	return opts, nil
}

// DoStartBatch uploads a JSONL input file describing every request (text via
// the Responses endpoint, image via the image generation/edit endpoints) and
// creates a batch job referencing it. Mirrors TS XaiBatch#doStartBatch.
func (b *Batch) DoStartBatch(ctx context.Context, opts provider.BatchV4StartOptions) (*provider.BatchV4StartResult, error) {
	if err := assertXAISupportedBatchRequests(opts.Requests); err != nil {
		return nil, err
	}

	var warnings []provider.BatchV4Warning
	if opts.WebhookURL != "" {
		warnings = append(warnings, provider.BatchV4Warning{
			Warning: types.Warning{
				Type:    "unsupported",
				Feature: "webhookUrl",
				Details: "The xAI Batch API does not support per-batch webhook URLs.",
			},
		})
	}

	batchOptions, err := extractXAIBatchProviderOptions(opts.ProviderOptions)
	if err != nil {
		return nil, err
	}

	var jsonl bytes.Buffer
	for _, req := range opts.Requests {
		endpoint, body, reqWarnings, err := b.prepareBatchRequest(req)
		if err != nil {
			return nil, err
		}
		line, err := json.Marshal(map[string]interface{}{
			"custom_id": req.RequestID(),
			"method":    "POST",
			"url":       endpoint,
			"body":      body,
		})
		if err != nil {
			return nil, err
		}
		jsonl.Write(line)
		jsonl.WriteByte('\n')
		for _, w := range reqWarnings {
			warnings = append(warnings, provider.BatchV4Warning{RequestID: req.RequestID(), Warning: w})
		}
	}

	headers := internalhttp.MergeHeaders(opts.Headers)

	var multipartBody bytes.Buffer
	writer := multipart.NewWriter(&multipartBody)
	// xAI rejects uploads where expires_after arrives after the file part,
	// so all fields precede the file.
	if batchOptions.InputFileExpiresAfter != nil {
		if err := writer.WriteField("expires_after", strconv.Itoa(*batchOptions.InputFileExpiresAfter)); err != nil {
			return nil, err
		}
	}
	// CreateFormFile hardcodes Content-Type: application/octet-stream; TS
	// uploads the JSONL body as `new Blob(fileParts, {type:
	// 'application/jsonl'})`, so the part header is built manually to match.
	fileHeader := textproto.MIMEHeader{}
	fileHeader.Set("Content-Disposition", `form-data; name="file"; filename="batch.jsonl"`)
	fileHeader.Set("Content-Type", "application/jsonl")
	part, err := writer.CreatePart(fileHeader)
	if err != nil {
		return nil, err
	}
	if _, err := part.Write(jsonl.Bytes()); err != nil {
		return nil, err
	}
	if err := writer.Close(); err != nil {
		return nil, err
	}

	uploadResp, err := b.provider.client.Do(ctx, internalhttp.Request{
		Method: stdhttp.MethodPost,
		Path:   "/files",
		Body:   &multipartBody,
		Headers: internalhttp.MergeHeaders(headers, map[string]string{
			"Content-Type": writer.FormDataContentType(),
		}),
	})
	if err != nil {
		return nil, b.handleError(err)
	}
	if uploadResp.StatusCode >= 400 {
		return nil, newXAIProviderError(b.Provider(), uploadResp.StatusCode, uploadResp.Body)
	}
	var uploadedFile xaiFileResponse
	if err := json.Unmarshal(uploadResp.Body, &uploadedFile); err != nil {
		return nil, err
	}

	var createResp xaiBatchResponseWire
	if err := b.provider.client.DoJSON(ctx, internalhttp.Request{
		Method:  stdhttp.MethodPost,
		Path:    "/batches",
		Body:    map[string]interface{}{"name": xaiBatchName, "input_file_id": uploadedFile.ID},
		Headers: headers,
	}, &createResp); err != nil {
		return nil, b.handleError(err)
	}

	status := convertXAIBatchStatus(createResp)
	meta := map[string]interface{}{"inputFileId": uploadedFile.ID}
	if uploadedFile.ExpiresAt != nil {
		meta["inputFileExpiresAt"] = time.Unix(*uploadedFile.ExpiresAt, 0).UTC().Format("2006-01-02T15:04:05.000Z")
	}
	status.ProviderMetadata = map[string]interface{}{"xai": meta}

	return &provider.BatchV4StartResult{
		BatchV4Status: status,
		BatchID:       createResp.BatchID,
		Warnings:      warnings,
	}, nil
}

// prepareBatchRequest builds the sub-request endpoint and body for one batch
// request, mirroring TS XaiBatch#prepareRequest.
func (b *Batch) prepareBatchRequest(req provider.BatchV4Request) (endpoint string, body map[string]interface{}, warnings []types.Warning, err error) {
	if req.Type == provider.BatchRequestTypeText {
		lm := NewResponsesLanguageModel(b.provider, req.Text.ModelID)
		reqBody, reqWarnings, err := lm.buildRequestBody(&req.Text.Options, false)
		if err != nil {
			return "", nil, nil, err
		}
		// The batch sub-request URL already encodes the intent (a
		// non-streaming Responses call); the wire body itself carries no
		// `stream` key in TS.
		delete(reqBody, "stream")
		return xaiBatchEndpoint, reqBody, reqWarnings, nil
	}

	imgOpts := req.Image.Options
	im := NewImageModel(b.provider, req.Image.ModelID)
	provOpts, err := extractImageProviderOptions(imgOpts.ProviderOptions)
	if err != nil {
		return "", nil, nil, err
	}
	warnings = im.checkUnsupportedOptions(&imgOpts)
	hasFiles := len(imgOpts.Files) > 0
	reqBody := im.buildRequestBody(&imgOpts, provOpts, hasFiles)

	endpoint = "/v1/images/generations"
	if hasFiles {
		endpoint = "/v1/images/edits"
	}
	return endpoint, reqBody, warnings, nil
}

// DoGetBatchStatus retrieves a batch's lifecycle status
// (`GET /batches/{id}`). Mirrors TS XaiBatch#doGetBatchStatus.
func (b *Batch) DoGetBatchStatus(ctx context.Context, opts provider.BatchV4OperationOptions) (*provider.BatchV4Status, error) {
	resp, err := b.retrieveBatch(ctx, opts.BatchID, opts.Headers)
	if err != nil {
		return nil, err
	}
	status := convertXAIBatchStatus(*resp)
	return &status, nil
}

// DoCancelBatch requests cancellation of a batch
// (`POST /batches/{id}:cancel`). Mirrors TS XaiBatch#doCancelBatch.
func (b *Batch) DoCancelBatch(ctx context.Context, opts provider.BatchV4OperationOptions) (*provider.BatchV4CancelResult, error) {
	var resp xaiBatchResponseWire
	if err := b.provider.client.DoJSON(ctx, internalhttp.Request{
		Method:  stdhttp.MethodPost,
		Path:    "/batches/" + providerutils.EncodePathSegment(opts.BatchID) + ":cancel",
		Body:    map[string]interface{}{},
		Headers: opts.Headers,
	}, &resp); err != nil {
		return nil, b.handleError(err)
	}
	return &provider.BatchV4CancelResult{}, nil
}

// DoListBatches lists batches (`GET /batches`). Mirrors TS
// XaiBatch#doListBatches.
func (b *Batch) DoListBatches(ctx context.Context, opts provider.BatchV4ListOptions) (*provider.BatchV4ListResult, error) {
	query := map[string]string{}
	if opts.Limit != nil {
		query["limit"] = strconv.Itoa(*opts.Limit)
	}
	if opts.Cursor != "" {
		// Pre-escaped: internalhttp.Client.Do concatenates query values
		// verbatim without percent-encoding them.
		query["pagination_token"] = url.QueryEscape(opts.Cursor)
	}

	var page xaiBatchListResponseWire
	if err := b.provider.client.DoJSON(ctx, internalhttp.Request{
		Method:  stdhttp.MethodGet,
		Path:    "/batches",
		Query:   query,
		Headers: opts.Headers,
	}, &page); err != nil {
		return nil, b.handleError(err)
	}

	batches := make([]provider.BatchV4ListItem, 0, len(page.Batches))
	for _, item := range page.Batches {
		batches = append(batches, provider.BatchV4ListItem{BatchV4Status: convertXAIBatchStatus(item), BatchID: item.BatchID})
	}

	result := &provider.BatchV4ListResult{Batches: batches}
	if page.PaginationToken != nil && *page.PaginationToken != "" {
		result.NextCursor = *page.PaginationToken
	}
	return result, nil
}

// DoGetBatchResults streams the per-request results of a terminal batch by
// paginating `GET /batches/{id}/results`. Mirrors TS
// XaiBatch#doGetBatchResults / iterateBatchResults.
func (b *Batch) DoGetBatchResults(ctx context.Context, opts provider.BatchV4OperationOptions) (provider.BatchV4ItemResultStream, error) {
	resp, err := b.retrieveBatch(ctx, opts.BatchID, opts.Headers)
	if err != nil {
		return nil, err
	}
	if convertXAIBatchStatus(*resp).Status == provider.BatchStatusPending {
		return nil, &providererrors.InvalidArgumentError{
			Field:   "batchId",
			Message: fmt.Sprintf("xAI batch %q is not complete.", opts.BatchID),
		}
	}
	return newXAIBatchResultsStream(ctx, b, opts.BatchID, opts.Headers), nil
}

// retrieveBatch fetches one batch's current representation
// (`GET /batches/{id}`).
func (b *Batch) retrieveBatch(ctx context.Context, batchID string, headers map[string]string) (*xaiBatchResponseWire, error) {
	var resp xaiBatchResponseWire
	if err := b.provider.client.DoJSON(ctx, internalhttp.Request{
		Method:  stdhttp.MethodGet,
		Path:    "/batches/" + providerutils.EncodePathSegment(batchID),
		Headers: headers,
	}, &resp); err != nil {
		return nil, b.handleError(err)
	}
	return &resp, nil
}

func (b *Batch) handleError(err error) error {
	var statusErr *internalhttp.HTTPStatusError
	if errors.As(err, &statusErr) {
		return newXAIProviderError(b.Provider(), statusErr.StatusCode, statusErr.Body)
	}
	return err
}

// --- Wire types and status conversion ----------------------------------

type xaiBatchStateWire struct {
	NumRequests  *int `json:"num_requests,omitempty"`
	NumPending   *int `json:"num_pending,omitempty"`
	NumSuccess   *int `json:"num_success,omitempty"`
	NumError     *int `json:"num_error,omitempty"`
	NumCancelled *int `json:"num_cancelled,omitempty"`
}

type xaiBatchResponseWire struct {
	BatchID            string             `json:"batch_id"`
	Name               string             `json:"name,omitempty"`
	CreateTime         string             `json:"create_time,omitempty"`
	ExpireTime         string             `json:"expire_time,omitempty"`
	CancelTime         *string            `json:"cancel_time,omitempty"`
	CancelByXAIMessage *string            `json:"cancel_by_xai_message,omitempty"`
	State              *xaiBatchStateWire `json:"state,omitempty"`
}

type xaiBatchListResponseWire struct {
	Batches         []xaiBatchResponseWire `json:"batches"`
	PaginationToken *string                `json:"pagination_token,omitempty"`
}

// convertXAIBatchStatus mirrors TS convertXaiBatchStatus. It never sets
// ProviderMetadata — DoStartBatch adds that separately, matching TS (only
// doStartBatch's result carries providerMetadata.xai.inputFileId).
func convertXAIBatchStatus(b xaiBatchResponseWire) provider.BatchV4Status {
	var total, pending, completed, failed *int
	if b.State != nil {
		total = b.State.NumRequests
		pending = b.State.NumPending
		completed = b.State.NumSuccess
		if b.State.NumError != nil && b.State.NumCancelled != nil {
			f := *b.State.NumError + *b.State.NumCancelled
			failed = &f
		}
	}
	counts := providerutils.NormalizeBatchRequestCounts(total, pending, completed, failed)

	isCancelled := (b.CancelTime != nil && *b.CancelTime != "") || (b.CancelByXAIMessage != nil && *b.CancelByXAIMessage != "")
	isExpired := xaiIsPastDate(b.ExpireTime)

	var status provider.BatchStatusValue
	switch {
	case isCancelled || isExpired:
		status = provider.BatchStatusFailed
	case counts != nil && counts.Total > 0 && counts.Pending == 0:
		status = provider.BatchStatusCompleted
	default:
		status = provider.BatchStatusPending
	}

	result := provider.BatchV4Status{
		Status:        status,
		RequestCounts: counts,
		CreatedAt:     b.CreateTime,
		ExpiresAt:     b.ExpireTime,
	}
	switch {
	case isCancelled:
		msg := fmt.Sprintf("xAI batch %q was cancelled.", b.BatchID)
		if b.CancelByXAIMessage != nil && *b.CancelByXAIMessage != "" {
			msg = *b.CancelByXAIMessage
		}
		result.Error = &provider.BatchError{Message: msg, Code: "batch_cancelled"}
	case isExpired:
		result.Error = &provider.BatchError{Message: fmt.Sprintf("xAI batch %q expired.", b.BatchID), Code: "batch_expired"}
	}
	return result
}

// xaiIsPastDate mirrors TS isPastDate.
func xaiIsPastDate(value string) bool {
	if value == "" {
		return false
	}
	t, err := time.Parse(time.RFC3339, value)
	if err != nil {
		return false
	}
	return !t.After(time.Now())
}

// --- Batch results streaming --------------------------------------------

type xaiBatchErrorWire struct {
	Code    interface{} `json:"code,omitempty"`
	Message string      `json:"message,omitempty"`
}

type xaiBatchResultWire struct {
	BatchRequestID string `json:"batch_request_id"`
	BatchResult    *struct {
		Response *struct {
			ChatGetCompletion json.RawMessage `json:"chat_get_completion,omitempty"`
			ImageGeneration   json.RawMessage `json:"image_generation,omitempty"`
		} `json:"response,omitempty"`
		Error *xaiBatchErrorWire `json:"error,omitempty"`
	} `json:"batch_result,omitempty"`
	ErrorMessage string `json:"error_message,omitempty"`
}

type xaiBatchResultsPageWire struct {
	Results         []xaiBatchResultWire `json:"results"`
	PaginationToken *string              `json:"pagination_token,omitempty"`
}

// xaiBatchResultsStream implements provider.BatchV4ItemResultStream over
// paginated `GET /batches/{id}/results` pages, mirroring TS
// XaiBatch#iterateBatchResults.
type xaiBatchResultsStream struct {
	ctx     context.Context
	batch   *Batch
	batchID string
	headers map[string]string

	buffer    []xaiBatchResultWire
	idx       int
	nextToken *string
	started   bool
	done      bool
	err       error
}

func newXAIBatchResultsStream(ctx context.Context, b *Batch, batchID string, headers map[string]string) *xaiBatchResultsStream {
	return &xaiBatchResultsStream{ctx: ctx, batch: b, batchID: batchID, headers: headers}
}

func (s *xaiBatchResultsStream) Next() (*provider.BatchV4ItemResult, error) {
	for {
		if s.idx < len(s.buffer) {
			item := s.buffer[s.idx]
			s.idx++
			return s.batch.convertBatchResult(s.ctx, item), nil
		}
		if s.done {
			return nil, io.EOF
		}
		if err := s.fetchPage(); err != nil {
			s.err = err
			return nil, err
		}
	}
}

func (s *xaiBatchResultsStream) fetchPage() error {
	s.started = true
	query := map[string]string{"limit": strconv.Itoa(xaiBatchResultsPageSize)}
	if s.nextToken != nil {
		// Pre-escaped: internalhttp.Client.Do concatenates query values
		// verbatim without percent-encoding them.
		query["pagination_token"] = url.QueryEscape(*s.nextToken)
	}

	var page xaiBatchResultsPageWire
	if err := s.batch.provider.client.DoJSON(s.ctx, internalhttp.Request{
		Method:  stdhttp.MethodGet,
		Path:    "/batches/" + providerutils.EncodePathSegment(s.batchID) + "/results",
		Query:   query,
		Headers: s.headers,
	}, &page); err != nil {
		return s.batch.handleError(err)
	}

	s.buffer = page.Results
	s.idx = 0
	if page.PaginationToken != nil && *page.PaginationToken != "" {
		s.nextToken = page.PaginationToken
	} else {
		s.nextToken = nil
		s.done = true
	}
	return nil
}

func (s *xaiBatchResultsStream) Err() error { return s.err }

func (s *xaiBatchResultsStream) Close() error { return nil }

// convertBatchResult mirrors TS XaiBatch#convertBatchResult.
func (b *Batch) convertBatchResult(ctx context.Context, wire xaiBatchResultWire) *provider.BatchV4ItemResult {
	var errWire *xaiBatchErrorWire
	if wire.BatchResult != nil {
		errWire = wire.BatchResult.Error
	}
	if xaiBatchResultHasError(wire.ErrorMessage, errWire) {
		status := provider.BatchItemFailed
		if errWire != nil && isXAICancellationError(errWire.Code) {
			status = provider.BatchItemCancelled
		}
		return &provider.BatchV4ItemResult{
			Type:   provider.BatchRequestTypeText,
			ID:     wire.BatchRequestID,
			Status: status,
			Error:  convertXAIBatchError(wire.ErrorMessage, errWire),
		}
	}

	var response *struct {
		ChatGetCompletion json.RawMessage `json:"chat_get_completion,omitempty"`
		ImageGeneration   json.RawMessage `json:"image_generation,omitempty"`
	}
	if wire.BatchResult != nil {
		response = wire.BatchResult.Response
	}
	if response == nil {
		return invalidXAIBatchTextResult(wire.BatchRequestID)
	}

	if isPresentRawJSON(response.ChatGetCompletion) {
		var textResp xaiBatchTextResponseWire
		if err := json.Unmarshal(response.ChatGetCompletion, &textResp); err != nil {
			return invalidXAIBatchTextResult(wire.BatchRequestID)
		}
		genResult, convErr := convertXAIBatchTextResponse(textResp)
		if convErr != nil {
			return &provider.BatchV4ItemResult{Type: provider.BatchRequestTypeText, ID: wire.BatchRequestID, Status: provider.BatchItemFailed, Error: convErr}
		}
		return &provider.BatchV4ItemResult{Type: provider.BatchRequestTypeText, ID: wire.BatchRequestID, Status: provider.BatchItemSucceeded, TextResult: genResult}
	}

	if isPresentRawJSON(response.ImageGeneration) {
		var imgResp xaiImageResponse
		if err := json.Unmarshal(response.ImageGeneration, &imgResp); err != nil {
			return invalidXAIBatchImageResult(wire.BatchRequestID)
		}
		for _, d := range imgResp.Data {
			if d.RespectModeration != nil && !*d.RespectModeration {
				return &provider.BatchV4ItemResult{
					Type:   provider.BatchRequestTypeImage,
					ID:     wire.BatchRequestID,
					Status: provider.BatchItemFailed,
					Error:  &provider.BatchError{Message: "Image generation was blocked due to a content policy violation."},
				}
			}
		}
		imgResult, err := b.convertImageBatchResponse(ctx, imgResp)
		if err != nil {
			return invalidXAIBatchImageResult(wire.BatchRequestID)
		}
		return &provider.BatchV4ItemResult{Type: provider.BatchRequestTypeImage, ID: wire.BatchRequestID, Status: provider.BatchItemSucceeded, ImageResult: imgResult}
	}

	return invalidXAIBatchTextResult(wire.BatchRequestID)
}

func isPresentRawJSON(raw json.RawMessage) bool {
	trimmed := bytes.TrimSpace(raw)
	return len(trimmed) > 0 && string(trimmed) != "null"
}

// xaiBatchResultHasError mirrors TS's error-presence check in
// convertBatchResult:
//
//	(result.error_message?.length ?? 0) > 0 ||
//	(error?.code != null && error.code !== 0 && error.code !== '0') ||
//	(error?.code == null && (error?.message?.length ?? 0) > 0)
func xaiBatchResultHasError(errorMessage string, errWire *xaiBatchErrorWire) bool {
	if errorMessage != "" {
		return true
	}
	if errWire == nil {
		return false
	}
	if errWire.Code != nil {
		return !isZeroXAIBatchErrorCode(errWire.Code)
	}
	return errWire.Message != ""
}

func isZeroXAIBatchErrorCode(code interface{}) bool {
	switch v := code.(type) {
	case float64:
		return v == 0
	case string:
		return v == "0"
	default:
		return false
	}
}

// isXAICancellationError mirrors TS isXaiCancellationError.
func isXAICancellationError(code interface{}) bool {
	switch v := code.(type) {
	case float64:
		return v == 1
	case string:
		lower := strings.ToLower(v)
		return lower == "1" || lower == "cancelled" || lower == "batch_cancelled"
	default:
		return false
	}
}

// convertXAIBatchError mirrors TS convertXaiBatchError.
func convertXAIBatchError(errorMessage string, errWire *xaiBatchErrorWire) *provider.BatchError {
	message := errorMessage
	if message == "" && errWire != nil {
		message = errWire.Message
	}
	if message == "" {
		message = "xAI batch request failed."
	}
	result := &provider.BatchError{Message: message}
	if errWire != nil && errWire.Code != nil && !isZeroXAIBatchErrorCode(errWire.Code) {
		switch v := errWire.Code.(type) {
		case float64:
			result.Code = strconv.FormatFloat(v, 'f', -1, 64)
		case string:
			result.Code = v
		default:
			result.Code = fmt.Sprintf("%v", v)
		}
	}
	return result
}

func invalidXAIBatchTextResult(id string) *provider.BatchV4ItemResult {
	return &provider.BatchV4ItemResult{
		Type:   provider.BatchRequestTypeText,
		ID:     id,
		Status: provider.BatchItemFailed,
		Error:  &provider.BatchError{Message: "xAI returned an invalid Responses batch result.", Code: "invalid_response"},
	}
}

func invalidXAIBatchImageResult(id string) *provider.BatchV4ItemResult {
	return &provider.BatchV4ItemResult{
		Type:   provider.BatchRequestTypeImage,
		ID:     id,
		Status: provider.BatchItemFailed,
		Error:  &provider.BatchError{Message: "xAI returned an invalid image batch result.", Code: "invalid_response"},
	}
}

// --- Text batch result conversion (xAI returns batch text results in Chat
// Completions API shape even though the request was submitted to the
// Responses endpoint) --------------------------------------------------

type xaiBatchToolCallWire struct {
	ID       string `json:"id"`
	Type     string `json:"type"`
	Function struct {
		Name      string `json:"name"`
		Arguments string `json:"arguments"`
	} `json:"function"`
}

type xaiBatchChoiceMessageWire struct {
	Role             string                 `json:"role"`
	Content          *string                `json:"content"`
	ReasoningContent *string                `json:"reasoning_content"`
	ToolCalls        []xaiBatchToolCallWire `json:"tool_calls"`
}

type xaiBatchChoiceWire struct {
	Message      xaiBatchChoiceMessageWire `json:"message"`
	Index        int                       `json:"index"`
	FinishReason string                    `json:"finish_reason"`
}

type xaiBatchUsageDetailsWire struct {
	CachedTokens    *int64 `json:"cached_tokens,omitempty"`
	ReasoningTokens *int64 `json:"reasoning_tokens,omitempty"`
}

type xaiBatchTextUsageWire struct {
	PromptTokens            int64                     `json:"prompt_tokens"`
	CompletionTokens        int64                     `json:"completion_tokens"`
	TotalTokens             int64                     `json:"total_tokens"`
	CostInUsdTicks          *int64                    `json:"cost_in_usd_ticks,omitempty"`
	PromptTokensDetails     *xaiBatchUsageDetailsWire `json:"prompt_tokens_details,omitempty"`
	CompletionTokensDetails *xaiBatchUsageDetailsWire `json:"completion_tokens_details,omitempty"`
}

type xaiBatchTextResponseWire struct {
	ID          string                 `json:"id"`
	Created     *int64                 `json:"created"`
	Model       string                 `json:"model"`
	Choices     []xaiBatchChoiceWire   `json:"choices"`
	Usage       *xaiBatchTextUsageWire `json:"usage"`
	Citations   []string               `json:"citations"`
	ServiceTier string                 `json:"service_tier"`
	Code        string                 `json:"code"`
	Error       string                 `json:"error"`
}

// mapXAIChatFinishReason mirrors TS mapXaiFinishReason (Chat Completions
// finish_reason values, distinct from the Responses API's status-based
// mapXAIResponsesFinishReason).
func mapXAIChatFinishReason(raw string) types.FinishReason {
	switch raw {
	case "stop":
		return types.FinishReasonStop
	case "length":
		return types.FinishReasonLength
	case "tool_calls", "function_call":
		return types.FinishReasonToolCalls
	case "content_filter":
		return types.FinishReasonContentFilter
	default:
		return types.FinishReasonOther
	}
}

// convertXAIBatchTextUsage mirrors TS convertXaiBatchTextUsage.
func convertXAIBatchTextUsage(u xaiBatchTextUsageWire) types.Usage {
	var cacheRead int64
	if u.PromptTokensDetails != nil && u.PromptTokensDetails.CachedTokens != nil {
		cacheRead = *u.PromptTokensDetails.CachedTokens
	}
	var reasoning int64
	if u.CompletionTokensDetails != nil && u.CompletionTokensDetails.ReasoningTokens != nil {
		reasoning = *u.CompletionTokensDetails.ReasoningTokens
	}

	promptIncludesCached := cacheRead <= u.PromptTokens
	inputTotal := u.PromptTokens + cacheRead
	noCache := u.PromptTokens
	if promptIncludesCached {
		inputTotal = u.PromptTokens
		noCache = u.PromptTokens - cacheRead
	}
	outputTotal := u.CompletionTokens + reasoning
	total := inputTotal + outputTotal
	textOut := u.CompletionTokens

	return types.Usage{
		InputTokens:  &inputTotal,
		OutputTokens: &outputTotal,
		TotalTokens:  &total,
		InputDetails: &types.InputTokenDetails{
			NoCacheTokens:   &noCache,
			CacheReadTokens: &cacheRead,
		},
		OutputDetails: &types.OutputTokenDetails{
			TextTokens:      &textOut,
			ReasoningTokens: &reasoning,
		},
	}
}

// convertXAIBatchTextResponse mirrors TS convertXaiBatchTextResponse.
func convertXAIBatchTextResponse(resp xaiBatchTextResponseWire) (*types.GenerateResult, *provider.BatchError) {
	if resp.Error != "" {
		berr := &provider.BatchError{Message: resp.Error}
		if resp.Code != "" {
			berr.Code = resp.Code
		}
		return nil, berr
	}
	if len(resp.Choices) == 0 {
		return nil, &provider.BatchError{Message: "xAI returned a batch response without any choices.", Code: "invalid_response"}
	}

	providerExecutedIDs := map[string]bool{}
	for _, choice := range resp.Choices {
		if choice.Message.Role == "tool" {
			for _, tc := range choice.Message.ToolCalls {
				providerExecutedIDs[tc.ID] = true
			}
		}
	}

	var content []types.ContentPart
	var toolCalls []types.ToolCall
	var text string
	var lastAssistant *xaiBatchChoiceWire

	for i := range resp.Choices {
		choice := resp.Choices[i]
		if choice.Message.Role == "tool" {
			if choice.Message.Content != nil {
				for _, tc := range choice.Message.ToolCalls {
					content = append(content, types.ToolResultContent{
						ToolCallID: tc.ID,
						ToolName:   tc.Function.Name,
						Result:     *choice.Message.Content,
						Dynamic:    true,
					})
				}
			}
			continue
		}

		lastAssistant = &resp.Choices[i]

		if choice.Message.Content != nil && *choice.Message.Content != "" {
			content = append(content, types.TextContent{Text: *choice.Message.Content})
			text += *choice.Message.Content
		}
		if choice.Message.ReasoningContent != nil && *choice.Message.ReasoningContent != "" {
			content = append(content, types.ReasoningContent{Text: *choice.Message.ReasoningContent})
		}
		for _, tc := range choice.Message.ToolCalls {
			var args map[string]interface{}
			_ = json.Unmarshal([]byte(tc.Function.Arguments), &args)
			call := types.ToolCall{
				ID:           tc.ID,
				ToolName:     tc.Function.Name,
				Arguments:    args,
				RawArguments: tc.Function.Arguments,
			}
			if providerExecutedIDs[tc.ID] {
				call.ProviderExecuted = true
				call.Dynamic = true
			}
			toolCalls = append(toolCalls, call)
		}
	}
	for _, u := range resp.Citations {
		content = append(content, types.SourceContent{SourceType: "url", ID: u, URL: u})
	}

	finishRaw := ""
	if lastAssistant != nil {
		finishRaw = lastAssistant.FinishReason
	}

	result := &types.GenerateResult{
		Text:         text,
		Content:      content,
		ToolCalls:    toolCalls,
		FinishReason: mapXAIChatFinishReason(finishRaw),
		Warnings:     []types.Warning{},
	}
	if resp.Usage != nil {
		result.Usage = convertXAIBatchTextUsage(*resp.Usage)
	}
	if resp.ID != "" || resp.Model != "" || resp.Created != nil {
		rm := &types.ResponseMetadata{ID: resp.ID, ModelID: resp.Model}
		if resp.Created != nil {
			rm.Timestamp = time.Unix(*resp.Created, 0).UTC()
		}
		result.ResponseMetadata = rm
	}
	if (resp.Usage != nil && resp.Usage.CostInUsdTicks != nil) || resp.ServiceTier != "" {
		meta := map[string]interface{}{}
		if resp.Usage != nil && resp.Usage.CostInUsdTicks != nil {
			meta["costInUsdTicks"] = *resp.Usage.CostInUsdTicks
		}
		if resp.ServiceTier != "" {
			meta["serviceTier"] = resp.ServiceTier
		}
		result.ProviderMetadata = map[string]interface{}{"xai": meta}
	}
	return result, nil
}

// --- Image batch result conversion --------------------------------------
//
// The batch image wire shape is identical to the (unbatched) image model's
// response (xaiImageResponse/xaiImageData in image_model.go), so it is
// reused directly instead of being duplicated here.

// convertImageBatchResponse mirrors TS XaiBatch#convertImageBatchResponse. It
// reuses ImageModel.responseImages for the same url/b64_json-fallback
// decoding (including downloading a remote URL when b64_json is absent) that
// the unbatched image model uses, so a defensive path exists even though
// batch requests always ask for b64_json (see prepareBatchRequest /
// ImageModel.buildRequestBody).
func (b *Batch) convertImageBatchResponse(ctx context.Context, resp xaiImageResponse) (*types.ImageResult, error) {
	im := NewImageModel(b.provider, "")
	images, base64Images, err := im.responseImages(ctx, resp.Data)
	if err != nil {
		return nil, err
	}

	imagesMeta := make([]XAIImageItemMetadata, len(resp.Data))
	for i, d := range resp.Data {
		imagesMeta[i] = XAIImageItemMetadata{RevisedPrompt: d.RevisedPrompt}
	}
	meta := XAIImageMetadata{Images: imagesMeta}
	if resp.Usage != nil && resp.Usage.CostInUsdTicks != nil {
		meta.CostInUsdTicks = resp.Usage.CostInUsdTicks
	}

	result := &types.ImageResult{
		MimeType:         "image/png",
		Warnings:         []types.Warning{},
		ProviderMetadata: map[string]interface{}{"xai": meta},
		Response:         &types.ResponseMetadata{Timestamp: time.Now()},
	}
	if len(images) > 0 {
		result.Image = images[0]
		result.Images = images
	}
	if len(base64Images) > 0 {
		result.Base64Image = base64Images[0]
		result.Base64Images = base64Images
	}
	return result, nil
}

var (
	_ provider.BatchProvider    = (*Provider)(nil)
	_ provider.BatchV4          = (*Batch)(nil)
	_ provider.BatchV4Canceller = (*Batch)(nil)
	_ provider.BatchV4Lister    = (*Batch)(nil)
)
