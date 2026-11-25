package ai

import (
	"context"
	"fmt"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/gateway"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
	"github.com/digitallysavvy/go-ai/pkg/version"
)

// ExtractUsage reports token usage for an ExperimentalExtract call.
type ExtractUsage struct {
	InputTokens  *int
	OutputTokens *int
	TotalTokens  *int
}

// ExtractResponse carries response metadata for an ExperimentalExtract call.
// Timestamp and ModelID are always set.
type ExtractResponse struct {
	ID        string
	Timestamp time.Time
	ModelID   string
	Headers   map[string]string
	Body      interface{}
}

// ExtractOnStartEvent is passed to ExtractOptions.OnStart when the extract
// operation begins, before the extraction model is called. Mirrors TS
// ExtractStartEvent. Unlike the event integrations receive, it carries the
// caller's unredacted State, Headers and ProviderOptions.
type ExtractOnStartEvent struct {
	// CallID is a unique identifier for this extract call.
	CallID string
	// OperationID is the canonical operation name ("ai.extract").
	OperationID string
	// RuntimeContext is the user-defined runtime context passed via options.
	RuntimeContext interface{}
	Provider       string
	ModelID        string
	// State is the shared state exactly as the caller passed it.
	State  interface{}
	Fields map[string]provider.ExtractionField
	// ReferenceDate is the YYYY-MM-DD reference date, or "".
	ReferenceDate   string
	MaxOutputTokens *int
	Reasoning       *types.ReasoningLevel
	MaxRetries      int
	Headers         map[string]string
	ProviderOptions map[string]interface{}
}

// ExtractOnEndEvent is passed to ExtractOptions.OnEnd when the extract
// operation completes successfully. Mirrors TS ExtractEndEvent.
type ExtractOnEndEvent struct {
	ExtractOnStartEvent
	Answers          map[string]provider.ExtractionAnswer
	Usage            ExtractUsage
	Warnings         []types.Warning
	ProviderMetadata map[string]interface{}
	Response         ExtractResponse
}

// ExtractResult is returned by ExperimentalExtract.
type ExtractResult struct {
	// Answers has exactly one typed answer per field, keyed by field ID.
	// Empty strings and empty lists are normalized to a nil Value.
	Answers          map[string]provider.ExtractionAnswer
	Usage            ExtractUsage
	Warnings         []types.Warning
	ProviderMetadata map[string]interface{}
	Response         ExtractResponse
}

// ExtractOptions are options for ExperimentalExtract.
type ExtractOptions struct {
	// Model is a provider.ExtractionModel instance, or a string ID resolved
	// via ExtractionProvider, then the SetDefaultProvider provider, then the
	// AI Gateway (TS `(globalThis.AI_SDK_DEFAULT_PROVIDER ?? gateway)
	// .extractionModel(id)`).
	Model interface{}

	// ExtractionProvider resolves a string Model ID to an ExtractionModel.
	// When nil and Model is a string, the SetDefaultProvider provider is
	// used, then the AI Gateway.
	ExtractionProvider provider.ExtractionModelProvider

	// State is the shared state: a string, a JSON object
	// (map[string]interface{}), a JSON array ([]interface{}, sent as one json
	// part), or an ordered []provider.DecisionStatePart of text, file, and
	// json parts.
	State interface{}

	// Fields maps field ID -> field.
	Fields map[string]provider.ExtractionField

	// FieldOrder optionally carries the insertion order of Fields (Go maps
	// are unordered). IDs it omits follow, sorted. Nil keeps sorted order.
	FieldOrder []string

	// ReferenceDate is a YYYY-MM-DD date that anchors relative dates and
	// dates without a year.
	ReferenceDate string

	// MaxOutputTokens is the maximum number of output tokens for models that
	// support it. Must be an integer >= 1.
	MaxOutputTokens *int

	// Reasoning is the reasoning effort for models that support it.
	Reasoning *types.ReasoningLevel

	// MaxRetries is the maximum number of retries for transient provider
	// failures. Defaults to 2.
	MaxRetries      *int
	Headers         map[string]string
	ProviderOptions map[string]interface{}

	// ExperimentalDownload downloads the URL-backed file parts of State. It
	// is required for them: without it a remote file fails with a
	// DownloadError and the call never falls back to the default downloader
	// (TS experimental_download). Use CreateDownload with a DNS-protected
	// transport for untrusted URLs. The model's own HTTP client is not used.
	ExperimentalDownload DownloadFunction

	Telemetry             *TelemetrySettings
	ExperimentalTelemetry *TelemetrySettings

	// RuntimeContext is user-defined runtime context for this call. Treat as
	// immutable.
	RuntimeContext interface{}

	// OnStart is called when the extract operation begins and gets the
	// unredacted event.
	OnStart func(ExtractOnStartEvent)
	// OnEnd is called when the extract operation completes successfully and
	// gets the unredacted event.
	OnEnd func(ExtractOnEndEvent)
}

// extractNoDefaultSupportMessage is the TS text for a default provider without
// extractionModel.
const extractNoDefaultSupportMessage = "The default provider does not support extraction models. Pass an extraction model instance or configure AI_SDK_DEFAULT_PROVIDER with an extractionModel method."

// ExperimentalExtract extracts typed field values from one shared state.
// Experimental: may change in patch releases. Mirrors TS
// experimental_extract.
func ExperimentalExtract(ctx context.Context, opts ExtractOptions) (*ExtractResult, error) {
	model, err := resolveExtractionModel(opts.Model, opts.ExtractionProvider)
	if err != nil {
		return nil, err
	}
	if err := validateMaxOutputTokens(opts.MaxOutputTokens); err != nil {
		return nil, err
	}
	// TS takes any JSON value as instructions: typed Go containers are read
	// as their JSON form.
	opts.Fields = extractionFieldsJSON(opts.Fields)
	if err := validateExtractionInput(opts.State, opts.Fields, opts.FieldOrder, opts.ReferenceDate); err != nil {
		return nil, err
	}

	for _, id := range provider.OrderedKeys(opts.Fields, opts.FieldOrder) {
		field := opts.Fields[id]
		if !containsString(model.SupportedFieldTypes(), field.Type) {
			return nil, &providererrors.ExtractionUnsupportedFieldTypeError{
				FieldID:   id,
				FieldType: field.Type,
				Provider:  model.Provider(),
				ModelID:   model.ModelID(),
			}
		}
	}

	if err := validateMaxRetries(opts.MaxRetries); err != nil {
		return nil, err
	}
	resolvedMaxRetries := preparedMaxRetries(opts.MaxRetries)
	telemetrySettings := effectiveTelemetrySettings(opts.Telemetry, opts.ExperimentalTelemetry)

	callID := newCallID()
	startEvent := ExtractOnStartEvent{
		CallID:          callID,
		OperationID:     "ai.extract",
		RuntimeContext:  opts.RuntimeContext,
		Provider:        model.Provider(),
		ModelID:         model.ModelID(),
		State:           opts.State,
		Fields:          opts.Fields,
		ReferenceDate:   opts.ReferenceDate,
		MaxOutputTokens: opts.MaxOutputTokens,
		Reasoning:       opts.Reasoning,
		MaxRetries:      resolvedMaxRetries,
		Headers:         opts.Headers,
		ProviderOptions: opts.ProviderOptions,
	}

	// Mirrors TS' restricted telemetry dispatcher: start and end go to
	// experimental_onExtractStart/End, the nested call to
	// experimental_onExtractionModelCallStart/End. The telemetry package
	// drops Headers and ProviderOptions and redacts file locations; the
	// lifecycle callbacks get the unredacted events.
	telemetryFields := extractionFieldsTelemetry(opts.Fields)
	telemetryStart := telemetry.ExtractStartEvent{
		Settings:        telemetrySettings,
		CallID:          callID,
		OperationID:     "ai.extract",
		ModelProvider:   model.Provider(),
		ModelID:         model.ModelID(),
		RuntimeContext:  telemetryRuntimeContext(telemetrySettings, opts.RuntimeContext),
		State:           opts.State,
		Fields:          telemetryFields,
		ReferenceDate:   opts.ReferenceDate,
		MaxOutputTokens: opts.MaxOutputTokens,
		Reasoning:       opts.Reasoning,
		MaxRetries:      resolvedMaxRetries,
		Headers:         opts.Headers,
		ProviderOptions: opts.ProviderOptions,
	}
	// TS notify([onStart, telemetry.onStart]): the user callback first.
	if opts.OnStart != nil {
		safeInvoke(func() { opts.OnStart(startEvent) })
	}
	ctx = telemetry.FireOnExtractStart(ctx, telemetryStart)

	fail := func(err error) (*ExtractResult, error) {
		telemetry.FireOnError(ctx, telemetry.TelemetryErrorEvent{Settings: telemetrySettings, Error: err, CallID: callID})
		return nil, err
	}

	// Resolve file URLs to inline data with the caller's download hook (TS
	// 79d2004947: never the global fetch). The model sees the prepared parts,
	// including part provider options; opts.State is not modified.
	preparedState, err := prepareDecisionState(ctx, opts.State, opts.ExperimentalDownload)
	if err != nil {
		return fail(err)
	}

	modelCallStart := telemetry.ExtractionModelCallStartEvent{
		Settings:      telemetrySettings,
		CallID:        callID,
		OperationID:   "ai.extract.doExtract",
		ModelProvider: model.Provider(),
		ModelID:       model.ModelID(),
		State:         preparedState,
		Fields:        telemetryFields,
		ReferenceDate: opts.ReferenceDate,
	}
	telemetry.FireOnExtractionModelCallStart(ctx, modelCallStart)

	headers := version.WithUserAgentSuffix(opts.Headers, version.UserAgent())
	var result *provider.ExtractionResult
	err = withEmbedRetry(ctx, resolvedMaxRetries, func(callCtx context.Context) error {
		// TS abortSignal?.throwIfAborted() before every attempt: it throws
		// signal.reason (context.Cause).
		if err := context.Cause(callCtx); err != nil {
			return err
		}
		res, callErr := model.DoExtract(callCtx, provider.ExtractionCallOptions{
			State:           preparedState,
			Fields:          opts.Fields,
			FieldOrder:      opts.FieldOrder,
			ReferenceDate:   opts.ReferenceDate,
			MaxOutputTokens: opts.MaxOutputTokens,
			Reasoning:       opts.Reasoning,
			Headers:         headers,
			ProviderOptions: opts.ProviderOptions,
		})
		if callErr != nil {
			return callErr
		}
		result = res
		return nil
	})
	if err != nil {
		return fail(err)
	}

	// TS abortSignal?.throwIfAborted() after the model call.
	if err := context.Cause(ctx); err != nil {
		return fail(err)
	}
	if result == nil {
		return fail(fmt.Errorf("extraction model %s/%s returned no result", model.Provider(), model.ModelID()))
	}

	answers := normalizeExtractionAnswers(result.Answers)
	if err := validateExtractionAnswers(opts.Fields, opts.FieldOrder, preparedState, answers); err != nil {
		return fail(err)
	}
	// Refusals are valid answers for the model but not for the caller: fail
	// without retrying, before the model-call end event.
	var refused []string
	for _, id := range provider.OrderedKeys(opts.Fields, opts.FieldOrder) {
		if answers[id].Type == "refusal" {
			refused = append(refused, id)
		}
	}
	if len(refused) > 0 {
		return fail(&providererrors.ExtractionRefusalError{FieldIDs: refused, Provider: model.Provider(), ModelID: model.ModelID()})
	}

	var modelUsage ExtractUsage
	if result.Usage != nil {
		modelUsage.InputTokens = result.Usage.InputTokens
		modelUsage.OutputTokens = result.Usage.OutputTokens
	}
	var modelResponseID, modelResponseModelID string
	if result.Response != nil {
		modelResponseID = result.Response.ID
		modelResponseModelID = result.Response.ModelID
	}
	telemetry.FireOnExtractionModelCallEnd(ctx, telemetry.ExtractionModelCallEndEvent{
		ExtractionModelCallStartEvent: modelCallStart,
		Answers:                       answers,
		Usage:                         telemetryUsageFromDecideUsage(DecideUsage(modelUsage)),
		Warnings:                      result.Warnings,
		ProviderMetadata:              result.ProviderMetadata,
		ResponseID:                    modelResponseID,
		ResponseModelID:               modelResponseModelID,
		Response:                      nilIfNilPtr(result.Response),
	})

	logModelWarnings(result.Warnings, model.Provider(), model.ModelID())

	usage := modelUsage
	if usage.InputTokens != nil && usage.OutputTokens != nil {
		total := *usage.InputTokens + *usage.OutputTokens
		usage.TotalTokens = &total
	}
	response := ExtractResponse{ModelID: model.ModelID(), Timestamp: time.Now()}
	if result.Response != nil {
		response.ID = result.Response.ID
		if !result.Response.Timestamp.IsZero() {
			response.Timestamp = result.Response.Timestamp
		}
		if result.Response.ModelID != "" {
			response.ModelID = result.Response.ModelID
		}
		response.Headers = result.Response.Headers
		response.Body = result.Response.Body
	}

	extractResult := &ExtractResult{
		Answers:          answers,
		Usage:            usage,
		Warnings:         warningsOrEmpty(result.Warnings),
		ProviderMetadata: result.ProviderMetadata,
		Response:         response,
	}

	// TS notify([onEnd, telemetry.onEnd]): the user callback first.
	if opts.OnEnd != nil {
		endEvent := ExtractOnEndEvent{
			ExtractOnStartEvent: startEvent,
			Answers:             extractResult.Answers,
			Usage:               extractResult.Usage,
			Warnings:            extractResult.Warnings,
			ProviderMetadata:    extractResult.ProviderMetadata,
			Response:            extractResult.Response,
		}
		safeInvoke(func() { opts.OnEnd(endEvent) })
	}
	telemetry.FireOnExtractEnd(ctx, telemetry.ExtractEndEvent{
		ExtractStartEvent: telemetryStart,
		Answers:           extractResult.Answers,
		Usage:             telemetryUsageFromDecideUsage(DecideUsage(extractResult.Usage)),
		Warnings:          extractResult.Warnings,
		ProviderMetadata:  extractResult.ProviderMetadata,
		Response:          extractResult.Response,
	})

	return extractResult, nil
}

// checkExtractionModelVersion is TS resolveExtractionModel's
// `model.specificationVersion !== 'v4'` check.
func checkExtractionModelVersion(m provider.ExtractionModel) (provider.ExtractionModel, error) {
	if v := m.SpecificationVersion(); v != "v4" {
		return nil, &UnsupportedModelVersionError{Version: v, Provider: m.Provider(), ModelID: m.ModelID()}
	}
	return m, nil
}

// ResolveExtractionModel resolves an extraction model the way
// ExperimentalExtract does for ExtractOptions.Model with no ExtractionProvider:
// an instance is checked (specification version "v4") and returned, a string
// ID goes to the SetDefaultProvider provider, then the AI Gateway. The
// registry and custom providers use it for their aliases. Experimental.
func ResolveExtractionModel(model interface{}) (provider.ExtractionModel, error) {
	return resolveExtractionModel(model, nil)
}

// resolveExtractionModel resolves model (a provider.ExtractionModel instance
// or a string ID), mirroring TS resolveExtractionModel (10ff0b1a1d): a
// string goes to resolver, else the SetDefaultProvider provider, else the AI
// Gateway. A provider without extraction models is a NoSuchModelError that
// says how to configure one.
func resolveExtractionModel(model interface{}, resolver provider.ExtractionModelProvider) (provider.ExtractionModel, error) {
	switch v := model.(type) {
	case provider.ExtractionModel:
		if v == nil {
			return nil, &providererrors.InvalidArgumentError{Field: "model", Message: "model is required"}
		}
		return checkExtractionModelVersion(v)
	case string:
		if resolver == nil {
			candidate := configuredDefaultProvider()
			if candidate == nil {
				gw, err := gateway.Gateway()
				if err != nil {
					return nil, fmt.Errorf("failed to resolve the default AI Gateway provider for extraction: %w", err)
				}
				candidate = gw
			}
			p, ok := candidate.(provider.ExtractionModelProvider)
			if !ok {
				return nil, &providererrors.NoSuchModelError{ModelID: v, ModelType: "extractionModel", Message: extractNoDefaultSupportMessage}
			}
			resolver = p
		}
		resolved, err := resolver.ExtractionModel(v)
		if err != nil {
			return nil, err
		}
		if resolved == nil {
			return nil, providererrors.NewNoSuchModelError(v, "extractionModel")
		}
		return checkExtractionModelVersion(resolved)
	case nil:
		return nil, &providererrors.InvalidArgumentError{Field: "model", Message: "model is required"}
	default:
		return nil, &providererrors.InvalidArgumentError{
			Field:   "model",
			Message: "model must be a provider.ExtractionModel instance or a string ID",
		}
	}
}

// extractionFieldsJSON converts typed Go containers in field instructions to
// the generic JSON form (TS fields are JSON values) in a copy of fields; the
// caller's map is never modified.
func extractionFieldsJSON(fields map[string]provider.ExtractionField) map[string]provider.ExtractionField {
	out := make(map[string]provider.ExtractionField, len(fields))
	for id, f := range fields {
		f.Instructions = provider.DecisionQuestionsJSON(map[string]provider.DecisionQuestion{id: {Instructions: f.Instructions}})[id].Instructions
		out[id] = f
	}
	return out
}

// extractionFieldsTelemetry is the JSON form of the fields as TS records
// them (ai.extraction.fields): camelCase keys, unset values omitted.
func extractionFieldsTelemetry(fields map[string]provider.ExtractionField) map[string]interface{} {
	out := make(map[string]interface{}, len(fields))
	for id, f := range fields {
		m := map[string]interface{}{"type": f.Type}
		if f.Instructions != nil {
			m["instructions"] = f.Instructions
		}
		if f.Unit != "" {
			m["unit"] = f.Unit
		}
		if f.Integer != nil {
			m["integer"] = *f.Integer
		}
		if f.Decimals != nil {
			m["decimals"] = *f.Decimals
		}
		if f.Items != "" {
			m["items"] = f.Items
		}
		out[id] = m
	}
	return out
}
