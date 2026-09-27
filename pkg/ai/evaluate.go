package ai

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providers/gateway"
	"github.com/digitallysavvy/go-ai/pkg/telemetry"
	"github.com/digitallysavvy/go-ai/pkg/version"
)

// EvaluateUsage reports token usage for an ExperimentalEvaluate call.
type EvaluateUsage struct {
	InputTokens  *int
	OutputTokens *int
	TotalTokens  *int
}

// EvaluateResponse carries response metadata for an ExperimentalEvaluate call.
type EvaluateResponse struct {
	ID        string
	Timestamp time.Time
	ModelID   string
	Headers   map[string]string
	Body      interface{}
}

// EvaluateOnStartEvent is emitted before calling the evaluation model.
type EvaluateOnStartEvent struct {
	// CallID is a unique identifier for this evaluate call.
	CallID string
	// OperationID is the canonical operation name ("ai.evaluate").
	OperationID string
	// RuntimeContext is the user-defined runtime context passed via options.
	RuntimeContext  interface{}
	Provider        string
	ModelID         string
	State           interface{}
	Questions       map[string]provider.EvaluationQuestion
	MaxRetries      int
	Headers         map[string]string
	ProviderOptions map[string]interface{}
}

// EvaluateOnEndEvent is emitted after the evaluate operation completes
// successfully.
type EvaluateOnEndEvent struct {
	EvaluateOnStartEvent
	Answers          map[string]provider.EvaluationAnswer
	Usage            EvaluateUsage
	Warnings         []types.Warning
	Rounding         *provider.EvaluationRounding
	ProviderMetadata map[string]interface{}
	Response         EvaluateResponse
}

// EvaluateResult is returned by ExperimentalEvaluate.
type EvaluateResult struct {
	// Answers has exactly one typed answer per question, keyed by question ID.
	Answers          map[string]provider.EvaluationAnswer
	Usage            EvaluateUsage
	Warnings         []types.Warning
	Rounding         *provider.EvaluationRounding
	ProviderMetadata map[string]interface{}
	Response         EvaluateResponse
}

// EvaluateOptions are options for ExperimentalEvaluate.
type EvaluateOptions struct {
	// Model is a provider.EvaluationModel instance, or a string ID resolved
	// via Provider (or the AI Gateway when Provider is nil).
	Model interface{}

	// Provider resolves a string Model ID to an EvaluationModel. When nil and
	// Model is a string, resolution falls back to the AI Gateway (mirrors
	// TypeScript: `(globalThis.AI_SDK_DEFAULT_PROVIDER ?? gateway).evaluationModel(id)`).
	Provider provider.EvaluationModelProvider

	// State is one shared JSON-compatible string, map[string]interface{}, or
	// []interface{}.
	State interface{}

	// Questions maps question ID -> question.
	Questions map[string]provider.EvaluationQuestion

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

	// OnStart is called when the evaluate operation begins.
	OnStart func(EvaluateOnStartEvent)
	// OnEnd is called when the evaluate operation completes successfully.
	OnEnd func(EvaluateOnEndEvent)
}

// ExperimentalEvaluate evaluates typed questions against one shared state.
// Experimental: may change in patch releases.
func ExperimentalEvaluate(ctx context.Context, opts EvaluateOptions) (*EvaluateResult, error) {
	model, err := resolveEvaluationModel(opts.Model, opts.Provider)
	if err != nil {
		return nil, err
	}

	if err := validateEvaluationInput(opts.State, opts.Questions); err != nil {
		return nil, err
	}

	supported := make(map[string]struct{}, len(model.SupportedQuestionTypes()))
	for _, t := range model.SupportedQuestionTypes() {
		supported[t] = struct{}{}
	}
	for id, q := range opts.Questions {
		if _, ok := supported[q.Type]; !ok {
			return nil, &providererrors.EvaluationUnsupportedQuestionTypeError{
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
	startEvent := EvaluateOnStartEvent{
		CallID:          callID,
		OperationID:     "ai.evaluate",
		RuntimeContext:  opts.RuntimeContext,
		Provider:        model.Provider(),
		ModelID:         model.ModelID(),
		State:           opts.State,
		Questions:       opts.Questions,
		MaxRetries:      resolvedMaxRetries,
		Headers:         opts.Headers,
		ProviderOptions: opts.ProviderOptions,
	}

	// Root "ai.evaluate" span. Mirrors TypeScript's restricted telemetry
	// dispatcher (packages/ai/src/evaluate/restricted-telemetry-dispatcher.ts):
	// evaluate's onStart/onEnd are routed to
	// experimental_onEvaluateStart/End instead of the generic dispatcher used
	// by generateText/embed/etc, so registered integrations decide how (and
	// whether) to create spans — core never touches OTel directly.
	ctx = telemetry.FireOnEvaluateStart(ctx, telemetry.EvaluateStartEvent{
		Settings:       telemetrySettings,
		CallID:         callID,
		OperationID:    "ai.evaluate",
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

	// Nested "ai.evaluate.doEvaluate" model-call event, wrapping the whole
	// retry loop for the underlying model call, mirrors the modelCallEvent
	// notify pair (experimental_onEvaluationModelCallStart/End) in evaluate.ts.
	telemetry.FireOnEvaluationModelCallStart(ctx, telemetry.EvaluationModelCallStartEvent{
		Settings:      telemetrySettings,
		CallID:        callID,
		OperationID:   "ai.evaluate.doEvaluate",
		ModelProvider: model.Provider(),
		ModelID:       model.ModelID(),
		State:         opts.State,
		Questions:     opts.Questions,
	})

	var result *provider.EvaluationResult
	err = withEmbedRetry(ctx, resolvedMaxRetries, func(callCtx context.Context) error {
		res, callErr := model.DoEvaluate(callCtx, provider.EvaluationCallOptions{
			State:           opts.State,
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
		// experimental_onEvaluationModelCallEnd is never notified on this
		// path (mirrors evaluate.ts); the registered integration is
		// responsible for defensively closing any nested span it opened at
		// OnEvaluationModelCallStart when it receives this CallID.
		telemetry.FireOnError(ctx, telemetry.TelemetryErrorEvent{Settings: telemetrySettings, Error: err, CallID: callID})
		return nil, err
	}

	if err := validateEvaluationAnswers(opts.Questions, result.Answers, result.Rounding); err != nil {
		telemetry.FireOnError(ctx, telemetry.TelemetryErrorEvent{Settings: telemetrySettings, Error: err, CallID: callID})
		return nil, err
	}

	usage := EvaluateUsage{}
	if result.Usage != nil {
		usage.InputTokens = result.Usage.InputTokens
		usage.OutputTokens = result.Usage.OutputTokens
		if result.Usage.InputTokens != nil && result.Usage.OutputTokens != nil {
			total := *result.Usage.InputTokens + *result.Usage.OutputTokens
			usage.TotalTokens = &total
		}
	}

	// Mirrors experimental_onEvaluationModelCallEnd: ai.evaluation.answers is
	// output-gated; usage and providerMetadata are unconditional.
	telemetry.FireOnEvaluationModelCallEnd(ctx, telemetry.EvaluationModelCallEndEvent{
		EvaluationModelCallStartEvent: telemetry.EvaluationModelCallStartEvent{
			Settings:      telemetrySettings,
			CallID:        callID,
			OperationID:   "ai.evaluate.doEvaluate",
			ModelProvider: model.Provider(),
			ModelID:       model.ModelID(),
			State:         opts.State,
			Questions:     opts.Questions,
		},
		Answers:          result.Answers,
		Usage:            telemetryUsageFromEvaluateUsage(usage),
		ProviderMetadata: result.ProviderMetadata,
	})

	logModelWarnings(result.Warnings, model.Provider(), model.ModelID())

	response := EvaluateResponse{ModelID: model.ModelID(), Timestamp: time.Now()}
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

	evalResult := &EvaluateResult{
		Answers:          result.Answers,
		Usage:            usage,
		Warnings:         warningsOrEmpty(result.Warnings),
		Rounding:         result.Rounding,
		ProviderMetadata: result.ProviderMetadata,
		Response:         response,
	}

	// Mirrors onEvaluateOperationEnd: ai.evaluation.answers is also recorded
	// on the root span, output-gated, and the root span is ended.
	telemetry.FireOnEvaluateEnd(ctx, telemetry.EvaluateEndEvent{
		EvaluateStartEvent: telemetry.EvaluateStartEvent{
			Settings:       telemetrySettings,
			CallID:         callID,
			OperationID:    "ai.evaluate",
			ModelProvider:  model.Provider(),
			ModelID:        model.ModelID(),
			RuntimeContext: telemetryRuntimeContext(telemetrySettings, opts.RuntimeContext),
		},
		Answers: evalResult.Answers,
	})

	if opts.OnEnd != nil {
		opts.OnEnd(EvaluateOnEndEvent{
			EvaluateOnStartEvent: startEvent,
			Answers:              evalResult.Answers,
			Usage:                evalResult.Usage,
			Warnings:             evalResult.Warnings,
			Rounding:             evalResult.Rounding,
			ProviderMetadata:     evalResult.ProviderMetadata,
			Response:             evalResult.Response,
		})
	}

	return evalResult, nil
}

func telemetryUsageFromEvaluateUsage(u EvaluateUsage) telemetry.TelemetryUsage {
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

// resolveEvaluationModel resolves model (a provider.EvaluationModel instance
// or a string ID) using resolver, falling back to the AI Gateway when
// resolver is nil and model is a string. Mirrors TypeScript's
// resolveEvaluationModel.
func resolveEvaluationModel(model interface{}, resolver provider.EvaluationModelProvider) (provider.EvaluationModel, error) {
	switch v := model.(type) {
	case provider.EvaluationModel:
		if v == nil {
			return nil, &providererrors.InvalidArgumentError{Field: "model", Message: "model is required"}
		}
		return v, nil
	case string:
		if resolver == nil {
			gw, err := gateway.Gateway()
			if err != nil {
				return nil, fmt.Errorf("failed to resolve the default AI Gateway provider for evaluation: %w", err)
			}
			resolver = gw
		}
		resolved, err := resolver.EvaluationModel(v)
		if err != nil {
			return nil, err
		}
		if resolved == nil {
			return nil, fmt.Errorf("no such evaluationModel: %s", v)
		}
		return resolved, nil
	case nil:
		return nil, &providererrors.InvalidArgumentError{Field: "model", Message: "model is required"}
	default:
		return nil, &providererrors.InvalidArgumentError{
			Field:   "model",
			Message: "model must be a provider.EvaluationModel instance or a string ID",
		}
	}
}

// --- Validation (mirrors TypeScript's evaluate/validate-evaluation.ts) ---

func isEvaluationRecord(value interface{}) (map[string]interface{}, bool) {
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

// isEvaluationInput reports whether value is a JSON-compatible string,
// map[string]interface{}, or []interface{} (mirrors TypeScript's isInput).
func isEvaluationInput(value interface{}) bool {
	switch value.(type) {
	case string:
		return true
	case []interface{}, map[string]interface{}:
		return isJSONCompatible(value)
	default:
		return false
	}
}

func invalidEvaluationInput(field string, message string) error {
	return &providererrors.InvalidArgumentError{Field: field, Message: message}
}

// validateEvaluationInput mirrors TypeScript's validateEvaluationInput.
func validateEvaluationInput(state interface{}, questions map[string]provider.EvaluationQuestion) error {
	if !isEvaluationInput(state) {
		return invalidEvaluationInput("state", "must be a JSON-compatible string, object, or array")
	}
	if len(questions) == 0 {
		return invalidEvaluationInput("questions", "must be a nonempty question map")
	}

	for id, q := range questions {
		field := fmt.Sprintf("questions.%s", id)
		if !isEvaluationInput(q.Instructions) {
			return invalidEvaluationInput(field, "instructions must be a JSON-compatible string, object, or array")
		}

		switch q.Type {
		case "choice":
			criteria, ok := isEvaluationRecord(q.Criteria)
			if !ok || len(criteria) == 0 {
				return invalidEvaluationInput(field, "choice criteria must be a nonempty option map")
			}
			for _, v := range criteria {
				if v != nil && !isEvaluationInput(v) {
					return invalidEvaluationInput(field, "criteria descriptions must be JSON-compatible strings, objects, arrays, or null")
				}
			}
		case "score":
			criteria, ok := q.Criteria.([]interface{})
			if !ok || len(criteria) < 2 {
				return invalidEvaluationInput(field, "score criteria must contain at least two ordered levels")
			}
			for _, v := range criteria {
				if v != nil && !isEvaluationInput(v) {
					return invalidEvaluationInput(field, "criteria descriptions must be JSON-compatible strings, objects, arrays, or null")
				}
			}
		case "boolean":
			if q.Criteria != nil {
				criteria, ok := isEvaluationRecord(q.Criteria)
				if !ok {
					return invalidEvaluationInput(field, "boolean criteria may only describe true and false")
				}
				for k, v := range criteria {
					if k != "true" && k != "false" {
						return invalidEvaluationInput(field, "boolean criteria may only describe true and false")
					}
					if v != nil && !isEvaluationInput(v) {
						return invalidEvaluationInput(field, "criteria descriptions must be JSON-compatible strings, objects, arrays, or null")
					}
				}
			}
		default:
			return invalidEvaluationInput(field, "question type must be choice, score, or boolean")
		}
	}
	return nil
}

const evaluationTolerance = 1e-6

func invalidEvaluationAnswer(answers interface{}, message string) error {
	return providererrors.NewInvalidResponseDataError(answers, message)
}

func isEvaluationProbability(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0) && v >= 0 && v <= 1
}

func evaluationRoundingError(decimals *int, answers interface{}) (float64, error) {
	if decimals == nil {
		return 0, nil
	}
	if *decimals < 0 || *decimals > 15 {
		return 0, invalidEvaluationAnswer(answers, "Evaluation rounding decimals must be integers between 0 and 15.")
	}
	return 0.5 * math.Pow(10, -float64(*decimals)), nil
}

func validateEvaluationDistribution(dist map[string]float64, keys []string, answers interface{}, id string, roundingErr float64) error {
	keySet := make(map[string]struct{}, len(keys))
	for _, k := range keys {
		keySet[k] = struct{}{}
	}
	if len(dist) != len(keySet) {
		return invalidEvaluationAnswer(answers, fmt.Sprintf("Question %q must have a complete distribution of finite probabilities in [0, 1].", id))
	}
	var sum float64
	for k, p := range dist {
		if _, ok := keySet[k]; !ok || !isEvaluationProbability(p) {
			return invalidEvaluationAnswer(answers, fmt.Sprintf("Question %q must have a complete distribution of finite probabilities in [0, 1].", id))
		}
		sum += p
	}
	if math.Abs(sum-1) > evaluationTolerance+float64(len(keys))*roundingErr {
		return invalidEvaluationAnswer(answers, fmt.Sprintf("Question %q probabilities must sum to 1 within the declared rounding precision.", id))
	}
	return nil
}

// validateEvaluationAnswers mirrors TypeScript's validateEvaluationAnswers.
func validateEvaluationAnswers(questions map[string]provider.EvaluationQuestion, answers map[string]provider.EvaluationAnswer, rounding *provider.EvaluationRounding) error {
	var probError, scoreError float64
	var err error
	if rounding != nil {
		probError, err = evaluationRoundingError(rounding.ProbabilityDecimals, answers)
		if err != nil {
			return err
		}
		scoreError, err = evaluationRoundingError(rounding.ScoreDecimals, answers)
		if err != nil {
			return err
		}
	}

	if len(answers) != len(questions) {
		return invalidEvaluationAnswer(answers, "Evaluation must return exactly one answer for every question.")
	}
	for id := range questions {
		if _, ok := answers[id]; !ok {
			return invalidEvaluationAnswer(answers, "Evaluation must return exactly one answer for every question.")
		}
	}

	for id, q := range questions {
		answer := answers[id]
		if answer.Type != q.Type {
			return invalidEvaluationAnswer(answers, fmt.Sprintf("Question %q returned an answer with the wrong type.", id))
		}

		switch q.Type {
		case "choice":
			criteria, _ := isEvaluationRecord(q.Criteria)
			if _, ok := criteria[answer.Choice]; answer.Choice == "" || !ok {
				return invalidEvaluationAnswer(answers, fmt.Sprintf("Question %q selected an unknown option.", id))
			}
			if answer.Probabilities != nil {
				keys := make([]string, 0, len(criteria))
				for k := range criteria {
					keys = append(keys, k)
				}
				if err := validateEvaluationDistribution(answer.Probabilities, keys, answers, id, probError); err != nil {
					return err
				}
				selected := answer.Probabilities[answer.Choice]
				for _, p := range answer.Probabilities {
					if p > selected+evaluationTolerance {
						return invalidEvaluationAnswer(answers, fmt.Sprintf("Question %q did not select a highest-probability option.", id))
					}
				}
			}
		case "score":
			criteria, _ := q.Criteria.([]interface{})
			maxScore := float64(len(criteria) - 1)
			if answer.Score == nil || !isFiniteFloat(*answer.Score) || *answer.Score < 0 || *answer.Score > maxScore {
				return invalidEvaluationAnswer(answers, fmt.Sprintf("Question %q score must be in [0, %v].", id, maxScore))
			}
			if answer.Probabilities != nil {
				keys := make([]string, len(criteria))
				for i := range criteria {
					keys[i] = strconv.Itoa(i)
				}
				if err := validateEvaluationDistribution(answer.Probabilities, keys, answers, id, probError); err != nil {
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
				if math.Abs(mean-*answer.Score) > evaluationTolerance+meanRoundingError+scoreError {
					return invalidEvaluationAnswer(answers, fmt.Sprintf("Question %q score must equal the probability-weighted mean within the declared rounding precision.", id))
				}
			}
		case "boolean":
			if answer.Probability == nil || !isEvaluationProbability(*answer.Probability) {
				return invalidEvaluationAnswer(answers, fmt.Sprintf("Question %q must return P(true) as a finite probability in [0, 1].", id))
			}
		}
	}
	return nil
}

func isFiniteFloat(v float64) bool {
	return !math.IsNaN(v) && !math.IsInf(v, 0)
}
