package openai

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai/responses"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// openaiBatchEndpoint is the sub-request URL embedded in each JSONL line of
// the uploaded batch input file (TS openaiBatchEndpoint).
const openaiBatchEndpoint = "/v1/responses"

// openaiBatchInputFileDefaultExpiresAfterSeconds is the default TTL (48h) for
// the uploaded batch input file, measured from upload time.
const openaiBatchInputFileDefaultExpiresAfterSeconds = 48 * 60 * 60

// openaiResponsesSupportedURLs mirrors TS's openaiResponsesSupportedUrls,
// shared between OpenAIResponsesLanguageModel and OpenAIBatch.
func openaiResponsesSupportedURLs() map[string][]string {
	return map[string][]string{
		"image/*":         {`^https?://.*$`},
		"application/pdf": {`^https?://.*$`},
	}
}

// openAIBatchConvertibleProviderToolIDs are the provider-defined tool IDs
// whose batch output the converter below can decode. Any other provider tool
// in a batch request only produces a warning (TS
// openAIBatchConvertibleProviderToolIds).
var openAIBatchConvertibleProviderToolIDs = map[string]bool{
	"openai.code_interpreter":   true,
	"openai.custom":             true,
	"openai.file_search":        true,
	"openai.web_search":         true,
	"openai.web_search_preview": true,
}

// Batch implements provider.BatchV4 for the OpenAI Batch API
// (`/v1/batches`, backed by `/v1/responses` sub-requests). Mirrors
// TypeScript's OpenAIBatch (packages/openai/src/openai-batch.ts).
type Batch struct {
	provider *Provider
}

// NewBatch creates a new OpenAI batch interface.
func NewBatch(p *Provider) *Batch { return &Batch{provider: p} }

// ExperimentalBatch implements provider.BatchProvider for Provider, exposing
// the OpenAI Batch API. Mirrors TypeScript's `provider.experimental_batch =
// createBatch`.
func (p *Provider) ExperimentalBatch() provider.BatchV4 { return NewBatch(p) }

// SpecificationVersion returns the batch interface specification version.
func (b *Batch) SpecificationVersion() string { return "v4" }

// Provider returns the batch interface's provider ID, e.g. "openai.batch".
func (b *Batch) Provider() string { return b.provider.Name() + ".batch" }

// SupportedURLs returns the same URL patterns the Responses API language
// model accepts directly.
func (b *Batch) SupportedURLs() map[string][]string { return openaiResponsesSupportedURLs() }

// DoStartBatch uploads a JSONL file of prepared `/v1/responses` sub-requests
// and starts a batch job (`POST /files`, then `POST /batches`).
func (b *Batch) DoStartBatch(ctx context.Context, opts provider.BatchV4StartOptions) (*provider.BatchV4StartResult, error) {
	if err := validateOpenAITextBatchRequests(opts.Requests); err != nil {
		return nil, err
	}
	if err := validateOpenAISingleModel(opts.Requests); err != nil {
		return nil, err
	}

	var warnings []provider.BatchV4Warning
	if opts.WebhookURL != "" {
		warnings = append(warnings, provider.BatchV4Warning{
			Warning: types.Warning{
				Type:    "unsupported",
				Feature: "webhookUrl",
				Details: "The OpenAI Batch API does not support per-batch webhook URLs.",
			},
		})
	}

	inputFileExpiresAfterSeconds := openaiBatchInputFileExpiresAfter(opts.ProviderOptions)

	var fileParts bytes.Buffer
	for _, req := range opts.Requests {
		textReq := req.Text

		lm := NewResponsesLanguageModel(b.provider, textReq.ModelID)
		body, _, reqWarnings, err := lm.buildRequest(&textReq.Options, false)
		if err != nil {
			return nil, err
		}

		line, err := json.Marshal(map[string]interface{}{
			"custom_id": textReq.ID,
			"method":    "POST",
			"url":       openaiBatchEndpoint,
			"body":      body,
		})
		if err != nil {
			return nil, err
		}
		fileParts.Write(line)
		fileParts.WriteByte('\n')

		for _, w := range reqWarnings {
			warnings = append(warnings, provider.BatchV4Warning{RequestID: textReq.ID, Warning: w})
		}
		for _, tool := range textReq.Options.Tools {
			if tool.Type == types.ToolTypeProviderDefined && !openAIBatchConvertibleProviderToolIDs[tool.ProviderID] {
				warnings = append(warnings, provider.BatchV4Warning{
					RequestID: textReq.ID,
					Warning: types.Warning{
						Type:    "unsupported",
						Feature: fmt.Sprintf("batch result conversion for tool %q", tool.Name),
						Details: "OpenAI may return output for this tool that AI SDK text batches cannot currently convert.",
					},
				})
			}
		}
	}

	uploadedFile, err := b.uploadBatchInputFile(ctx, fileParts.Bytes(), inputFileExpiresAfterSeconds, opts.Headers)
	if err != nil {
		return nil, err
	}

	var batchResp openAIBatchResponseWire
	if err := b.provider.client.DoJSON(ctx, internalhttp.Request{
		Method: http.MethodPost,
		Path:   "/batches",
		Body: map[string]interface{}{
			"input_file_id":     uploadedFile.ID,
			"endpoint":          openaiBatchEndpoint,
			"completion_window": "24h",
		},
		Headers: opts.Headers,
	}, &batchResp); err != nil {
		return nil, b.handleError(err)
	}

	metadata := map[string]interface{}{"inputFileId": uploadedFile.ID}
	if uploadedFile.ExpiresAt != nil {
		metadata["inputFileExpiresAt"] = unixSecondsToTime(uploadedFile.ExpiresAt).UTC().Format(time.RFC3339)
	}

	status := batchResp.toStatus()
	status.ProviderMetadata = map[string]interface{}{"openai": metadata}

	return &provider.BatchV4StartResult{
		BatchV4Status: status,
		BatchID:       batchResp.ID,
		Warnings:      warnings,
	}, nil
}

// uploadBatchInputFile uploads the JSONL batch input as a "batch"-purpose
// file (`POST /files`), streaming the body via a multipart writer.
func (b *Batch) uploadBatchInputFile(ctx context.Context, content []byte, expiresAfterSeconds int, headers map[string]string) (*openAIFilesResponse, error) {
	parts := []internalhttp.MultipartStreamPart{
		{Name: "purpose", Value: "batch"},
		{Name: "expires_after[anchor]", Value: "created_at"},
		{Name: "expires_after[seconds]", Value: strconv.Itoa(expiresAfterSeconds)},
		{IsFile: true, Name: "file", Filename: "batch.jsonl", MediaType: "application/jsonl", Content: bytes.NewReader(content)},
	}
	body, contentType, err := internalhttp.NewMultipartStreamBody(parts)
	if err != nil {
		return nil, err
	}
	defer body.Close() //nolint:errcheck

	resp, err := b.provider.client.Do(ctx, internalhttp.Request{
		Method:  http.MethodPost,
		Path:    "/files",
		Body:    body,
		Headers: internalhttp.MergeHeaders(map[string]string{"Content-Type": contentType}, headers),
	})
	if err != nil {
		return nil, b.handleError(err)
	}
	if resp.StatusCode >= 400 {
		return nil, b.handleError(&internalhttp.HTTPStatusError{StatusCode: resp.StatusCode, Headers: resp.Headers, Body: resp.Body})
	}

	var out openAIFilesResponse
	if err := json.Unmarshal(resp.Body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DoGetBatchStatus retrieves a batch's lifecycle status (`GET /batches/{id}`).
func (b *Batch) DoGetBatchStatus(ctx context.Context, opts provider.BatchV4OperationOptions) (*provider.BatchV4Status, error) {
	batch, err := b.retrieveBatch(ctx, opts.BatchID, opts.Headers)
	if err != nil {
		return nil, err
	}
	status := batch.toStatus()
	return &status, nil
}

// DoCancelBatch requests cancellation of a batch (`POST /batches/{id}/cancel`).
func (b *Batch) DoCancelBatch(ctx context.Context, opts provider.BatchV4OperationOptions) (*provider.BatchV4CancelResult, error) {
	var resp openAIBatchResponseWire
	if err := b.provider.client.DoJSON(ctx, internalhttp.Request{
		Method:  http.MethodPost,
		Path:    "/batches/" + providerutils.EncodePathSegment(opts.BatchID) + "/cancel",
		Body:    map[string]interface{}{},
		Headers: opts.Headers,
	}, &resp); err != nil {
		return nil, b.handleError(err)
	}
	return &provider.BatchV4CancelResult{}, nil
}

// DoListBatches lists batches (`GET /batches`).
func (b *Batch) DoListBatches(ctx context.Context, opts provider.BatchV4ListOptions) (*provider.BatchV4ListResult, error) {
	query := map[string]string{}
	if opts.Limit > 0 {
		query["limit"] = strconv.Itoa(opts.Limit)
	}
	if opts.Cursor != "" {
		query["after"] = opts.Cursor
	}

	var resp openAIBatchListResponseWire
	if err := b.provider.client.DoJSON(ctx, internalhttp.Request{
		Method:  http.MethodGet,
		Path:    "/batches",
		Query:   query,
		Headers: opts.Headers,
	}, &resp); err != nil {
		return nil, b.handleError(err)
	}

	batches := make([]provider.BatchV4ListItem, 0, len(resp.Data))
	for _, item := range resp.Data {
		batches = append(batches, provider.BatchV4ListItem{BatchV4Status: item.toStatus(), BatchID: item.ID})
	}
	result := &provider.BatchV4ListResult{Batches: batches}
	if resp.HasMore && resp.LastID != nil {
		result.NextCursor = *resp.LastID
	}
	return result, nil
}

// DoGetBatchResults streams the per-request results of a terminal batch by
// reading its output and error files (NDJSON). Mirrors TypeScript's
// OpenAIBatch.doGetBatchResults.
func (b *Batch) DoGetBatchResults(ctx context.Context, opts provider.BatchV4OperationOptions) (provider.BatchV4ItemResultStream, error) {
	batch, err := b.retrieveBatch(ctx, opts.BatchID, opts.Headers)
	if err != nil {
		return nil, err
	}
	status := batch.toStatus()
	if status.Status == provider.BatchStatusPending {
		return nil, &providererrors.InvalidArgumentError{
			Field:   "batchId",
			Message: fmt.Sprintf("OpenAI batch %q is not complete.", opts.BatchID),
		}
	}

	var fileIDs []string
	if batch.OutputFileID != nil {
		fileIDs = append(fileIDs, *batch.OutputFileID)
	}
	if batch.ErrorFileID != nil {
		fileIDs = append(fileIDs, *batch.ErrorFileID)
	}
	if status.Status == provider.BatchStatusCompleted && len(fileIDs) == 0 {
		return nil, providererrors.NewInvalidResponseDataError(batch,
			fmt.Sprintf("OpenAI batch %q completed without batch output.", opts.BatchID))
	}

	return newOpenAIBatchResultsStream(ctx, b, fileIDs, opts.Headers), nil
}

func (b *Batch) retrieveBatch(ctx context.Context, batchID string, headers map[string]string) (*openAIBatchResponseWire, error) {
	var resp openAIBatchResponseWire
	if err := b.provider.client.DoJSON(ctx, internalhttp.Request{
		Method:  http.MethodGet,
		Path:    "/batches/" + providerutils.EncodePathSegment(batchID),
		Headers: headers,
	}, &resp); err != nil {
		return nil, b.handleError(err)
	}
	return &resp, nil
}

func (b *Batch) handleError(err error) error {
	lm := &LanguageModel{provider: b.provider}
	return lm.handleError(err)
}

// validateOpenAITextBatchRequests mirrors TS assertTextBatchRequests.
func validateOpenAITextBatchRequests(requests []provider.BatchV4Request) error {
	for _, req := range requests {
		if req.Type != provider.BatchRequestTypeText || req.Text == nil {
			return &providererrors.UnsupportedFunctionalityError{
				Functionality: fmt.Sprintf("batch request type: %s", req.Type),
				Message:       fmt.Sprintf("The OpenAI Batch API does not support batch requests with type %q.", req.Type),
			}
		}
	}
	return nil
}

// validateOpenAISingleModel mirrors TS validateSingleModel: every request in
// an OpenAI batch must use the same model.
func validateOpenAISingleModel(requests []provider.BatchV4Request) error {
	if len(requests) == 0 {
		return nil
	}
	modelID := requests[0].Text.ModelID
	for _, req := range requests {
		if req.Text.ModelID != modelID {
			return &providererrors.InvalidArgumentError{
				Field: "requests",
				Message: fmt.Sprintf(
					"The OpenAI Batch API requires all requests in a batch to use the same model. Found %q and %q.",
					modelID, req.Text.ModelID),
			}
		}
	}
	return nil
}

// openaiBatchInputFileExpiresAfter extracts
// providerOptions.openai.inputFileExpiresAfter (or providerOptions.azure.*
// for Azure-hosted OpenAI), defaulting to 48 hours.
func openaiBatchInputFileExpiresAfter(providerOptions map[string]interface{}) int {
	for _, key := range []string{"openai", "azure"} {
		if opts, ok := providerOptions[key].(map[string]interface{}); ok {
			if v, ok := opts["inputFileExpiresAfter"].(float64); ok {
				return int(v)
			}
		}
	}
	return openaiBatchInputFileDefaultExpiresAfterSeconds
}

// --- Wire types -------------------------------------------------------

type openAIBatchRequestCountsWire struct {
	Total     *int `json:"total"`
	Completed *int `json:"completed"`
	Failed    *int `json:"failed"`
}

type openAIBatchErrorDataWire struct {
	Code    *string `json:"code"`
	Message *string `json:"message"`
}

type openAIBatchResponseWire struct {
	ID            string                        `json:"id"`
	Status        string                        `json:"status"`
	OutputFileID  *string                       `json:"output_file_id"`
	ErrorFileID   *string                       `json:"error_file_id"`
	CreatedAt     *int64                        `json:"created_at"`
	ExpiresAt     *int64                        `json:"expires_at"`
	RequestCounts *openAIBatchRequestCountsWire `json:"request_counts"`
	Errors        *struct {
		Data []openAIBatchErrorDataWire `json:"data"`
	} `json:"errors"`
}

func (r openAIBatchResponseWire) toStatus() provider.BatchV4Status {
	status := provider.BatchV4Status{
		Status:    mapOpenAIBatchStatus(r.Status),
		RawStatus: r.Status,
	}
	if r.RequestCounts != nil {
		var pending *int
		if r.RequestCounts.Total != nil && r.RequestCounts.Completed != nil && r.RequestCounts.Failed != nil {
			p := *r.RequestCounts.Total - *r.RequestCounts.Completed - *r.RequestCounts.Failed
			pending = &p
		}
		status.RequestCounts = providerutils.NormalizeBatchRequestCounts(
			r.RequestCounts.Total, pending, r.RequestCounts.Completed, r.RequestCounts.Failed,
		)
	}
	if r.Errors != nil && len(r.Errors.Data) > 0 {
		first := r.Errors.Data[0]
		msg := "OpenAI batch failed."
		if first.Message != nil {
			msg = *first.Message
		}
		batchErr := &provider.BatchError{Message: msg}
		if first.Code != nil {
			batchErr.Code = *first.Code
		}
		status.Error = batchErr
	}
	if t := unixSecondsToTime(r.CreatedAt); t != nil {
		status.CreatedAt = t.UTC().Format(time.RFC3339)
	}
	if t := unixSecondsToTime(r.ExpiresAt); t != nil {
		status.ExpiresAt = t.UTC().Format(time.RFC3339)
	}
	return status
}

func mapOpenAIBatchStatus(rawStatus string) provider.BatchStatusValue {
	switch rawStatus {
	case "completed":
		return provider.BatchStatusCompleted
	case "failed", "expired", "cancelled":
		return provider.BatchStatusFailed
	default:
		// "validating", "in_progress", "finalizing", "cancelling", and any
		// unknown status are treated conservatively as non-terminal.
		return provider.BatchStatusPending
	}
}

type openAIBatchListResponseWire struct {
	Data    []openAIBatchResponseWire `json:"data"`
	HasMore bool                      `json:"has_more"`
	LastID  *string                   `json:"last_id"`
}

type openAIBatchResultLineWire struct {
	CustomID string `json:"custom_id"`
	Response *struct {
		StatusCode int             `json:"status_code"`
		RequestID  *string         `json:"request_id"`
		Body       json.RawMessage `json:"body"`
	} `json:"response"`
	Error *struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

// openAIBatchResponseBody is the wire shape of a batch line's `response.body`
// (a Responses API response, plus the top-level `error` field the Responses
// API can include that responses.ResponsesAPIResponse doesn't model).
type openAIBatchResponseBody struct {
	responses.ResponsesAPIResponse
	Error *struct {
		Message string      `json:"message"`
		Type    string      `json:"type,omitempty"`
		Code    interface{} `json:"code,omitempty"`
	} `json:"error,omitempty"`
}

// openAIBatchResultsStream implements provider.BatchV4ItemResultStream over
// the concatenated NDJSON content of a batch's output and error files.
type openAIBatchResultsStream struct {
	ctx     context.Context
	batch   *Batch
	fileIDs []string
	fileIdx int
	headers map[string]string
	scanner *bufio.Scanner
	rc      io.ReadCloser
	err     error
}

func newOpenAIBatchResultsStream(ctx context.Context, b *Batch, fileIDs []string, headers map[string]string) *openAIBatchResultsStream {
	return &openAIBatchResultsStream{ctx: ctx, batch: b, fileIDs: fileIDs, headers: headers}
}

func (s *openAIBatchResultsStream) advanceFile() (bool, error) {
	if s.rc != nil {
		_ = s.rc.Close()
		s.rc = nil
	}
	if s.fileIdx >= len(s.fileIDs) {
		return false, nil
	}
	fileID := s.fileIDs[s.fileIdx]
	s.fileIdx++

	httpResp, err := s.batch.provider.client.DoStream(s.ctx, internalhttp.Request{
		Method:  http.MethodGet,
		Path:    "/files/" + providerutils.EncodePathSegment(fileID) + "/content",
		Headers: s.headers,
	})
	if err != nil {
		return false, s.batch.handleError(err)
	}
	s.rc = httpResp.Body
	scanner := bufio.NewScanner(s.rc)
	scanner.Buffer(make([]byte, 64*1024), 32*1024*1024)
	s.scanner = scanner
	return true, nil
}

func (s *openAIBatchResultsStream) Next() (*provider.BatchV4ItemResult, error) {
	for {
		if s.scanner == nil {
			ok, err := s.advanceFile()
			if err != nil {
				s.err = err
				return nil, err
			}
			if !ok {
				return nil, io.EOF
			}
		}

		if !s.scanner.Scan() {
			if err := s.scanner.Err(); err != nil {
				s.err = err
				return nil, err
			}
			s.scanner = nil
			continue
		}

		line := bytes.TrimSpace(s.scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var wire openAIBatchResultLineWire
		if err := json.Unmarshal(line, &wire); err != nil {
			s.err = err
			return nil, err
		}
		return convertOpenAIBatchResultLine(wire), nil
	}
}

func convertOpenAIBatchResultLine(wire openAIBatchResultLineWire) *provider.BatchV4ItemResult {
	if wire.Error != nil {
		batchErr := &provider.BatchError{Message: wire.Error.Message, Code: wire.Error.Code}
		status := provider.BatchItemFailed
		switch wire.Error.Code {
		case "batch_cancelled":
			status = provider.BatchItemCancelled
		case "batch_expired":
			status = provider.BatchItemExpired
		}
		return &provider.BatchV4ItemResult{Type: provider.BatchRequestTypeText, ID: wire.CustomID, Status: status, Error: batchErr}
	}

	if wire.Response == nil {
		return &provider.BatchV4ItemResult{
			Type:   provider.BatchRequestTypeText,
			ID:     wire.CustomID,
			Status: provider.BatchItemFailed,
			Error:  &provider.BatchError{Message: "OpenAI returned a batch result without a response or error.", Code: "invalid_batch_result"},
		}
	}

	if wire.Response.StatusCode < 200 || wire.Response.StatusCode >= 300 {
		return &provider.BatchV4ItemResult{
			Type:   provider.BatchRequestTypeText,
			ID:     wire.CustomID,
			Status: provider.BatchItemFailed,
			Error:  convertOpenAIBatchErrorResponse(wire.Response.Body, wire.Response.StatusCode),
		}
	}

	genResult, batchErr := convertOpenAIBatchResponseBodyBytes(wire.Response.Body)
	if batchErr != nil {
		return &provider.BatchV4ItemResult{Type: provider.BatchRequestTypeText, ID: wire.CustomID, Status: provider.BatchItemFailed, Error: batchErr}
	}
	return &provider.BatchV4ItemResult{Type: provider.BatchRequestTypeText, ID: wire.CustomID, Status: provider.BatchItemSucceeded, TextResult: genResult}
}

func convertOpenAIBatchErrorResponse(body json.RawMessage, statusCode int) *provider.BatchError {
	var envelope struct {
		Error struct {
			Message string      `json:"message"`
			Type    string      `json:"type"`
			Code    interface{} `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(body, &envelope); err != nil || envelope.Error.Message == "" {
		return &provider.BatchError{
			Message:    fmt.Sprintf("OpenAI batch request failed with status code %d.", statusCode),
			StatusCode: statusCode,
		}
	}
	code := ""
	if envelope.Error.Code != nil {
		code = fmt.Sprintf("%v", envelope.Error.Code)
	}
	return &provider.BatchError{Message: envelope.Error.Message, Type: envelope.Error.Type, Code: code, StatusCode: statusCode}
}

// convertOpenAIBatchResponseBodyBytes decodes and converts one succeeded
// batch line's response.body, reusing ResponsesLanguageModel.convertResponse
// for content conversion (that method only reads its resp/store/
// webSearchToolName/providerOptionsName parameters, so a zero-value receiver
// is safe here — there is no original request context for a batch result).
func convertOpenAIBatchResponseBodyBytes(body json.RawMessage) (*types.GenerateResult, *provider.BatchError) {
	var resp openAIBatchResponseBody
	if err := json.Unmarshal(body, &resp); err != nil {
		return nil, &provider.BatchError{Message: "OpenAI returned an invalid Responses batch result.", Code: "invalid_response"}
	}
	if resp.Error != nil {
		code := ""
		if resp.Error.Code != nil {
			code = fmt.Sprintf("%v", resp.Error.Code)
		}
		return nil, &provider.BatchError{Message: resp.Error.Message, Type: resp.Error.Type, Code: code}
	}
	if resp.Output == nil {
		message := "OpenAI Responses returned no output."
		if resp.IncompleteDetails != nil && resp.IncompleteDetails.Reason != "" {
			message = fmt.Sprintf("OpenAI Responses returned no output (%s).", resp.IncompleteDetails.Reason)
		}
		return nil, &provider.BatchError{Message: message, Code: "invalid_response"}
	}

	// The shared ResponsesLanguageModel.convertResponse switch (used for
	// both streaming and non-streaming generate calls) has no default case:
	// an output-item type it doesn't recognize is silently dropped rather
	// than surfaced. A batch result has no interactive warning channel, so
	// TS's batch-local converter uses a separate, stricter allowlist and
	// fails the whole item with "unsupported_content" instead — matched
	// here by pre-scanning the raw output items before delegating to the
	// shared converter, without altering that converter's own (intentional)
	// non-batch behavior.
	if unsupportedType, found := firstUnsupportedOpenAIBatchOutputItemType(resp.Output); found {
		return nil, &provider.BatchError{
			Message: fmt.Sprintf("OpenAI returned an unsupported %q output item in an AI SDK text batch.", unsupportedType),
			Code:    "unsupported_content",
		}
	}

	var lm ResponsesLanguageModel
	// tools is nil: it is only used to expand the internal parallel tool
	// call wrapper (P1-5b), which batch requests never include.
	// approvalFromPrompt is nil: a batch result has no prior turns carrying
	// MCP approval responses (P1-5c).
	genResult, err := lm.convertResponse(resp.ResponsesAPIResponse, false, "", nil, nil, "openai")
	if err != nil {
		return nil, &provider.BatchError{Message: err.Error(), Code: "invalid_response"}
	}
	return genResult, nil
}

// openaiBatchKnownOutputItemTypes are the Responses output-item types TS's
// batch-local convertOpenAIBatchResult switch implements (anthropic-batch.ts
// mirrors this pattern with knownAnthropicBatchContentTypes): reasoning,
// message, function_call, custom_tool_call, web_search_call,
// file_search_call, code_interpreter_call. This is intentionally a
// different (stricter, and in the file_search_call/code_interpreter_call
// case, currently narrower) set than what the shared
// ResponsesLanguageModel.convertResponse switch implements for
// streaming/non-streaming generate calls.
var openaiBatchKnownOutputItemTypes = map[string]bool{
	"reasoning":             true,
	"message":               true,
	"function_call":         true,
	"custom_tool_call":      true,
	"web_search_call":       true,
	"file_search_call":      true,
	"code_interpreter_call": true,
}

// firstUnsupportedOpenAIBatchOutputItemType returns the type of the first
// output item whose "type" field is not in openaiBatchKnownOutputItemTypes,
// in array order (matching TS's switch, which fails on the first unmatched
// item it iterates to). Items that fail to decode even a bare "type" field
// are skipped here; the shared converter's own decoding surfaces that error.
func firstUnsupportedOpenAIBatchOutputItemType(output []json.RawMessage) (string, bool) {
	for _, raw := range output {
		var peek struct {
			Type string `json:"type"`
		}
		if err := json.Unmarshal(raw, &peek); err != nil {
			continue
		}
		if !openaiBatchKnownOutputItemTypes[peek.Type] {
			return peek.Type, true
		}
	}
	return "", false
}

func (s *openAIBatchResultsStream) Err() error { return s.err }

func (s *openAIBatchResultsStream) Close() error {
	if s.rc != nil {
		return s.rc.Close()
	}
	return nil
}
