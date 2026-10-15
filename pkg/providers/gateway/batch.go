package gateway

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// Batch implements provider.BatchV4 for the AI Gateway's async batch routes
// (`/batch/start`, `/batch/status`, `/batch/results`, `/batch/cancel`).
//
// The returned batch ID is the Gateway job id — provider-native batch ids
// stay server-side, so status, results and cancellation always route back
// through the Gateway job. Mirrors TypeScript's GatewayBatch
// (packages/gateway/src/gateway-batch.ts).
type Batch struct {
	provider *Provider
}

// NewBatch creates a new Gateway batch interface.
func NewBatch(p *Provider) *Batch {
	return &Batch{provider: p}
}

// ExperimentalBatch implements provider.BatchProvider for Provider, exposing
// the Gateway's async batch API.
func (p *Provider) ExperimentalBatch() provider.BatchV4 {
	return NewBatch(p)
}

// SpecificationVersion returns the batch interface specification version.
func (b *Batch) SpecificationVersion() string { return "v4" }

// Provider returns the batch interface's provider ID.
func (b *Batch) Provider() string { return "gateway.batch" }

// SupportedURLs reports that Gateway can pass through direct file URLs.
func (b *Batch) SupportedURLs() map[string][]string {
	return map[string][]string{"*/*": {`.*`}}
}

// DoStartBatch starts a durable batch of text-generation requests through the
// Gateway's async batch surface (`POST {baseURL}/batch/start`).
func (b *Batch) DoStartBatch(ctx context.Context, opts provider.BatchV4StartOptions) (*provider.BatchV4StartResult, error) {
	modelID, err := validateGatewayTextBatchRequests(opts.Requests)
	if err != nil {
		return nil, err
	}

	idempotencyKey, forwardedProviderOptions := extractGatewayBatchIdempotencyKey(opts.ProviderOptions)

	// TS combineHeaders(config, options.headers, {ai-model-id}, o11y, idempotency-key)
	headers := gatewayRequestHeaders(ctx, opts.Headers, map[string]string{"ai-model-id": modelID})
	if idempotencyKey != "" {
		headers["idempotency-key"] = idempotencyKey
	}

	wireRequests := make([]map[string]interface{}, 0, len(opts.Requests))
	for _, req := range opts.Requests {
		wireReq, buildErr := b.gatewayBatchWireRequest(req)
		if buildErr != nil {
			return nil, buildErr
		}
		wireRequests = append(wireRequests, wireReq)
	}

	body := map[string]interface{}{"requests": wireRequests}
	if opts.WebhookURL != "" {
		body["callbackUrl"] = opts.WebhookURL
	}
	if len(forwardedProviderOptions) > 0 {
		body["providerOptions"] = forwardedProviderOptions
	}

	var response gatewayBatchStartResponse
	if err := b.provider.client.DoJSON(ctx, internalhttp.Request{
		Method:  http.MethodPost,
		Path:    "/batch/start",
		Body:    body,
		Headers: headers,
	}, &response); err != nil {
		return nil, b.handleError(ctx, err)
	}

	warnings := make([]provider.BatchV4Warning, 0, len(response.Warnings))
	for _, w := range response.Warnings {
		warnings = append(warnings, provider.BatchV4Warning{RequestID: w.RequestID, Warning: w.Warning})
	}

	return &provider.BatchV4StartResult{
		BatchV4Status: response.toStatus(),
		BatchID:       response.BatchID,
		Warnings:      warnings,
	}, nil
}

// DoGetBatchStatus retrieves the lifecycle status of a Gateway batch job
// (`POST {baseURL}/batch/status`).
func (b *Batch) DoGetBatchStatus(ctx context.Context, opts provider.BatchV4OperationOptions) (*provider.BatchV4Status, error) {
	headers := gatewayRequestHeaders(ctx, opts.Headers, nil)

	var response gatewayBatchStatusFields
	if err := b.provider.client.DoJSON(ctx, internalhttp.Request{
		Method:  http.MethodPost,
		Path:    "/batch/status",
		Body:    map[string]interface{}{"batchId": opts.BatchID},
		Headers: headers,
	}, &response); err != nil {
		return nil, b.handleError(ctx, err)
	}

	status := response.toStatus()
	return &status, nil
}

// DoCancelBatch requests cancellation of a Gateway batch job (`POST
// {baseURL}/batch/cancel`). Status and partial results remain separate reads.
func (b *Batch) DoCancelBatch(ctx context.Context, opts provider.BatchV4OperationOptions) (*provider.BatchV4CancelResult, error) {
	headers := gatewayRequestHeaders(ctx, opts.Headers, nil)

	var response gatewayBatchStatusFields
	if err := b.provider.client.DoJSON(ctx, internalhttp.Request{
		Method:  http.MethodPost,
		Path:    "/batch/cancel",
		Body:    map[string]interface{}{"batchId": opts.BatchID},
		Headers: headers,
	}, &response); err != nil {
		return nil, b.handleError(ctx, err)
	}

	return &provider.BatchV4CancelResult{ProviderMetadata: copyGatewayProviderMetadata(response.ProviderMetadata)}, nil
}

// DoGetBatchResults streams the per-request results of a terminal Gateway
// batch job (`POST {baseURL}/batch/results`, `application/x-ndjson`: one item
// result JSON object per line). The route responds with an error while the
// batch is non-terminal.
func (b *Batch) DoGetBatchResults(ctx context.Context, opts provider.BatchV4OperationOptions) (provider.BatchV4ItemResultStream, error) {
	headers := gatewayRequestHeaders(ctx, opts.Headers, nil)

	maxLineBytes, err := b.provider.config.BatchResultDownloads.ResolveMaxLineBytes()
	if err != nil {
		return nil, err
	}

	httpResp, err := b.provider.client.DoStream(ctx, internalhttp.Request{
		Method:  http.MethodPost,
		Path:    "/batch/results",
		Body:    map[string]interface{}{"batchId": opts.BatchID},
		Headers: headers,
	})
	if err != nil {
		return nil, b.handleError(ctx, err)
	}

	return newGatewayBatchResultsStream(httpResp.Body, strings.TrimRight(b.provider.config.BaseURL, "/")+"/batch/results", maxLineBytes), nil
}

func (b *Batch) handleError(ctx context.Context, err error) error {
	// TS isAbortOrTimeoutError: an aborted or timed-out batch call keeps its
	// own error (a start may still have been accepted server-side) instead of
	// becoming a retryable Gateway 500 / client-timeout error.
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return err
	}
	lm := &LanguageModel{provider: b.provider}
	return lm.handleErrorWithContext(ctx, err)
}

// gatewayBatchWireRequest converts one normalized batch request into the
// Gateway's wire shape ({id, type, modelId, options}). Only text requests are
// supported; validateGatewayTextBatchRequests rejects anything else before
// this is reached.
func (b *Batch) gatewayBatchWireRequest(req provider.BatchV4Request) (map[string]interface{}, error) {
	if req.Type != provider.BatchRequestTypeText || req.Text == nil {
		return nil, unsupportedGatewayBatchRequestType(req.Type)
	}

	lm := &LanguageModel{provider: b.provider, modelID: req.Text.ModelID}
	optionsBody, err := lm.buildRequestBody(&req.Text.Options, false)
	if err != nil {
		return nil, err
	}

	return map[string]interface{}{
		"id":      req.Text.ID,
		"type":    "text",
		"modelId": req.Text.ModelID,
		"options": optionsBody,
	}, nil
}

// validateGatewayTextBatchRequests mirrors TypeScript's
// assertTextBatchRequests + validateSingleModel: every request must be text,
// and every request in the batch must use the same model.
func validateGatewayTextBatchRequests(requests []provider.BatchV4Request) (string, error) {
	if len(requests) == 0 {
		return "", &providererrors.InvalidArgumentError{
			Field:   "requests",
			Message: "The AI Gateway Batch API requires at least one request.",
		}
	}

	var modelID string
	for _, req := range requests {
		if req.Type != provider.BatchRequestTypeText || req.Text == nil {
			return "", unsupportedGatewayBatchRequestType(req.Type)
		}
		if modelID == "" {
			modelID = req.Text.ModelID
			continue
		}
		if req.Text.ModelID != modelID {
			return "", &providererrors.InvalidArgumentError{
				Field: "requests",
				Message: fmt.Sprintf(
					"The AI Gateway Batch API requires all requests in a batch to use the same model. Found %q and %q.",
					modelID, req.Text.ModelID,
				),
			}
		}
	}
	return modelID, nil
}

func unsupportedGatewayBatchRequestType(requestType provider.BatchRequestType) error {
	return &providererrors.UnsupportedFunctionalityError{
		Functionality: fmt.Sprintf("batch request type: %s", requestType),
		Message:       fmt.Sprintf("The AI Gateway Batch API does not support batch requests with type %q.", requestType),
	}
}

// extractGatewayBatchIdempotencyKey extracts the optional Gateway idempotency
// key from providerOptions.gateway.idempotencyKey. It is sent as the
// idempotency-key request header — the Gateway's replay contract for batch
// starts — and stripped from the forwarded body so equivalent retries don't
// produce different request digests.
func extractGatewayBatchIdempotencyKey(providerOptions map[string]interface{}) (string, map[string]interface{}) {
	if len(providerOptions) == 0 {
		return "", providerOptions
	}
	gatewayRaw, ok := providerOptions["gateway"]
	if !ok {
		return "", providerOptions
	}
	gatewayMap, ok := gatewayRaw.(map[string]interface{})
	if !ok {
		return "", providerOptions
	}
	keyRaw, ok := gatewayMap["idempotencyKey"]
	if !ok {
		return "", providerOptions
	}
	key, _ := keyRaw.(string)

	restGateway := make(map[string]interface{}, len(gatewayMap))
	for k, v := range gatewayMap {
		if k == "idempotencyKey" {
			continue
		}
		restGateway[k] = v
	}
	restProviderOptions := make(map[string]interface{}, len(providerOptions))
	for k, v := range providerOptions {
		restProviderOptions[k] = v
	}
	if len(restGateway) == 0 {
		delete(restProviderOptions, "gateway")
	} else {
		restProviderOptions["gateway"] = restGateway
	}
	return key, restProviderOptions
}

// --- Wire types -------------------------------------------------------

type gatewayBatchRequestCountsWire struct {
	Total     *int `json:"total"`
	Pending   *int `json:"pending"`
	Completed *int `json:"completed"`
	Failed    *int `json:"failed"`
}

type gatewayBatchErrorWire struct {
	Message    string  `json:"message"`
	Type       *string `json:"type"`
	Code       *string `json:"code"`
	StatusCode *int    `json:"statusCode"`
}

func (e *gatewayBatchErrorWire) toBatchError() *provider.BatchError {
	if e == nil {
		return nil
	}
	out := &provider.BatchError{Message: e.Message}
	if e.Type != nil {
		out.Type = *e.Type
	}
	if e.Code != nil {
		out.Code = *e.Code
	}
	if e.StatusCode != nil {
		out.StatusCode = *e.StatusCode
	}
	return out
}

type gatewayBatchStatusFields struct {
	Status           string                            `json:"status"`
	RawStatus        *string                           `json:"rawStatus"`
	RequestCounts    *gatewayBatchRequestCountsWire    `json:"requestCounts"`
	Error            *gatewayBatchErrorWire            `json:"error"`
	CreatedAt        *string                           `json:"createdAt"`
	ExpiresAt        *string                           `json:"expiresAt"`
	ProviderMetadata map[string]map[string]interface{} `json:"providerMetadata"`
}

func (f gatewayBatchStatusFields) toStatus() provider.BatchV4Status {
	status := provider.BatchV4Status{Status: provider.BatchStatusValue(f.Status)}
	if f.RawStatus != nil {
		status.RawStatus = *f.RawStatus
	}
	// TS omits requestCounts unless all four counters are present; Go must
	// not fabricate zeros for the missing ones.
	if rc := f.RequestCounts; rc != nil && rc.Total != nil && rc.Pending != nil && rc.Completed != nil && rc.Failed != nil {
		status.RequestCounts = &provider.BatchRequestCounts{Total: *rc.Total, Pending: *rc.Pending, Completed: *rc.Completed, Failed: *rc.Failed}
	}
	status.Error = f.Error.toBatchError()
	if f.CreatedAt != nil {
		status.CreatedAt = *f.CreatedAt
	}
	if f.ExpiresAt != nil {
		status.ExpiresAt = *f.ExpiresAt
	}
	status.ProviderMetadata = copyGatewayProviderMetadata(f.ProviderMetadata)
	return status
}

func copyGatewayProviderMetadata(in map[string]map[string]interface{}) map[string]interface{} {
	if len(in) == 0 {
		return nil
	}
	out := make(map[string]interface{}, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}

type gatewayBatchWarningWire struct {
	RequestID string        `json:"requestId,omitempty"`
	Warning   types.Warning `json:"warning"`
}

type gatewayBatchStartResponse struct {
	gatewayBatchStatusFields
	BatchID  string                    `json:"batchId"`
	Warnings []gatewayBatchWarningWire `json:"warnings"`
}

// gatewayBatchItemResultBody decodes a succeeded text batch item's `result`
// field. Its shape matches the non-streaming `/language-model` response
// decoded by LanguageModel.DoGenerate: warnings are decoded separately as raw
// JSON so an absent field defaults to empty and a malformed one does not fail
// the whole line.
type gatewayBatchItemResultBody struct {
	types.GenerateResult
	Warnings json.RawMessage `json:"warnings,omitempty"`
}

type gatewayBatchItemResultLine struct {
	Type             string                            `json:"type"`
	ID               string                            `json:"id"`
	Status           string                            `json:"status"`
	Result           *gatewayBatchItemResultBody       `json:"result,omitempty"`
	Error            *gatewayBatchErrorWire            `json:"error,omitempty"`
	ProviderMetadata map[string]map[string]interface{} `json:"providerMetadata,omitempty"`
}

func (w gatewayBatchItemResultLine) toItemResult() *provider.BatchV4ItemResult {
	item := &provider.BatchV4ItemResult{
		Type:             provider.BatchRequestType(w.Type),
		ID:               w.ID,
		Status:           provider.BatchItemStatus(w.Status),
		Error:            w.Error.toBatchError(),
		ProviderMetadata: copyGatewayProviderMetadata(w.ProviderMetadata),
	}
	if item.Status == provider.BatchItemSucceeded && w.Result != nil {
		result := w.Result.GenerateResult
		result.Warnings = parseGatewayWarnings(w.Result.Warnings)
		item.TextResult = &result
	}
	return item
}

// gatewayBatchResultsStream implements provider.BatchV4ItemResultStream over
// the NDJSON body returned by `/batch/results`.
type gatewayBatchResultsStream struct {
	scanner      *bufio.Scanner
	body         io.ReadCloser
	url          string
	maxLineBytes int
	err          error
}

func newGatewayBatchResultsStream(body io.ReadCloser, url string, maxLineBytes int) *gatewayBatchResultsStream {
	// Individual result lines (a full LanguageModelV4GenerateResult) may
	// exceed bufio.Scanner's 64KiB default; rows are bounded by maxLineBytes.
	scanner := providerutils.NewJSONLinesScanner(body, maxLineBytes)
	return &gatewayBatchResultsStream{scanner: scanner, body: body, url: url, maxLineBytes: maxLineBytes}
}

func (s *gatewayBatchResultsStream) Next() (*provider.BatchV4ItemResult, error) {
	for {
		if !s.scanner.Scan() {
			if err := s.scanner.Err(); err != nil {
				err = providerutils.JSONLinesScanError(err, s.url, s.maxLineBytes)
				s.err = err
				_ = s.body.Close()
				return nil, err
			}
			return nil, io.EOF
		}
		line := bytes.TrimSpace(s.scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		var wire gatewayBatchItemResultLine
		if err := json.Unmarshal(line, &wire); err != nil {
			s.err = err
			return nil, err
		}
		return wire.toItemResult(), nil
	}
}

func (s *gatewayBatchResultsStream) Err() error {
	return s.err
}

func (s *gatewayBatchResultsStream) Close() error {
	return s.body.Close()
}
