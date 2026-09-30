package google

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/gemini"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/streaming"
)

// googleBatchInputFileMaxBytes is the maximum size Google accepts for a batch
// input file (2 GB).
const googleBatchInputFileMaxBytes = 2 * 1024 * 1024 * 1024

// googleBatchInlineCreationMaxBytes is the threshold above which the batch
// input switches from an inline request body to an uploaded JSONL file.
const googleBatchInlineCreationMaxBytes = 20_000_000

// googleBatchSupportedContentTypes are the LanguageModelV4GenerateResult
// content part types a text batch result can carry. Anything else fails
// that item's conversion, matching TS supportedGoogleBatchContentTypes.
// Generated image content is handled separately (and takes priority): see
// convertGoogleBatchImageResult, called unconditionally before this check,
// mirroring TS's iterateBatchResults calling convertGoogleImageBatchResult
// before checking supportedGoogleBatchContentTypes.
var googleBatchSupportedContentTypes = map[string]bool{
	"text": true, "reasoning": true, "source": true, "tool-call": true, "tool-result": true,
}

// Batch implements provider.BatchV4 for the Gemini Batch API
// (`{model}:batchGenerateContent`, inline or file-based input). Mirrors
// TypeScript's GoogleBatch (packages/google/src/google-batch.ts). Both text
// and image requests are supported; only inline (non-GCS-file) image
// requests are implemented, matching prepareImageRequest in
// google-batch.ts.
type Batch struct {
	provider *Provider
}

// NewBatch creates a new Google batch interface.
func NewBatch(p *Provider) *Batch { return &Batch{provider: p} }

// ExperimentalBatch implements provider.BatchProvider for Provider, exposing
// the Gemini Batch API. Mirrors TypeScript's `provider.experimental_batch =
// createBatch`.
func (p *Provider) ExperimentalBatch() provider.BatchV4 { return NewBatch(p) }

// SpecificationVersion returns the batch interface specification version.
func (b *Batch) SpecificationVersion() string { return "v4" }

// Provider returns the batch interface's provider ID, e.g. "google.batch".
func (b *Batch) Provider() string {
	return strings.TrimSuffix(b.provider.Name(), ".generative-ai") + ".batch"
}

// SupportedURLs returns the URL patterns accepted directly, without a
// specific model (TS `getSupportedUrls(undefined, false)`).
func (b *Batch) SupportedURLs() map[string][]string {
	return googleSupportedURLs(b.provider.config.BaseURL, "")
}

// DoStartBatch starts a batch job (`POST {model}:batchGenerateContent`),
// using an inline request body when it fits under the 20MB threshold and an
// uploaded JSONL file otherwise.
func (b *Batch) DoStartBatch(ctx context.Context, opts provider.BatchV4StartOptions) (*provider.BatchV4StartResult, error) {
	if err := validateGoogleBatchRequests(opts.Requests); err != nil {
		return nil, err
	}
	modelID, err := getGoogleBatchModelID(opts.Requests)
	if err != nil {
		return nil, err
	}

	var warnings []provider.BatchV4Warning
	inlinedRequests := make([]map[string]interface{}, 0, len(opts.Requests))
	fileLines := make([]map[string]interface{}, 0, len(opts.Requests))
	for _, req := range opts.Requests {
		var body map[string]interface{}
		var reqWarnings []types.Warning
		switch req.Type {
		case provider.BatchRequestTypeImage:
			body, reqWarnings, err = b.prepareImageBatchRequest(req.Image)
		default:
			textReq := req.Text
			lm := NewLanguageModel(b.provider, textReq.ModelID)
			body, reqWarnings, err = lm.PrepareBatchRequestBody(&textReq.Options)
		}
		if err != nil {
			return nil, err
		}
		id := req.RequestID()
		inlinedRequests = append(inlinedRequests, map[string]interface{}{
			"request":  body,
			"metadata": map[string]interface{}{"key": id},
		})
		fileLines = append(fileLines, map[string]interface{}{"key": id, "request": body})
		for _, w := range reqWarnings {
			warnings = append(warnings, provider.BatchV4Warning{RequestID: id, Warning: w})
		}
	}

	displayName := "ai-sdk-batch-" + generateGoogleBatchDisplayNameSuffix()
	batchFields := map[string]interface{}{"displayName": displayName}
	if opts.WebhookURL != "" {
		batchFields["webhookConfig"] = map[string]interface{}{"uris": []string{opts.WebhookURL}}
	}

	createPath := fmt.Sprintf("/%s:batchGenerateContent", gemini.GetModelPath(modelID))

	inlineBody := map[string]interface{}{}
	for k, v := range batchFields {
		inlineBody[k] = v
	}
	inlineBody["inputConfig"] = map[string]interface{}{
		"requests": map[string]interface{}{"requests": inlinedRequests},
	}
	requestBody := map[string]interface{}{"batch": inlineBody}
	inlineJSON, _ := json.Marshal(requestBody)

	if len(inlineJSON) < googleBatchInlineCreationMaxBytes {
		var operation googleBatchOperationWire
		if err := b.provider.client.DoJSON(ctx, internalhttp.Request{
			Method:  http.MethodPost,
			Path:    createPath,
			Body:    requestBody,
			Headers: opts.Headers,
		}, &operation); err != nil {
			return nil, b.handleError(err)
		}
		return &provider.BatchV4StartResult{BatchV4Status: operation.toStatus(), BatchID: operation.Name, Warnings: warnings}, nil
	}

	var fileParts bytes.Buffer
	for _, line := range fileLines {
		encoded, err := json.Marshal(line)
		if err != nil {
			return nil, err
		}
		fileParts.Write(encoded)
		fileParts.WriteByte('\n')
	}
	if fileParts.Len() > googleBatchInputFileMaxBytes {
		return nil, &providererrors.InvalidArgumentError{Field: "requests", Message: "Google batch input files must not exceed 2 GB."}
	}

	uploadedFile, err := b.uploadBatchInputFile(ctx, fileParts.Bytes(), displayName+"-input", opts.Headers)
	if err != nil {
		return nil, err
	}

	fileBatchBody := map[string]interface{}{}
	for k, v := range batchFields {
		fileBatchBody[k] = v
	}
	fileBatchBody["inputConfig"] = map[string]interface{}{"fileName": uploadedFile.Name}

	var operation googleBatchOperationWire
	if err := b.provider.client.DoJSON(ctx, internalhttp.Request{
		Method:  http.MethodPost,
		Path:    createPath,
		Body:    map[string]interface{}{"batch": fileBatchBody},
		Headers: opts.Headers,
	}, &operation); err != nil {
		return nil, b.handleError(err)
	}

	status := operation.toStatus()
	metadata := map[string]interface{}{"inputFileId": uploadedFile.Name}
	if uploadedFile.ExpirationTime != "" {
		metadata["inputFileExpiresAt"] = uploadedFile.ExpirationTime
	}
	status.ProviderMetadata = map[string]interface{}{"google": metadata}

	return &provider.BatchV4StartResult{BatchV4Status: status, BatchID: operation.Name, Warnings: warnings}, nil
}

// googleBatchUploadedFile is the result of uploading a batch input JSONL
// file via Google's resumable upload protocol.
type googleBatchUploadedFile struct {
	Name           string
	ExpirationTime string
}

// uploadBatchInputFile uploads content as a resumable file upload (the same
// two-step protocol as FilesAPI.UploadFile, without the processing-state
// poll batch input files don't need). Mirrors TS's inline upload calls in
// GoogleBatch.doStartBatch.
func (b *Batch) uploadBatchInputFile(ctx context.Context, content []byte, displayName string, headers map[string]string) (*googleBatchUploadedFile, error) {
	baseOrigin := strings.TrimSuffix(b.provider.config.BaseURL, "/v1beta")
	authHeaders := internalhttp.MergeHeaders(map[string]string{"x-goog-api-key": b.provider.APIKey()}, b.provider.config.Headers)
	authHeaders = internalhttp.MergeHeaders(authHeaders, headers)

	initPayload, err := json.Marshal(map[string]interface{}{"file": map[string]interface{}{"display_name": displayName}})
	if err != nil {
		return nil, err
	}
	initReq, err := http.NewRequestWithContext(ctx, http.MethodPost, baseOrigin+"/upload/v1beta/files", bytes.NewReader(initPayload))
	if err != nil {
		return nil, err
	}
	for k, v := range authHeaders {
		initReq.Header.Set(k, v)
	}
	initReq.Header.Set("X-Goog-Upload-Protocol", "resumable")
	initReq.Header.Set("X-Goog-Upload-Command", "start")
	initReq.Header.Set("X-Goog-Upload-Header-Content-Length", strconv.Itoa(len(content)))
	initReq.Header.Set("X-Goog-Upload-Header-Content-Type", "application/jsonl")
	initReq.Header.Set("Content-Type", "application/json")

	initResp, err := b.provider.client.HTTPClient().Do(initReq)
	if err != nil {
		return nil, err
	}
	defer initResp.Body.Close() //nolint:errcheck
	if initResp.StatusCode >= 400 {
		body, _ := io.ReadAll(initResp.Body)
		return nil, b.handleError(&internalhttp.HTTPStatusError{StatusCode: initResp.StatusCode, Headers: initResp.Header, Body: body})
	}
	uploadURL := initResp.Header.Get("x-goog-upload-url")
	if uploadURL == "" {
		return nil, providererrors.NewInvalidResponseDataError(nil, "Google did not return a resumable upload URL.")
	}

	uploadReq, err := http.NewRequestWithContext(ctx, http.MethodPost, uploadURL, bytes.NewReader(content))
	if err != nil {
		return nil, err
	}
	uploadReq.Header.Set("Content-Length", strconv.Itoa(len(content)))
	uploadReq.Header.Set("X-Goog-Upload-Offset", "0")
	uploadReq.Header.Set("X-Goog-Upload-Command", "upload, finalize")
	uploadReq.Header.Set("Content-Type", "application/jsonl")

	uploadResp, err := b.provider.client.HTTPClient().Do(uploadReq)
	if err != nil {
		return nil, err
	}
	defer uploadResp.Body.Close() //nolint:errcheck
	if uploadResp.StatusCode >= 400 {
		body, _ := io.ReadAll(uploadResp.Body)
		return nil, b.handleError(&internalhttp.HTTPStatusError{StatusCode: uploadResp.StatusCode, Headers: uploadResp.Header, Body: body})
	}

	var result struct {
		File struct {
			Name           string `json:"name"`
			ExpirationTime string `json:"expirationTime"`
		} `json:"file"`
	}
	if err := json.NewDecoder(uploadResp.Body).Decode(&result); err != nil {
		return nil, err
	}
	return &googleBatchUploadedFile{Name: result.File.Name, ExpirationTime: result.File.ExpirationTime}, nil
}

// prepareImageBatchRequest builds a GenerateContent request body for one
// image batch item, mirroring TS GoogleBatch.prepareImageRequest: translates
// ImageGenerateOptions into a single-turn Gemini chat request with
// responseModalities: ["IMAGE"], reusing the same content/providerOptions
// construction as ImageModel.DoGenerate (googleGeminiImageContent,
// geminiImageProviderOptions, googleImageWarnings) instead of duplicating
// it.
func (b *Batch) prepareImageBatchRequest(req *provider.ImageBatchV4Request) (map[string]interface{}, []types.Warning, error) {
	opts := req.Options

	if opts.Mask != nil {
		return nil, nil, &providererrors.UnsupportedFunctionalityError{
			Functionality: "mask-based image editing in Google batches",
		}
	}
	if opts.N != nil && *opts.N > 1 {
		return nil, nil, &providererrors.UnsupportedFunctionalityError{
			Functionality: "multiple images per Google batch request",
		}
	}

	warnings := googleImageWarnings(&opts)

	providerOptions, googleSearch := geminiImageProviderOptions(&opts)
	lmOpts := &provider.GenerateOptions{
		Prompt: types.Prompt{
			Messages: []types.Message{{
				Role:    types.RoleUser,
				Content: googleGeminiImageContent(&opts),
			}},
		},
		Seed:            opts.Seed,
		ProviderOptions: providerOptions,
	}
	if googleSearch != nil {
		googleSearchArgs, _ := googleSearch.(map[string]interface{})
		lmOpts.Tools = []types.Tool{gemini.GoogleSearchTool(googleSearchArgs)}
	}

	lm := NewLanguageModel(b.provider, req.ModelID)
	body, reqWarnings, err := lm.PrepareBatchRequestBody(lmOpts)
	if err != nil {
		return nil, nil, err
	}
	return body, append(warnings, reqWarnings...), nil
}

// DoGetBatchStatus retrieves a batch's lifecycle status (`GET {batchId}`).
func (b *Batch) DoGetBatchStatus(ctx context.Context, opts provider.BatchV4OperationOptions) (*provider.BatchV4Status, error) {
	operation, err := b.retrieveBatch(ctx, opts.BatchID, opts.Headers)
	if err != nil {
		return nil, err
	}
	status := operation.toStatus()
	return &status, nil
}

// DoCancelBatch requests cancellation of a batch (`POST {batchId}:cancel`).
func (b *Batch) DoCancelBatch(ctx context.Context, opts provider.BatchV4OperationOptions) (*provider.BatchV4CancelResult, error) {
	var response map[string]interface{}
	if err := b.provider.client.DoJSON(ctx, internalhttp.Request{
		Method:  http.MethodPost,
		Path:    "/" + opts.BatchID + ":cancel",
		Body:    map[string]interface{}{},
		Headers: opts.Headers,
	}, &response); err != nil {
		return nil, b.handleError(err)
	}
	return &provider.BatchV4CancelResult{}, nil
}

// DoListBatches lists batches (`GET /batches`).
func (b *Batch) DoListBatches(ctx context.Context, opts provider.BatchV4ListOptions) (*provider.BatchV4ListResult, error) {
	query := map[string]string{}
	if opts.Limit != nil {
		query["pageSize"] = strconv.Itoa(*opts.Limit)
	}
	if opts.Cursor != "" {
		query["pageToken"] = opts.Cursor
	}

	var response struct {
		Operations    []googleBatchOperationWire `json:"operations"`
		NextPageToken string                     `json:"nextPageToken"`
	}
	if err := b.provider.client.DoJSON(ctx, internalhttp.Request{
		Method:  http.MethodGet,
		Path:    "/batches",
		Query:   query,
		Headers: opts.Headers,
	}, &response); err != nil {
		return nil, b.handleError(err)
	}

	batches := make([]provider.BatchV4ListItem, 0, len(response.Operations))
	for _, op := range response.Operations {
		batches = append(batches, provider.BatchV4ListItem{BatchV4Status: op.toStatus(), BatchID: op.Name})
	}
	result := &provider.BatchV4ListResult{Batches: batches}
	if response.NextPageToken != "" {
		result.NextCursor = response.NextPageToken
	}
	return result, nil
}

// DoGetBatchResults streams the per-request results of a terminal batch,
// either from the operation's inlined responses or by downloading its
// responses file (NDJSON). Mirrors TypeScript's GoogleBatch.doGetBatchResults.
func (b *Batch) DoGetBatchResults(ctx context.Context, opts provider.BatchV4OperationOptions) (provider.BatchV4ItemResultStream, error) {
	operation, err := b.retrieveBatch(ctx, opts.BatchID, opts.Headers)
	if err != nil {
		return nil, err
	}
	status := operation.toStatus()
	if status.Status == provider.BatchStatusPending {
		return nil, &providererrors.InvalidArgumentError{
			Field:   "batchId",
			Message: fmt.Sprintf("Google batch %q is not complete.", opts.BatchID),
		}
	}

	if inlined := operation.inlinedResponses(); inlined != nil {
		return newGoogleBatchInlineResultsStream(b, inlined), nil
	}

	responsesFile := operation.responsesFile()
	if responsesFile == "" {
		if status.Status == provider.BatchStatusCompleted {
			return nil, providererrors.NewInvalidResponseDataError(operation,
				fmt.Sprintf("Google batch %q completed without batch output.", opts.BatchID))
		}
		return newGoogleBatchInlineResultsStream(b, nil), nil
	}

	segments := strings.Split(responsesFile, "/")
	for i, seg := range segments {
		segments[i] = providerutils.EncodePathSegment(seg)
	}
	baseOrigin := strings.TrimSuffix(b.provider.config.BaseURL, "/v1beta")
	downloadURL := fmt.Sprintf("%s/download/v1beta/%s:download?alt=media", baseOrigin, strings.Join(segments, "/"))

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, downloadURL, nil)
	if err != nil {
		return nil, err
	}
	authHeaders := internalhttp.MergeHeaders(map[string]string{"x-goog-api-key": b.provider.APIKey()}, b.provider.config.Headers)
	authHeaders = internalhttp.MergeHeaders(authHeaders, opts.Headers)
	for k, v := range authHeaders {
		req.Header.Set(k, v)
	}

	resp, err := b.provider.client.HTTPClient().Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		defer resp.Body.Close() //nolint:errcheck
		body, _ := io.ReadAll(resp.Body)
		return nil, b.handleError(&internalhttp.HTTPStatusError{StatusCode: resp.StatusCode, Headers: resp.Header, Body: body})
	}

	return newGoogleBatchFileResultsStream(b, resp.Body), nil
}

func (b *Batch) retrieveBatch(ctx context.Context, batchID string, headers map[string]string) (*googleBatchOperationWire, error) {
	var operation googleBatchOperationWire
	if err := b.provider.client.DoJSON(ctx, internalhttp.Request{
		Method:  http.MethodGet,
		Path:    "/" + batchID,
		Headers: headers,
	}, &operation); err != nil {
		return nil, b.handleError(err)
	}
	return &operation, nil
}

func (b *Batch) handleError(err error) error {
	lm := NewLanguageModel(b.provider, "")
	return lm.HandleError(err)
}

// validateGoogleBatchRequests mirrors TS assertSupportedBatchRequests: text
// and image requests are supported, anything else is rejected.
func validateGoogleBatchRequests(requests []provider.BatchV4Request) error {
	for _, req := range requests {
		switch req.Type {
		case provider.BatchRequestTypeText:
			if req.Text == nil {
				return &providererrors.UnsupportedFunctionalityError{
					Functionality: fmt.Sprintf("batch request type: %s", req.Type),
					Message:       fmt.Sprintf("The Google Batch API does not support batch requests with type %q.", req.Type),
				}
			}
		case provider.BatchRequestTypeImage:
			if req.Image == nil {
				return &providererrors.UnsupportedFunctionalityError{
					Functionality: fmt.Sprintf("batch request type: %s", req.Type),
					Message:       fmt.Sprintf("The Google Batch API does not support batch requests with type %q.", req.Type),
				}
			}
		default:
			return &providererrors.UnsupportedFunctionalityError{
				Functionality: fmt.Sprintf("batch request type: %s", req.Type),
				Message:       fmt.Sprintf("The Google Batch API does not support batch requests with type %q.", req.Type),
			}
		}
	}
	return nil
}

// getGoogleBatchModelID mirrors TS getGoogleBatchModelId: every request must
// use the same model, since the model is part of the batch endpoint URL.
func getGoogleBatchModelID(requests []provider.BatchV4Request) (string, error) {
	if len(requests) == 0 {
		return "", &providererrors.InvalidArgumentError{Field: "requests", Message: "Google batches require at least one request."}
	}
	modelID := requests[0].RequestModelID()
	for _, req := range requests {
		if req.RequestModelID() != modelID {
			return "", &providererrors.InvalidArgumentError{
				Field:   "requests",
				Message: "Google batches require every request to use the same model because the model is part of the batch endpoint.",
			}
		}
	}
	return modelID, nil
}

// generateGoogleBatchDisplayNameSuffix returns a short random identifier for
// batch displayName values.
func generateGoogleBatchDisplayNameSuffix() string {
	return streaming.GenerateID()
}

// --- Wire types -------------------------------------------------------

type googleRPCStatusWire struct {
	Code    interface{} `json:"code"`
	Message string      `json:"message"`
	Status  string      `json:"status"`
}

func (e *googleRPCStatusWire) toBatchError(fallbackMessage string) *provider.BatchError {
	if e == nil {
		return nil
	}
	out := &provider.BatchError{Message: e.Message}
	if out.Message == "" {
		out.Message = fallbackMessage
	}
	if e.Status != "" {
		out.Type = e.Status
	}
	if e.Code != nil {
		out.Code = fmt.Sprintf("%v", e.Code)
	}
	return out
}

type googleBatchStatsWire struct {
	RequestCount           interface{} `json:"requestCount"`
	SuccessfulRequestCount interface{} `json:"successfulRequestCount"`
	FailedRequestCount     interface{} `json:"failedRequestCount"`
	PendingRequestCount    interface{} `json:"pendingRequestCount"`
}

type googleBatchInlinedResponseWire struct {
	Metadata struct {
		Key string `json:"key"`
	} `json:"metadata"`
	Response json.RawMessage      `json:"response"`
	Error    *googleRPCStatusWire `json:"error"`
}

type googleBatchOutputWire struct {
	ResponsesFile    string `json:"responsesFile"`
	InlinedResponses *struct {
		InlinedResponses []googleBatchInlinedResponseWire `json:"inlinedResponses"`
	} `json:"inlinedResponses"`
}

type googleBatchOperationWire struct {
	Name     string `json:"name"`
	Metadata *struct {
		State      string                 `json:"state"`
		CreateTime string                 `json:"createTime"`
		BatchStats *googleBatchStatsWire  `json:"batchStats"`
		Output     *googleBatchOutputWire `json:"output"`
	} `json:"metadata"`
	Done     bool                   `json:"done"`
	Error    *googleRPCStatusWire   `json:"error"`
	Response *googleBatchOutputWire `json:"response"`
}

func (o googleBatchOperationWire) inlinedResponses() []googleBatchInlinedResponseWire {
	if o.Metadata != nil && o.Metadata.Output != nil && o.Metadata.Output.InlinedResponses != nil {
		return o.Metadata.Output.InlinedResponses.InlinedResponses
	}
	if o.Response != nil && o.Response.InlinedResponses != nil {
		return o.Response.InlinedResponses.InlinedResponses
	}
	return nil
}

func (o googleBatchOperationWire) responsesFile() string {
	if o.Metadata != nil && o.Metadata.Output != nil && o.Metadata.Output.ResponsesFile != "" {
		return o.Metadata.Output.ResponsesFile
	}
	if o.Response != nil {
		return o.Response.ResponsesFile
	}
	return ""
}

func (o googleBatchOperationWire) toStatus() provider.BatchV4Status {
	var rawStatus string
	if o.Metadata != nil {
		rawStatus = o.Metadata.State
	}
	batchErr := o.Error.toBatchError("Google batch failed.")

	status := provider.BatchV4Status{
		Status:    mapGoogleBatchStatus(rawStatus, o.Done, batchErr != nil),
		RawStatus: rawStatus,
		Error:     batchErr,
	}
	if o.Metadata != nil {
		status.RequestCounts = convertGoogleRequestCounts(o.Metadata.BatchStats)
		status.CreatedAt = o.Metadata.CreateTime
	}
	return status
}

func mapGoogleBatchStatus(rawStatus string, done bool, hasError bool) provider.BatchStatusValue {
	if hasError {
		return provider.BatchStatusFailed
	}
	if rawStatus == "" {
		if done {
			return provider.BatchStatusCompleted
		}
		return provider.BatchStatusPending
	}
	normalized := strings.TrimPrefix(rawStatus, "BATCH_STATE_")
	normalized = strings.TrimPrefix(normalized, "JOB_STATE_")
	switch normalized {
	case "SUCCEEDED":
		return provider.BatchStatusCompleted
	case "FAILED", "CANCELLED", "EXPIRED":
		return provider.BatchStatusFailed
	default:
		// "UNSPECIFIED", "PENDING", "RUNNING", and any unknown state.
		return provider.BatchStatusPending
	}
}

// convertGoogleRequestCounts mirrors TS convertGoogleRequestCounts: the
// sub-counts default to 0 when absent (parseCount(counts?.xField ?? 0)),
// but total does not (parseCount(counts?.requestCount)) — a batch whose
// stats never report a total (or report inconsistent/negative/non-integer
// counts) omits requestCounts entirely via NormalizeBatchRequestCounts,
// rather than fabricating a zero-value block.
func convertGoogleRequestCounts(stats *googleBatchStatsWire) *provider.BatchRequestCounts {
	if stats == nil {
		return nil
	}
	total := parseGoogleBatchCount(stats.RequestCount)
	pending := parseGoogleBatchCountOrZero(stats.PendingRequestCount)
	completed := parseGoogleBatchCountOrZero(stats.SuccessfulRequestCount)
	failed := parseGoogleBatchCountOrZero(stats.FailedRequestCount)
	return providerutils.NormalizeBatchRequestCounts(total, pending, completed, failed)
}

// parseGoogleBatchCount parses a wire count (string or float64) into a
// non-negative int, returning nil when missing or invalid (matching TS
// parseCount's Number.isSafeInteger && >= 0 check; Go's int has no separate
// "safe integer" range concern).
func parseGoogleBatchCount(v interface{}) *int {
	switch t := v.(type) {
	case string:
		if n, err := strconv.Atoi(t); err == nil && n >= 0 {
			return &n
		}
		return nil
	case float64:
		if n := int(t); float64(n) == t && n >= 0 {
			return &n
		}
		return nil
	default:
		return nil
	}
}

// parseGoogleBatchCountOrZero mirrors TS's `parseCount(counts?.xField ?? 0)`
// default-to-zero behavior for the completed/failed/pending sub-counts.
func parseGoogleBatchCountOrZero(v interface{}) *int {
	if v == nil {
		zero := 0
		return &zero
	}
	return parseGoogleBatchCount(v)
}

// convertGoogleBatchResultLine converts one batch result (key + response +
// error) into a BatchV4ItemResult, mirroring TS GoogleBatch.iterateBatchResults.
func (b *Batch) convertGoogleBatchResultLine(key string, response json.RawMessage, errWire *googleRPCStatusWire) *provider.BatchV4ItemResult {
	if errWire != nil {
		status := provider.BatchItemFailed
		codeStr := fmt.Sprintf("%v", errWire.Code)
		if errWire.Status == "CANCELLED" || codeStr == "1" {
			status = provider.BatchItemCancelled
		}
		return &provider.BatchV4ItemResult{
			Type:   provider.BatchRequestTypeText,
			ID:     key,
			Status: status,
			Error:  errWire.toBatchError("Google batch request failed."),
		}
	}

	if len(response) == 0 || string(response) == "null" {
		return &provider.BatchV4ItemResult{
			Type:   provider.BatchRequestTypeText,
			ID:     key,
			Status: provider.BatchItemFailed,
			Error:  &provider.BatchError{Message: "Google returned a batch result without a response or error.", Code: "invalid_batch_result"},
		}
	}

	var preview struct {
		Candidates     []json.RawMessage `json:"candidates"`
		PromptFeedback *struct {
			BlockReason string `json:"blockReason"`
		} `json:"promptFeedback"`
	}
	if err := json.Unmarshal(response, &preview); err == nil && len(preview.Candidates) == 0 {
		message := "Google returned a batch response without any candidates."
		code := "invalid_response"
		errType := ""
		var meta map[string]interface{}
		if preview.PromptFeedback != nil {
			meta = map[string]interface{}{"google": map[string]interface{}{
				"promptFeedback": map[string]interface{}{"blockReason": preview.PromptFeedback.BlockReason},
			}}
			if preview.PromptFeedback.BlockReason != "" {
				message = fmt.Sprintf("Google blocked the batch request (%s).", preview.PromptFeedback.BlockReason)
				code = "prompt_blocked"
				errType = preview.PromptFeedback.BlockReason
			}
		}
		item := &provider.BatchV4ItemResult{
			Type:   provider.BatchRequestTypeText,
			ID:     key,
			Status: provider.BatchItemFailed,
			Error:  &provider.BatchError{Message: message, Type: errType, Code: code},
		}
		if meta != nil {
			item.ProviderMetadata = meta
		}
		return item
	}

	var geminiResp gemini.Response
	if err := json.Unmarshal(response, &geminiResp); err != nil {
		return &provider.BatchV4ItemResult{
			Type:   provider.BatchRequestTypeText,
			ID:     key,
			Status: provider.BatchItemFailed,
			Error:  &provider.BatchError{Message: "Google returned an invalid GenerateContent batch result.", Code: "invalid_response"},
		}
	}

	lm := NewLanguageModel(b.provider, "")
	genResult := lm.ConvertBatchResponse(geminiResp)

	if imageResult := convertGoogleBatchImageResult(genResult); imageResult != nil {
		return &provider.BatchV4ItemResult{
			Type:        provider.BatchRequestTypeImage,
			ID:          key,
			Status:      provider.BatchItemSucceeded,
			ImageResult: imageResult,
		}
	}

	for _, part := range genResult.Content {
		if !googleBatchSupportedContentTypes[part.ContentType()] {
			return &provider.BatchV4ItemResult{
				Type:   provider.BatchRequestTypeText,
				ID:     key,
				Status: provider.BatchItemFailed,
				Error: &provider.BatchError{
					Message: fmt.Sprintf("Google returned a %q content block, but that content is not supported in AI SDK text batches.", part.ContentType()),
					Code:    "unsupported_content",
				},
			}
		}
	}

	return &provider.BatchV4ItemResult{Type: provider.BatchRequestTypeText, ID: key, Status: provider.BatchItemSucceeded, TextResult: genResult}
}

// convertGoogleBatchImageResult mirrors TS convertGoogleImageBatchResult:
// unconditionally inspects a converted batch result for inline generated
// image data (regardless of the original request's declared modality,
// matching TS's iterateBatchResults calling this before the text
// content-type check) and, if any is found, builds an ImageResult. Returns
// nil when the result carries no generated image content, so the caller
// falls through to text-result handling.
func convertGoogleBatchImageResult(genResult *types.GenerateResult) *types.ImageResult {
	var images [][]byte
	var base64Images []string
	for _, part := range genResult.Content {
		file, ok := part.(types.GeneratedFileContent)
		if !ok || !strings.HasPrefix(file.MediaType, "image/") || len(file.Data) == 0 {
			continue
		}
		images = append(images, file.Data)
		base64Images = append(base64Images, base64.StdEncoding.EncodeToString(file.Data))
	}
	if len(images) == 0 {
		return nil
	}

	googleMetadata := map[string]interface{}{}
	if raw, ok := genResult.ProviderMetadata["google"]; ok {
		if meta, ok := raw.(map[string]interface{}); ok {
			for k, v := range meta {
				googleMetadata[k] = v
			}
		}
	}
	imagesMeta := make([]map[string]interface{}, len(images))
	for i := range imagesMeta {
		imagesMeta[i] = map[string]interface{}{}
	}
	googleMetadata["images"] = imagesMeta

	var inputTokens, outputTokens int
	if genResult.Usage.InputTokens != nil {
		inputTokens = int(*genResult.Usage.InputTokens)
	}
	if genResult.Usage.OutputTokens != nil {
		outputTokens = int(*genResult.Usage.OutputTokens)
	}

	var modelID string
	var headers map[string]string
	if genResult.ResponseMetadata != nil {
		modelID = genResult.ResponseMetadata.ModelID
		headers = genResult.ResponseMetadata.Headers
	}

	return &types.ImageResult{
		Image:        images[0],
		Images:       images,
		Base64Image:  base64Images[0],
		Base64Images: base64Images,
		Usage: types.ImageUsage{
			ImageCount:   len(images),
			InputTokens:  inputTokens,
			OutputTokens: outputTokens,
			TotalTokens:  inputTokens + outputTokens,
		},
		Warnings:         genResult.Warnings,
		ProviderMetadata: map[string]interface{}{"google": googleMetadata},
		Response: &types.ResponseMetadata{
			ModelID:   modelID,
			Timestamp: time.Now(),
			Headers:   headers,
		},
	}
}

// googleBatchInlineResultsStream implements provider.BatchV4ItemResultStream
// over an operation's inlinedResponses.
type googleBatchInlineResultsStream struct {
	batch   *Batch
	results []googleBatchInlinedResponseWire
	idx     int
}

func newGoogleBatchInlineResultsStream(b *Batch, results []googleBatchInlinedResponseWire) *googleBatchInlineResultsStream {
	return &googleBatchInlineResultsStream{batch: b, results: results}
}

func (s *googleBatchInlineResultsStream) Next() (*provider.BatchV4ItemResult, error) {
	if s.idx >= len(s.results) {
		return nil, io.EOF
	}
	r := s.results[s.idx]
	s.idx++
	return s.batch.convertGoogleBatchResultLine(r.Metadata.Key, r.Response, r.Error), nil
}

func (s *googleBatchInlineResultsStream) Err() error   { return nil }
func (s *googleBatchInlineResultsStream) Close() error { return nil }

// googleBatchResultLineWire is one line of a downloaded responses file.
type googleBatchResultLineWire struct {
	Key      string               `json:"key"`
	Response json.RawMessage      `json:"response"`
	Error    *googleRPCStatusWire `json:"error"`
}

// googleBatchFileResultsStream implements provider.BatchV4ItemResultStream
// over the NDJSON body of a downloaded responses file.
type googleBatchFileResultsStream struct {
	batch   *Batch
	scanner *bufio.Scanner
	body    io.ReadCloser
	err     error
}

func newGoogleBatchFileResultsStream(b *Batch, body io.ReadCloser) *googleBatchFileResultsStream {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 64*1024), 32*1024*1024)
	return &googleBatchFileResultsStream{batch: b, scanner: scanner, body: body}
}

func (s *googleBatchFileResultsStream) Next() (*provider.BatchV4ItemResult, error) {
	for {
		if !s.scanner.Scan() {
			if err := s.scanner.Err(); err != nil {
				s.err = err
				return nil, err
			}
			return nil, io.EOF
		}
		line := bytes.TrimSpace(s.scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var wire googleBatchResultLineWire
		if err := json.Unmarshal(line, &wire); err != nil {
			s.err = err
			return nil, err
		}
		return s.batch.convertGoogleBatchResultLine(wire.Key, wire.Response, wire.Error), nil
	}
}

func (s *googleBatchFileResultsStream) Err() error   { return s.err }
func (s *googleBatchFileResultsStream) Close() error { return s.body.Close() }
