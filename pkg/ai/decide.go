package ai

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strconv"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/gateway"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
	"github.com/digitallysavvy/go-ai/pkg/version"
)

// DecideUsage reports token usage for an ExperimentalDecide call.
type DecideUsage struct {
	InputTokens  *int
	OutputTokens *int
	TotalTokens  *int
}

// DecideResponse carries response metadata for an ExperimentalDecide call.
type DecideResponse struct {
	ID        string
	Timestamp time.Time
	ModelID   string
	Headers   map[string]string
	Body      interface{}
}

// DecideOnStartEvent is emitted before calling the decision model.
type DecideOnStartEvent struct {
	// CallID is a unique identifier for this decide call.
	CallID string
	// OperationID is the canonical operation name ("ai.decide").
	OperationID string
	// RuntimeContext is the user-defined runtime context passed via options.
	RuntimeContext  interface{}
	Provider        string
	ModelID         string
	State           interface{}
	Questions       map[string]provider.DecisionQuestion
	MaxRetries      int
	Headers         map[string]string
	ProviderOptions map[string]interface{}
}

// DecideOnEndEvent is emitted after the decide operation completes
// successfully.
type DecideOnEndEvent struct {
	DecideOnStartEvent
	Answers          map[string]provider.DecisionAnswer
	Usage            DecideUsage
	Warnings         []types.Warning
	Rounding         *provider.DecisionRounding
	ProviderMetadata map[string]interface{}
	Response         DecideResponse
}

// DecideResult is returned by ExperimentalDecide.
type DecideResult struct {
	// Answers has exactly one typed answer per question, keyed by question ID.
	Answers          map[string]provider.DecisionAnswer
	Usage            DecideUsage
	Warnings         []types.Warning
	Rounding         *provider.DecisionRounding
	ProviderMetadata map[string]interface{}
	Response         DecideResponse
}

// DecideOptions are options for ExperimentalDecide.
type DecideOptions struct {
	// Model is a provider.DecisionModel instance, or a string ID resolved
	// via Provider (or the AI Gateway when Provider is nil).
	Model interface{}

	// Provider resolves a string Model ID to an DecisionModel. When nil and
	// Model is a string, resolution falls back to the AI Gateway (mirrors
	// TypeScript: `(globalThis.AI_SDK_DEFAULT_PROVIDER ?? gateway).decisionModel(id)`).
	Provider interface{}

	// State is one shared JSON-compatible string, map[string]interface{}, or
	// []interface{}.
	State interface{}

	// Questions maps question ID -> question.
	Questions map[string]provider.DecisionQuestion

	// MaxRetries is the maximum number of retries for transient provider
	// failures. Defaults to 2.
	MaxRetries      *int
	Headers         map[string]string
	ProviderOptions map[string]interface{}

	Telemetry             *TelemetrySettings
	ExperimentalTelemetry *TelemetrySettings

	// RuntimeContext is user-defined runtime context for this call. Treat as
	// immutable.
	RuntimeContext interface{}

	// OnStart is called when the decide operation begins.
	OnStart func(DecideOnStartEvent)
	// OnEnd is called when the decide operation completes successfully.
	OnEnd func(DecideOnEndEvent)
}

// ExperimentalDecide decides typed questions against one shared state.
// Experimental: may change in patch releases.
func ExperimentalDecide(ctx context.Context, opts DecideOptions) (*DecideResult, error) {
	model, err := resolveDecisionModel(opts.Model, opts.Provider)
	if err != nil {
		return nil, err
	}

	if err := validateDecisionInput(opts.State, opts.Questions); err != nil {
		return nil, err
	}

	supported := make(map[string]struct{}, len(model.SupportedQuestionTypes()))
	for _, t := range model.SupportedQuestionTypes() {
		supported[t] = struct{}{}
	}
	for id, q := range opts.Questions {
		if _, ok := supported[q.Type]; !ok {
			return nil, &providererrors.DecisionUnsupportedQuestionTypeError{
				QuestionID:   id,
				QuestionType: q.Type,
				Provider:     model.Provider(),
				ModelID:      model.ModelID(),
			}
		}
	}

	if err := validateMaxRetries(opts.MaxRetries); err != nil {
		return nil, err
	}
	resolvedMaxRetries := preparedMaxRetries(opts.MaxRetries)

	telemetrySettings := effectiveTelemetrySettings(opts.Telemetry, opts.ExperimentalTelemetry)

	callID := newCallID()
	startEvent := DecideOnStartEvent{
		CallID:          callID,
		OperationID:     "ai.decide",
		RuntimeContext:  opts.RuntimeContext,
		Provider:        model.Provider(),
		ModelID:         model.ModelID(),
		State:           opts.State,
		Questions:       opts.Questions,
		MaxRetries:      resolvedMaxRetries,
		Headers:         opts.Headers,
		ProviderOptions: opts.ProviderOptions,
	}

	// Root "ai.decide" span. Mirrors TypeScript's restricted telemetry
	// dispatcher (packages/ai/src/decide/restricted-telemetry-dispatcher.ts):
	// decide's onStart/onEnd are routed to
	// experimental_onDecideStart/End instead of the generic dispatcher used
	// by generateText/embed/etc, so registered integrations decide how (and
	// whether) to create spans — core never touches OTel directly.
	ctx = telemetry.FireOnDecideStart(ctx, telemetry.DecideStartEvent{
		Settings:       telemetrySettings,
		CallID:         callID,
		OperationID:    "ai.decide",
		ModelProvider:  model.Provider(),
		ModelID:        model.ModelID(),
		RuntimeContext: telemetryRuntimeContext(telemetrySettings, opts.RuntimeContext),
		State:          opts.State,
		Questions:      opts.Questions,
		MaxRetries:     resolvedMaxRetries,
		Headers:        opts.Headers,
	})
	if opts.OnStart != nil {
		opts.OnStart(startEvent)
	}

	headers := version.WithUserAgentSuffix(opts.Headers, version.UserAgent())

	// Resolve file URLs to inline data; the model-call events and the model
	// see the prepared parts (mirrors prepareDecisionState in decide.ts).
	preparedState, err := prepareDecisionState(ctx, opts.State)
	if err != nil {
		telemetry.FireOnError(ctx, telemetry.TelemetryErrorEvent{Settings: telemetrySettings, Error: err, CallID: callID})
		return nil, err
	}

	// Nested "ai.decide.doDecide" model-call event, wrapping the whole
	// retry loop for the underlying model call, mirrors the modelCallEvent
	// notify pair (experimental_onDecisionModelCallStart/End) in decide.ts.
	telemetry.FireOnDecisionModelCallStart(ctx, telemetry.DecisionModelCallStartEvent{
		Settings:      telemetrySettings,
		CallID:        callID,
		OperationID:   "ai.decide.doDecide",
		ModelProvider: model.Provider(),
		ModelID:       model.ModelID(),
		State:         preparedState,
		Questions:     opts.Questions,
	})

	var result *provider.DecisionResult
	err = withEmbedRetry(ctx, resolvedMaxRetries, func(callCtx context.Context) error {
		res, callErr := model.DoDecide(callCtx, provider.DecisionCallOptions{
			State:           preparedState,
			Questions:       opts.Questions,
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
		// experimental_onDecisionModelCallEnd is never notified on this
		// path (mirrors decide.ts); the registered integration is
		// responsible for defensively closing any nested span it opened at
		// OnDecisionModelCallStart when it receives this CallID.
		telemetry.FireOnError(ctx, telemetry.TelemetryErrorEvent{Settings: telemetrySettings, Error: err, CallID: callID})
		return nil, err
	}

	if err := validateDecisionAnswers(opts.Questions, result.Answers, result.Rounding); err != nil {
		telemetry.FireOnError(ctx, telemetry.TelemetryErrorEvent{Settings: telemetrySettings, Error: err, CallID: callID})
		return nil, err
	}

	// Refusals are valid answers for the model but not for the caller: fail
	// without retrying, before the model-call end event (mirrors decide.ts).
	var refused []string
	for id := range opts.Questions {
		if result.Answers[id].Type == "refusal" {
			refused = append(refused, id)
		}
	}
	if len(refused) > 0 {
		sort.Strings(refused)
		err := &providererrors.DecisionRefusalError{QuestionIDs: refused, Provider: model.Provider(), ModelID: model.ModelID()}
		telemetry.FireOnError(ctx, telemetry.TelemetryErrorEvent{Settings: telemetrySettings, Error: err, CallID: callID})
		return nil, err
	}

	usage := DecideUsage{}
	if result.Usage != nil {
		usage.InputTokens = result.Usage.InputTokens
		usage.OutputTokens = result.Usage.OutputTokens
		if result.Usage.InputTokens != nil && result.Usage.OutputTokens != nil {
			total := *result.Usage.InputTokens + *result.Usage.OutputTokens
			usage.TotalTokens = &total
		}
	}

	var modelResponseID, modelResponseModelID string
	if result.Response != nil {
		modelResponseID = result.Response.ID
		modelResponseModelID = result.Response.ModelID
	}

	// Mirrors experimental_onDecisionModelCallEnd: ai.decision.answers is
	// output-gated; usage and providerMetadata are unconditional.
	telemetry.FireOnDecisionModelCallEnd(ctx, telemetry.DecisionModelCallEndEvent{
		DecisionModelCallStartEvent: telemetry.DecisionModelCallStartEvent{
			Settings:      telemetrySettings,
			CallID:        callID,
			OperationID:   "ai.decide.doDecide",
			ModelProvider: model.Provider(),
			ModelID:       model.ModelID(),
			State:         preparedState,
			Questions:     opts.Questions,
		},
		Answers:          result.Answers,
		Usage:            telemetryUsageFromDecideUsage(usage),
		ProviderMetadata: result.ProviderMetadata,
		ResponseID:       modelResponseID,
		ResponseModelID:  modelResponseModelID,
	})

	logModelWarnings(result.Warnings, model.Provider(), model.ModelID())

	response := DecideResponse{ModelID: model.ModelID(), Timestamp: time.Now()}
	if result.Response != nil {
		if result.Response.ID != "" {
			response.ID = result.Response.ID
		}
		if !result.Response.Timestamp.IsZero() {
			response.Timestamp = result.Response.Timestamp
		}
		if result.Response.ModelID != "" {
			response.ModelID = result.Response.ModelID
		}
		response.Headers = result.Response.Headers
		response.Body = result.Response.Body
	}

	evalResult := &DecideResult{
		Answers:          result.Answers,
		Usage:            usage,
		Warnings:         warningsOrEmpty(result.Warnings),
		Rounding:         result.Rounding,
		ProviderMetadata: result.ProviderMetadata,
		Response:         response,
	}

	// Mirrors onDecideOperationEnd: ai.decision.answers is also recorded
	// on the root span, output-gated, and the root span is ended.
	telemetry.FireOnDecideEnd(ctx, telemetry.DecideEndEvent{
		DecideStartEvent: telemetry.DecideStartEvent{
			Settings:       telemetrySettings,
			CallID:         callID,
			OperationID:    "ai.decide",
			ModelProvider:  model.Provider(),
			ModelID:        model.ModelID(),
			RuntimeContext: telemetryRuntimeContext(telemetrySettings, opts.RuntimeContext),
		},
		Answers: evalResult.Answers,
	})

	if opts.OnEnd != nil {
		opts.OnEnd(DecideOnEndEvent{
			DecideOnStartEvent: startEvent,
			Answers:            evalResult.Answers,
			Usage:              evalResult.Usage,
			Warnings:           evalResult.Warnings,
			Rounding:           evalResult.Rounding,
			ProviderMetadata:   evalResult.ProviderMetadata,
			Response:           evalResult.Response,
		})
	}

	return evalResult, nil
}

func telemetryUsageFromDecideUsage(u DecideUsage) telemetry.TelemetryUsage {
	toInt64 := func(v *int) *int64 {
		if v == nil {
			return nil
		}
		out := int64(*v)
		return &out
	}
	return telemetry.TelemetryUsage{
		InputTokens:  toInt64(u.InputTokens),
		OutputTokens: toInt64(u.OutputTokens),
		TotalTokens:  toInt64(u.TotalTokens),
	}
}

// resolveDecisionModel resolves model (a provider.DecisionModel instance
// or a string ID) using resolver, falling back to the AI Gateway when
// resolver is nil and model is a string. Mirrors TypeScript's
// resolveDecisionModel.
func resolveDecisionModel(model interface{}, resolver interface{}) (provider.DecisionModel, error) {
	switch v := model.(type) {
	case provider.DecisionModel:
		if v == nil {
			return nil, &providererrors.InvalidArgumentError{Field: "model", Message: "model is required"}
		}
		return v, nil
	case provider.EvaluationModel:
		// Deprecated pre-rename model (DoEvaluate): adapt it, like TS's
		// legacy-model adapter in resolveDecisionModel.
		if v == nil {
			return nil, &providererrors.InvalidArgumentError{Field: "model", Message: "model is required"}
		}
		return provider.AdaptEvaluationModel(v), nil
	case string:
		if resolver == nil {
			gw, err := gateway.Gateway()
			if err != nil {
				return nil, fmt.Errorf("failed to resolve the default AI Gateway provider for decision: %w", err)
			}
			resolver = gw
		}
		resolved, supported, err := provider.DecisionModelFromProvider(resolver, v)
		if !supported {
			return nil, &providererrors.InvalidArgumentError{
				Field:   "provider",
				Message: "provider must implement provider.DecisionModelProvider (or the deprecated provider.EvaluationModelProvider)",
			}
		}
		if err != nil {
			return nil, err
		}
		if resolved == nil {
			return nil, fmt.Errorf("no such decisionModel: %s", v)
		}
		return resolved, nil
	case nil:
		return nil, &providererrors.InvalidArgumentError{Field: "model", Message: "model is required"}
	default:
		return nil, &providererrors.InvalidArgumentError{
			Field:   "model",
			Message: "model must be a provider.DecisionModel instance or a string ID",
		}
	}
}

// --- Validation (mirrors TypeScript's decide/validate-decision.ts) ---

func isDecisionRecord(value interface{}) (map[string]interface{}, bool) {
	m, ok := value.(map[string]interface{})
	return m, ok
}

func isJSONCompatible(value interface{}) bool {
	if value == nil {
		return true
	}
	switch v := value.(type) {
	case string, bool:
		return true
	case float32:
		return !math.IsNaN(float64(v)) && !math.IsInf(float64(v), 0)
	case float64:
		return !math.IsNaN(v) && !math.IsInf(v, 0)
	case int, int8, int16, int32, int64, uint, uint8, uint16, uint32, uint64:
		return true
	case []interface{}:
		for _, item := range v {
			if !isJSONCompatible(item) {
				return false
			}
		}
		return true
	case map[string]interface{}:
		for _, item := range v {
			if !isJSONCompatible(item) {
				return false
			}
		}
		return true
	default:
		return false
	}
}

// isDecisionInput reports whether value is a JSON-compatible string,
// map[string]interface{}, or []interface{} (mirrors TypeScript's isInput).
func isDecisionInput(value interface{}) bool {
	switch value.(type) {
	case string:
		return true
	case []interface{}, map[string]interface{}:
		return isJSONCompatible(value)
	default:
		return false
	}
}

func invalidDecisionInput(field string, message string) error {
	return &providererrors.InvalidArgumentError{Field: field, Message: message}
}

// validateDecisionInput mirrors TypeScript's validateDecisionInput.
func validateDecisionInput(state interface{}, questions map[string]provider.DecisionQuestion) error {
	if err := validateDecisionState(state); err != nil {
		return err
	}
	if len(questions) == 0 {
		return invalidDecisionInput("questions", "must be a nonempty question map")
	}

	for id, q := range questions {
		field := fmt.Sprintf("questions.%s", id)
		if !isDecisionInput(q.Instructions) {
			return invalidDecisionInput(field, "instructions must be a JSON-compatible string, object, or array")
		}

		switch q.Type {
		case "choice":
			criteria, ok := isDecisionRecord(q.Criteria)
			if !ok || len(criteria) == 0 {
				return invalidDecisionInput(field, "choice criteria must be a nonempty option map")
			}
			for _, v := range criteria {
				if v != nil && !isDecisionInput(v) {
					return invalidDecisionInput(field, "criteria descriptions must be JSON-compatible strings, objects, arrays, or null")
				}
			}
		case "score":
			criteria, ok := q.Criteria.([]interface{})
			if !ok || len(criteria) < 2 {
				return invalidDecisionInput(field, "score criteria must contain at least two ordered levels")
			}
			for _, v := range criteria {
				if v != nil && !isDecisionInput(v) {
					return invalidDecisionInput(field, "criteria descriptions must be JSON-compatible strings, objects, arrays, or null")
				}
			}
		case "boolean":
			if q.Criteria != nil {
				criteria, ok := isDecisionRecord(q.Criteria)
				if !ok {
					return invalidDecisionInput(field, "boolean criteria may only describe true and false")
				}
				for k, v := range criteria {
					if k != "true" && k != "false" {
						return invalidDecisionInput(field, "boolean criteria may only describe true and false")
					}
					if v != nil && !isDecisionInput(v) {
						return invalidDecisionInput(field, "criteria descriptions must be JSON-compatible strings, objects, arrays, or null")
					}
				}
			}
		default:
			return invalidDecisionInput(field, "question type must be choice, score, or boolean")
		}
	}
	return nil
}

const decisionTolerance = 1e-6

func invalidDecisionAnswer(answers interface{}, message string) error {
	return providererrors.NewInvalidResponseDataError(answers, message)
}

func isDecisionProbability(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0) && v >= 0 && v <= 1
}

func decisionRoundingError(decimals *int, answers interface{}) (float64, error) {
	if decimals == nil {
		return 0, nil
	}
	if *decimals < 0 || *decimals > 15 {
		return 0, invalidDecisionAnswer(answers, "Decision rounding decimals must be integers between 0 and 15.")
	}
	return 0.5 * math.Pow(10, -float64(*decimals)), nil
}

func validateDecisionDistribution(dist map[string]float64, keys []string, answers interface{}, id string, roundingErr float64) error {
	keySet := make(map[string]struct{}, len(keys))
	for _, k := range keys {
		keySet[k] = struct{}{}
	}
	if len(dist) != len(keySet) {
		return invalidDecisionAnswer(answers, fmt.Sprintf("Question %q must have a complete distribution of finite probabilities in [0, 1].", id))
	}
	var sum float64
	for k, p := range dist {
		if _, ok := keySet[k]; !ok || !isDecisionProbability(p) {
			return invalidDecisionAnswer(answers, fmt.Sprintf("Question %q must have a complete distribution of finite probabilities in [0, 1].", id))
		}
		sum += p
	}
	if math.Abs(sum-1) > decisionTolerance+float64(len(keys))*roundingErr {
		return invalidDecisionAnswer(answers, fmt.Sprintf("Question %q probabilities must sum to 1 within the declared rounding precision.", id))
	}
	return nil
}

// validateDecisionAnswers mirrors TypeScript's validateDecisionAnswers.
func validateDecisionAnswers(questions map[string]provider.DecisionQuestion, answers map[string]provider.DecisionAnswer, rounding *provider.DecisionRounding) error {
	var probError, scoreError float64
	var err error
	if rounding != nil {
		probError, err = decisionRoundingError(rounding.ProbabilityDecimals, answers)
		if err != nil {
			return err
		}
		scoreError, err = decisionRoundingError(rounding.ScoreDecimals, answers)
		if err != nil {
			return err
		}
	}

	if len(answers) != len(questions) {
		return invalidDecisionAnswer(answers, "Decision must return exactly one answer for every question.")
	}
	for id := range questions {
		if _, ok := answers[id]; !ok {
			return invalidDecisionAnswer(answers, "Decision must return exactly one answer for every question.")
		}
	}

	for id, q := range questions {
		answer := answers[id]
		if answer.Type == "refusal" {
			continue
		}
		if answer.Type != q.Type {
			return invalidDecisionAnswer(answers, fmt.Sprintf("Question %q returned an answer with the wrong type.", id))
		}

		switch q.Type {
		case "choice":
			criteria, _ := isDecisionRecord(q.Criteria)
			if _, ok := criteria[answer.Choice]; answer.Choice == "" || !ok {
				return invalidDecisionAnswer(answers, fmt.Sprintf("Question %q selected an unknown option.", id))
			}
			if answer.Probabilities != nil {
				keys := make([]string, 0, len(criteria))
				for k := range criteria {
					keys = append(keys, k)
				}
				if err := validateDecisionDistribution(answer.Probabilities, keys, answers, id, probError); err != nil {
					return err
				}
				selected := answer.Probabilities[answer.Choice]
				for _, p := range answer.Probabilities {
					if p > selected+decisionTolerance+2*probError {
						return invalidDecisionAnswer(answers, fmt.Sprintf("Question %q did not select a highest-probability option.", id))
					}
				}
			}
		case "score":
			criteria, _ := q.Criteria.([]interface{})
			maxScore := float64(len(criteria) - 1)
			if answer.Score == nil || !isFiniteFloat(*answer.Score) || *answer.Score < 0 || *answer.Score > maxScore {
				return invalidDecisionAnswer(answers, fmt.Sprintf("Question %q score must be in [0, %v].", id, maxScore))
			}
			if answer.Probabilities != nil {
				keys := make([]string, len(criteria))
				for i := range criteria {
					keys[i] = strconv.Itoa(i)
				}
				if err := validateDecisionDistribution(answer.Probabilities, keys, answers, id, probError); err != nil {
					return err
				}
				var mean, meanRoundingError float64
				for idxStr, p := range answer.Probabilities {
					idx, _ := strconv.Atoi(idxStr)
					mean += float64(idx) * p
				}
				for i := range keys {
					meanRoundingError += float64(i) * probError
				}
				if math.Abs(mean-*answer.Score) > decisionTolerance+meanRoundingError+scoreError {
					return invalidDecisionAnswer(answers, fmt.Sprintf("Question %q score must equal the probability-weighted mean within the declared rounding precision.", id))
				}
			}
		case "boolean":
			if answer.Probability == nil || !isDecisionProbability(*answer.Probability) {
				return invalidDecisionAnswer(answers, fmt.Sprintf("Question %q must return P(true) as a finite probability in [0, 1].", id))
			}
		}
	}
	return nil
}

func isFiniteFloat(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}
