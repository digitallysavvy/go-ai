package anthropic

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	stdhttp "net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/internal/fileutil"
	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// Batch implements provider.BatchV4 for the Anthropic Message Batches API
// (`/v1/messages/batches`). Mirrors TypeScript's AnthropicBatch
// (packages/anthropic/src/anthropic-batch.ts).
type Batch struct {
	provider *Provider
}

// NewBatch creates a new Anthropic batch interface.
func NewBatch(p *Provider) *Batch { return &Batch{provider: p} }

// ExperimentalBatch implements provider.BatchProvider for Provider, exposing
// the Anthropic Message Batches API. Mirrors TypeScript's
// `provider.experimental_batch = createBatch`.
func (p *Provider) ExperimentalBatch() provider.BatchV4 { return NewBatch(p) }

// SpecificationVersion returns the batch interface specification version.
func (b *Batch) SpecificationVersion() string { return "v4" }

// Provider returns the batch interface's provider ID, e.g. "anthropic.batch".
func (b *Batch) Provider() string { return b.provider.Name() + ".batch" }

// SupportedURLs returns the same URL patterns the underlying language model
// accepts directly, matching TS `supportedUrls: () => supportedUrls` shared
// between AnthropicLanguageModel and AnthropicBatch.
func (b *Batch) SupportedURLs() map[string][]string {
	lm := &LanguageModel{provider: b.provider}
	return lm.SupportedURLs()
}

var anthropicBatchRequestIDPattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,64}$`)

// DoStartBatch starts a Message Batch (`POST /messages/batches`).
func (b *Batch) DoStartBatch(ctx context.Context, opts provider.BatchV4StartOptions) (*provider.BatchV4StartResult, error) {
	if err := validateAnthropicTextBatchRequests(opts.Requests); err != nil {
		return nil, err
	}
	if err := validateAnthropicBatchRequestIDs(opts.Requests); err != nil {
		return nil, err
	}

	providerName := b.provider.Name()
	explicitBatchBetas := anthropicBatchProviderBetas(providerName, opts.ProviderOptions)
	betas := betaSet{}
	betas.add(explicitBatchBetas...)

	var warnings []provider.BatchV4Warning
	if opts.WebhookURL != "" {
		warnings = append(warnings, provider.BatchV4Warning{
			Warning: types.Warning{
				Type:    "unsupported",
				Feature: "webhookUrl",
				Details: "The Anthropic Message Batches API does not support completion webhooks.",
			},
		})
	}

	preparedRequests := make([]map[string]interface{}, 0, len(opts.Requests))
	for _, req := range opts.Requests {
		textReq := req.Text

		requestBetas := anthropicBatchProviderBetas(providerName, textReq.Options.ProviderOptions)
		if len(requestBetas) > 0 {
			return nil, &providererrors.UnsupportedFunctionalityError{
				Functionality: "per-request providerOptions.anthropic.anthropicBeta",
				Message: fmt.Sprintf(
					"Anthropic Message Batches do not support per-request betas "+
						"(request %q). Set providerOptions.anthropic.anthropicBeta "+
						"on startBatch instead.", textReq.ID),
			}
		}

		lm := NewLanguageModel(b.provider, textReq.ModelID, nil)
		prepared, err := lm.prepareRequest(&textReq.Options, false)
		if err != nil {
			return nil, err
		}
		if prepared.usesJSONResponseTool {
			return nil, &providererrors.UnsupportedFunctionalityError{
				Functionality: "batch responseFormat JSON-tool fallback",
				Message: fmt.Sprintf(
					"Anthropic Message Batches cannot decode the JSON-tool structured-output fallback "+
						"(request %q) because batch results are retrieved independently of the start call. "+
						"Use a model that supports native output_format structured outputs.", textReq.ID),
			}
		}
		if aliased := findAliasedAnthropicProviderTool(textReq.Options.Tools, prepared.toolsetNames); aliased != "" {
			return nil, &providererrors.UnsupportedFunctionalityError{
				Functionality: "aliased provider tool names in batches",
				Message: fmt.Sprintf(
					"Anthropic Message Batches cannot restore the custom provider-tool name "+
						"%q when results are retrieved independently of the start call "+
						"(request %q). Use the provider's canonical tool name.", aliased, textReq.ID),
			}
		}

		body := lm.transformRequestBody(prepared.body, prepared.betas, false)
		if err := validateAnthropicBatchBody(body, textReq.ID); err != nil {
			return nil, err
		}

		preparedRequests = append(preparedRequests, map[string]interface{}{
			"custom_id": textReq.ID,
			"params":    body,
		})
		betas.add(prepared.betas...)
		for _, w := range prepared.warnings {
			warnings = append(warnings, provider.BatchV4Warning{RequestID: textReq.ID, Warning: w})
		}
	}

	headers := b.startBatchHeaders(betas.list, opts.Headers)

	var response anthropicBatchResponseWire
	if err := b.provider.client.DoJSON(ctx, internalhttp.Request{
		Method:  stdhttp.MethodPost,
		Path:    "/messages/batches",
		Body:    map[string]interface{}{"requests": preparedRequests},
		Headers: headers,
	}, &response); err != nil {
		return nil, b.handleError(err)
	}

	return &provider.BatchV4StartResult{
		BatchV4Status: response.toStatus(),
		BatchID:       response.ID,
		Warnings:      warnings,
	}, nil
}

// DoGetBatchStatus retrieves a batch's lifecycle status
// (`GET /messages/batches/{id}`).
func (b *Batch) DoGetBatchStatus(ctx context.Context, opts provider.BatchV4OperationOptions) (*provider.BatchV4Status, error) {
	batch, err := b.retrieveBatch(ctx, opts.BatchID, opts.Headers)
	if err != nil {
		return nil, err
	}
	status := batch.toStatus()
	return &status, nil
}

// DoCancelBatch requests cancellation of a batch
// (`POST /messages/batches/{id}/cancel`).
func (b *Batch) DoCancelBatch(ctx context.Context, opts provider.BatchV4OperationOptions) (*provider.BatchV4CancelResult, error) {
	var response anthropicBatchResponseWire
	if err := b.provider.client.DoJSON(ctx, internalhttp.Request{
		Method:  stdhttp.MethodPost,
		Path:    "/messages/batches/" + providerutils.EncodePathSegment(opts.BatchID) + "/cancel",
		Body:    map[string]interface{}{},
		Headers: opts.Headers,
	}, &response); err != nil {
		return nil, b.handleError(err)
	}
	return &provider.BatchV4CancelResult{}, nil
}

// DoListBatches lists batches (`GET /messages/batches`).
func (b *Batch) DoListBatches(ctx context.Context, opts provider.BatchV4ListOptions) (*provider.BatchV4ListResult, error) {
	query := map[string]string{}
	if opts.Limit > 0 {
		query["limit"] = strconv.Itoa(opts.Limit)
	}
	if opts.Cursor != "" {
		query["after_id"] = opts.Cursor
	}

	var response anthropicBatchListResponseWire
	if err := b.provider.client.DoJSON(ctx, internalhttp.Request{
		Method:  stdhttp.MethodGet,
		Path:    "/messages/batches",
		Query:   query,
		Headers: opts.Headers,
	}, &response); err != nil {
		return nil, b.handleError(err)
	}

	batches := make([]provider.BatchV4ListItem, 0, len(response.Data))
	for _, item := range response.Data {
		batches = append(batches, provider.BatchV4ListItem{BatchV4Status: item.toStatus(), BatchID: item.ID})
	}

	result := &provider.BatchV4ListResult{Batches: batches}
	if response.HasMore && response.LastID != nil {
		result.NextCursor = *response.LastID
	}
	return result, nil
}

// DoGetBatchResults streams the per-request results of a terminal batch by
// following its `results_url` (a JSONL file). Mirrors TypeScript's
// AnthropicBatch.doGetBatchResults.
func (b *Batch) DoGetBatchResults(ctx context.Context, opts provider.BatchV4OperationOptions) (provider.BatchV4ItemResultStream, error) {
	batch, err := b.retrieveBatch(ctx, opts.BatchID, opts.Headers)
	if err != nil {
		return nil, err
	}

	if batch.toStatus().Status == provider.BatchStatusPending {
		return nil, &providererrors.InvalidArgumentError{
			Field:   "batchId",
			Message: fmt.Sprintf("Anthropic batch %q is not complete.", opts.BatchID),
		}
	}
	if batch.ArchivedAt != nil {
		return nil, &providererrors.InvalidArgumentError{
			Field:   "batchId",
			Message: fmt.Sprintf("Anthropic batch %q results are no longer available.", opts.BatchID),
		}
	}
	if batch.ResultsURL == nil || *batch.ResultsURL == "" {
		return nil, providererrors.NewInvalidResponseDataError(batch,
			fmt.Sprintf("Anthropic batch %q completed without batch output.", opts.BatchID))
	}

	resultsURL := *batch.ResultsURL

	// results_url is provider-response data, not developer-configured
	// config, and commonly points off the Anthropic API host (e.g. a signed
	// file-storage URL). Mirrors TS getFromApi({validateUrl: true,
	// credentialedOrigin: baseURL, trustedOrigin: baseURL}): only a hop that
	// stays on the configured base URL's origin is dialed through the plain
	// transport and receives the API key/version/custom headers; every other
	// host is validated against the SSRF blocklist, DNS-pinned, and never
	// sees credentials, matching TS's outgoingHeaders = {} for a
	// non-same-origin hop.
	baseURL := b.provider.config.BaseURL
	isTrusted := func(raw string) bool { return providerutils.IsSameOrigin(raw, baseURL) }
	downloadOpts := fileutil.DefaultDownloadOptions()
	downloadOpts.URLValidator = fileutil.TrustedURLValidator(isTrusted)
	downloadOpts.Transport = fileutil.TrustRoutingTransport(isTrusted, nil, fileutil.SafeTransport())
	if err := downloadOpts.URLValidator(resultsURL); err != nil {
		return nil, err
	}

	httpReq, err := stdhttp.NewRequestWithContext(ctx, stdhttp.MethodGet, resultsURL, nil)
	if err != nil {
		return nil, err
	}
	if isTrusted(resultsURL) {
		for k, v := range b.rawRequestHeaders(opts.Headers) {
			httpReq.Header.Set(k, v)
		}
	}

	client := fileutil.NewDownloadClient(resultsURL, downloadOpts)
	httpResp, err := client.Do(httpReq)
	if err != nil {
		return nil, b.handleError(err)
	}
	if httpResp.StatusCode >= 400 {
		defer httpResp.Body.Close() //nolint:errcheck
		body, _ := fileutil.ReadResponseWithSizeLimit(httpResp, resultsURL, downloadOpts.MaxSize)
		return nil, b.handleError(&internalhttp.HTTPStatusError{StatusCode: httpResp.StatusCode, Headers: httpResp.Header, Body: body})
	}

	return newAnthropicBatchResultsStream(httpResp.Body, b.provider), nil
}

// retrieveBatch fetches one batch's current representation
// (`GET /messages/batches/{id}`).
func (b *Batch) retrieveBatch(ctx context.Context, batchID string, headers map[string]string) (*anthropicBatchResponseWire, error) {
	var response anthropicBatchResponseWire
	if err := b.provider.client.DoJSON(ctx, internalhttp.Request{
		Method:  stdhttp.MethodGet,
		Path:    "/messages/batches/" + providerutils.EncodePathSegment(batchID),
		Headers: headers,
	}, &response); err != nil {
		return nil, b.handleError(err)
	}
	return &response, nil
}

func (b *Batch) handleError(err error) error {
	lm := &LanguageModel{provider: b.provider}
	return lm.handleError(err)
}

// startBatchHeaders builds the create-batch request headers, combining the
// combined beta set into a single anthropic-beta header (TS
// getStartBatchHeaders). The computed anthropic-beta value always wins over
// any anthropic-beta the caller passed in headers, mirroring TS
// combineHeaders(normalizeHeaders(await this.getBatchHeaders(headers)), {
// 'anthropic-beta': ... }) — later arguments win, and the computed value is
// the later argument.
func (b *Batch) startBatchHeaders(betas []string, headers map[string]string) map[string]string {
	out := map[string]string{}
	for k, v := range headers {
		out[k] = v
	}
	if len(betas) > 0 {
		out["anthropic-beta"] = strings.Join(betas, ",")
	}
	return out
}

// rawRequestHeaders builds the authentication headers used for a raw
// (non-client-relative) request, such as following a results_url returned by
// the batches API. Duplicates the header construction in New() because
// *http.Client (returned by Client.HTTPClient()) does not carry the
// provider's default headers.
func (b *Batch) rawRequestHeaders(extra map[string]string) map[string]string {
	headers := map[string]string{}
	if b.provider.config.APIKey != "" {
		headers["x-api-key"] = b.provider.config.APIKey
	}
	if !b.provider.config.OmitAPIVersionHeader {
		apiVersion := b.provider.config.APIVersion
		if apiVersion == "" {
			apiVersion = DefaultAPIVersion
		}
		headers["anthropic-version"] = apiVersion
	}
	for k, v := range b.provider.config.Headers {
		headers[k] = v
	}
	for k, v := range extra {
		headers[k] = v
	}
	return headers
}

// anthropicBatchProviderBetas extracts providerOptions.anthropic.anthropicBeta
// (or the equivalent providerOptions.<custom-name>.anthropicBeta when the
// provider was constructed with a custom Name), mirroring TS
// getAnthropicBatchProviderBetas's canonical+custom option merge.
func anthropicBatchProviderBetas(providerName string, providerOptions map[string]interface{}) []string {
	betas := anthropicBetaOptionList(providerOptions, "anthropic")
	if providerName != "anthropic" {
		if opts, ok := providerOptions[providerName].(map[string]interface{}); ok {
			if _, present := opts["anthropicBeta"]; present {
				betas = anthropicBetaOptionList(providerOptions, providerName)
			}
		}
	}
	return betas
}

func anthropicBetaOptionList(providerOptions map[string]interface{}, key string) []string {
	opts, ok := providerOptions[key].(map[string]interface{})
	if !ok {
		return nil
	}
	raw, ok := opts["anthropicBeta"].([]interface{})
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		if s, ok := v.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

// validateAnthropicTextBatchRequests mirrors TS assertTextBatchRequests: every
// request in a batch must be a text request.
func validateAnthropicTextBatchRequests(requests []provider.BatchV4Request) error {
	for _, req := range requests {
		if req.Type != provider.BatchRequestTypeText || req.Text == nil {
			return &providererrors.UnsupportedFunctionalityError{
				Functionality: fmt.Sprintf("batch request type: %s", req.Type),
				Message:       fmt.Sprintf("The Anthropic Message Batches API does not support batch requests with type %q.", req.Type),
			}
		}
	}
	return nil
}

// validateAnthropicBatchRequestIDs mirrors TS validateRequestIds.
func validateAnthropicBatchRequestIDs(requests []provider.BatchV4Request) error {
	seen := map[string]bool{}
	for _, req := range requests {
		id := req.Text.ID
		if !anthropicBatchRequestIDPattern.MatchString(id) {
			return &providererrors.InvalidArgumentError{
				Field:   "requests",
				Message: fmt.Sprintf("Anthropic batch request ID %q must match ^[A-Za-z0-9_-]{1,64}$.", id),
			}
		}
		if seen[id] {
			return &providererrors.InvalidArgumentError{
				Field:   "requests",
				Message: fmt.Sprintf("Anthropic batch request IDs must be unique; duplicate ID %q.", id),
			}
		}
		seen[id] = true
	}
	return nil
}

// validateAnthropicBatchBody mirrors TS validateAnthropicBatchBody: batches
// don't support the speed option, at the top level or in any fallback.
func validateAnthropicBatchBody(body map[string]interface{}, requestID string) error {
	if body["speed"] != nil {
		return &providererrors.UnsupportedFunctionalityError{
			Functionality: "providerOptions.anthropic.speed",
			Message:       fmt.Sprintf("Anthropic Message Batches do not support speed (request %q).", requestID),
		}
	}
	if fallbacks, ok := body["fallbacks"].([]FallbackConfig); ok {
		for _, fb := range fallbacks {
			if fb.Speed != "" {
				return &providererrors.UnsupportedFunctionalityError{
					Functionality: "providerOptions.anthropic.fallbacks[].speed",
					Message:       fmt.Sprintf("Anthropic Message Batches do not support fallback speed (request %q).", requestID),
				}
			}
		}
	}
	return nil
}

// findAliasedAnthropicProviderTool returns the SDK-facing name of the first
// provider-defined tool whose Anthropic wire name (per toolsetNames) differs
// from its SDK name, or "" when none is aliased. Mirrors TS's check against
// prepared.toolNameMapping.toProviderToolName: batch results can't restore an
// aliased provider tool call's original SDK name because they're retrieved
// independently of the start call's tool list.
func findAliasedAnthropicProviderTool(tools []types.Tool, toolsetNames map[string]string) string {
	for _, t := range tools {
		if t.Type != types.ToolTypeProviderDefined {
			continue
		}
		if mapped, ok := toolsetNames[t.Name]; ok && mapped != t.Name {
			return t.Name
		}
	}
	return ""
}

// --- Wire types -------------------------------------------------------

type anthropicBatchRequestCountsWire struct {
	Processing int `json:"processing"`
	Succeeded  int `json:"succeeded"`
	Errored    int `json:"errored"`
	Canceled   int `json:"canceled"`
	Expired    int `json:"expired"`
}

type anthropicBatchResponseWire struct {
	ID                string                          `json:"id"`
	Type              string                          `json:"type"`
	ProcessingStatus  string                          `json:"processing_status"`
	RequestCounts     anthropicBatchRequestCountsWire `json:"request_counts"`
	CreatedAt         string                          `json:"created_at"`
	ExpiresAt         string                          `json:"expires_at"`
	ArchivedAt        *string                         `json:"archived_at"`
	CancelInitiatedAt *string                         `json:"cancel_initiated_at"`
	EndedAt           *string                         `json:"ended_at"`
	ResultsURL        *string                         `json:"results_url"`
}

func (r anthropicBatchResponseWire) toStatus() provider.BatchV4Status {
	counts := r.RequestCounts
	total := counts.Processing + counts.Succeeded + counts.Errored + counts.Canceled + counts.Expired
	pending := counts.Processing
	completed := counts.Succeeded
	failed := counts.Errored + counts.Canceled + counts.Expired
	return provider.BatchV4Status{
		Status:        mapAnthropicBatchStatus(r.ProcessingStatus),
		RawStatus:     r.ProcessingStatus,
		RequestCounts: providerutils.NormalizeBatchRequestCounts(&total, &pending, &completed, &failed),
		CreatedAt:     r.CreatedAt,
		ExpiresAt: r.ExpiresAt,
		ProviderMetadata: map[string]interface{}{
			"anthropic": map[string]interface{}{
				"archivedAt":        r.ArchivedAt,
				"cancelInitiatedAt": r.CancelInitiatedAt,
				"endedAt":           r.EndedAt,
				"requestCounts":     counts,
				"resultsUrl":        r.ResultsURL,
			},
		},
	}
}

func mapAnthropicBatchStatus(rawStatus string) provider.BatchStatusValue {
	if rawStatus == "ended" {
		return provider.BatchStatusCompleted
	}
	// "in_progress", "canceling", and any unknown status are all pending,
	// matching TS mapAnthropicBatchStatus's default case.
	return provider.BatchStatusPending
}

type anthropicBatchListResponseWire struct {
	Data    []anthropicBatchResponseWire `json:"data"`
	HasMore bool                         `json:"has_more"`
	LastID  *string                      `json:"last_id"`
}

type anthropicBatchResultErrorWire struct {
	Type  string `json:"type"`
	Error struct {
		Type    string `json:"type"`
		Message string `json:"message"`
	} `json:"error"`
	RequestID *string `json:"request_id"`
}

type anthropicBatchResultWire struct {
	Type    string          `json:"type"` // succeeded | errored | canceled | expired
	Message json.RawMessage `json:"message,omitempty"`
	Error   json.RawMessage `json:"error,omitempty"`
}

type anthropicBatchResultLineWire struct {
	CustomID string          `json:"custom_id"`
	Result   json.RawMessage `json:"result"`
}

// anthropicBatchResultsStream implements provider.BatchV4ItemResultStream
// over the JSONL body returned by a batch's results_url.
type anthropicBatchResultsStream struct {
	scanner  *bufio.Scanner
	body     io.ReadCloser
	provider *Provider
	err      error
}

func newAnthropicBatchResultsStream(body io.ReadCloser, p *Provider) *anthropicBatchResultsStream {
	scanner := bufio.NewScanner(body)
	scanner.Buffer(make([]byte, 64*1024), 32*1024*1024)
	return &anthropicBatchResultsStream{scanner: scanner, body: body, provider: p}
}

func (s *anthropicBatchResultsStream) Next() (*provider.BatchV4ItemResult, error) {
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
		var wire anthropicBatchResultLineWire
		if err := json.Unmarshal(line, &wire); err != nil {
			s.err = err
			return nil, err
		}
		return s.convertResult(wire), nil
	}
}

func (s *anthropicBatchResultsStream) convertResult(wire anthropicBatchResultLineWire) *provider.BatchV4ItemResult {
	var result anthropicBatchResultWire
	if err := json.Unmarshal(wire.Result, &result); err != nil {
		return invalidAnthropicBatchResult(wire.CustomID)
	}

	switch result.Type {
	case "canceled":
		return &provider.BatchV4ItemResult{Type: provider.BatchRequestTypeText, ID: wire.CustomID, Status: provider.BatchItemCancelled}
	case "expired":
		return &provider.BatchV4ItemResult{Type: provider.BatchRequestTypeText, ID: wire.CustomID, Status: provider.BatchItemExpired}
	case "errored":
		var errWire anthropicBatchResultErrorWire
		if err := json.Unmarshal(result.Error, &errWire); err != nil {
			return invalidAnthropicBatchResult(wire.CustomID)
		}
		item := &provider.BatchV4ItemResult{
			Type:   provider.BatchRequestTypeText,
			ID:     wire.CustomID,
			Status: provider.BatchItemFailed,
			Error:  &provider.BatchError{Message: errWire.Error.Message, Type: errWire.Error.Type},
		}
		if errWire.RequestID != nil {
			item.ProviderMetadata = map[string]interface{}{"anthropic": map[string]interface{}{"requestId": *errWire.RequestID}}
		}
		return item
	case "succeeded":
		var response anthropicResponse
		if err := json.Unmarshal(result.Message, &response); err != nil {
			return invalidAnthropicBatchResult(wire.CustomID)
		}
		lm := &LanguageModel{provider: s.provider}
		genResult := lm.convertResponseWithOptions(response, convertOptions{
			// Batch results are retrieved independently of the start call, so
			// there is no original tool list to map provider tool names
			// against, and implicitly provisioned code execution must stay
			// self-describing (dynamic), matching TS convertAnthropicBatchResponse.
			markCodeExecutionDynamic: true,
		})
		return &provider.BatchV4ItemResult{
			Type:       provider.BatchRequestTypeText,
			ID:         wire.CustomID,
			Status:     provider.BatchItemSucceeded,
			TextResult: genResult,
		}
	default:
		return invalidAnthropicBatchResult(wire.CustomID)
	}
}

func invalidAnthropicBatchResult(id string) *provider.BatchV4ItemResult {
	return &provider.BatchV4ItemResult{
		Type:   provider.BatchRequestTypeText,
		ID:     id,
		Status: provider.BatchItemFailed,
		Error:  &provider.BatchError{Message: "Anthropic returned an invalid Message batch result.", Code: "invalid_response"},
	}
}

func (s *anthropicBatchResultsStream) Err() error { return s.err }

func (s *anthropicBatchResultsStream) Close() error { return s.body.Close() }
