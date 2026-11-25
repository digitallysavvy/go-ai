package scaledown

import (
	"context"
	"fmt"
	"math"
	"net/http"
	"sort"
	"strings"

	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/internal/jsonexact"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/jsonstr"
)

// decisionProviderName is the provider ID of ScaleDown decision models.
const decisionProviderName = "scaledown.decision"

// DecisionModel implements provider.DecisionModel for the ScaleDown Decisions
// API. Mirrors TS DecisionScaleDownModel: boolean questions are sent as the
// native "noul" type with their criteria folded into the instructions (the
// endpoint ignores noul criteria), structured instructions and criteria are
// sent as JSON text, and per-question confidence, score legends and the
// request cost are returned in ProviderMetadata["scaledown"].
type DecisionModel struct {
	provider *Provider
	modelID  string
}

// NewDecisionModel creates a ScaleDown decision model.
func NewDecisionModel(p *Provider, modelID string) *DecisionModel {
	return &DecisionModel{provider: p, modelID: modelID}
}

// SpecificationVersion returns "v4".
func (m *DecisionModel) SpecificationVersion() string { return "v4" }

// Provider returns "scaledown.decision".
func (m *DecisionModel) Provider() string { return decisionProviderName }

// ModelID returns the model ID.
func (m *DecisionModel) ModelID() string { return m.modelID }

// SupportedQuestionTypes reports choice, boolean and score.
func (m *DecisionModel) SupportedQuestionTypes() []string {
	return []string{"choice", "boolean", "score"}
}

// toText is TS toText: a string as is, anything else its JSON text.
func toText(value interface{}) (string, error) {
	if s, ok := value.(string); ok {
		return s, nil
	}
	encoded, err := jsonstr.Marshal(value)
	if err != nil {
		return "", err
	}
	return string(encoded), nil
}

// DoDecide decides every question against the same state in one request.
func (m *DecisionModel) DoDecide(ctx context.Context, opts provider.DecisionCallOptions) (*provider.DecisionResult, error) {
	// Typed Go containers in Instructions / Criteria take their JSON form
	// (TS providers always receive JSON).
	opts.Questions = provider.DecisionQuestionsJSON(opts.Questions)
	ids := opts.QuestionIDs()
	questions := providerutils.OrderedMap{Keys: ids, Vals: make(map[string]interface{}, len(ids))}
	for _, id := range ids {
		q := opts.Questions[id]
		var instructions string
		haveInstructions := q.Instructions != nil
		if haveInstructions {
			text, err := toText(q.Instructions)
			if err != nil {
				return nil, err
			}
			instructions = text
		}
		wire := providerutils.OrderedMap{Keys: []string{"type"}, Vals: map[string]interface{}{}}
		withInstructions := func(text string) {
			wire.Keys = append(wire.Keys, "instructions")
			wire.Vals["instructions"] = text
		}
		switch q.Type {
		case "choice":
			wire.Vals["type"] = "choice"
			if haveInstructions {
				withInstructions(instructions)
			}
			options, _ := q.Criteria.(map[string]interface{})
			keys := provider.OrderedKeys(options, opts.CriteriaOrder[id])
			criteria := providerutils.OrderedMap{Keys: keys, Vals: make(map[string]interface{}, len(keys))}
			for _, key := range keys {
				value := options[key]
				if value == nil {
					criteria.Vals[key] = key
					continue
				}
				text, err := toText(value)
				if err != nil {
					return nil, err
				}
				criteria.Vals[key] = text
			}
			wire.Keys = append(wire.Keys, "criteria")
			wire.Vals["criteria"] = criteria
		case "boolean":
			wire.Vals["type"] = "noul"
			// The native endpoint ignores noul criteria, so they are included
			// in its instructions to preserve their meaning.
			var lines []string
			if haveInstructions {
				lines = append(lines, instructions)
			}
			options, _ := q.Criteria.(map[string]interface{})
			for _, key := range provider.OrderedKeys(options, opts.CriteriaOrder[id]) {
				if options[key] == nil {
					continue
				}
				text, err := toText(options[key])
				if err != nil {
					return nil, err
				}
				answer := "no"
				if key == "true" {
					answer = "yes"
				}
				lines = append(lines, fmt.Sprintf("Answer %s when: %s", answer, text))
			}
			withInstructions(strings.Join(lines, "\n"))
		case "score":
			levels, _ := q.Criteria.([]interface{})
			nullLevel := false
			for _, level := range levels {
				if level == nil {
					nullLevel = true
				}
			}
			if len(levels) < 2 || len(levels) > 10 || nullLevel {
				return nil, providererrors.NewArgumentError(fmt.Sprintf("questions.%s.criteria", id), "ScaleDown Score questions require 2 to 10 non-null levels.", nil)
			}
			wire.Vals["type"] = "score"
			if haveInstructions {
				withInstructions(instructions)
			}
			texts := make([]interface{}, len(levels))
			for i, level := range levels {
				text, err := toText(level)
				if err != nil {
					return nil, err
				}
				texts[i] = text
			}
			wire.Keys = append(wire.Keys, "criteria")
			wire.Vals["criteria"] = texts
		default:
			return nil, &providererrors.DecisionUnsupportedQuestionTypeError{
				QuestionID: id, QuestionType: q.Type, Provider: m.Provider(), ModelID: m.modelID,
			}
		}
		questions.Vals[id] = wire
	}

	// TS evaluates the postJsonToApi headers (loadApiKey) before the body
	// (convertToScaleDownState), so a missing key wins over an unsupported file.
	headers, err := m.provider.resolveHeaders()
	if err != nil {
		return nil, err
	}
	headers = internalhttp.MergeHeaders(headers, opts.Headers)
	state, err := convertToScaleDownState(provider.DecisionStateParts(opts.State))
	if err != nil {
		return nil, err
	}

	body := providerutils.OrderedMap{Keys: []string{"model", "state", "questions"}, Vals: map[string]interface{}{
		"model": m.modelID, "state": state, "questions": questions,
	}}
	var response scaledownDecisionResponse
	httpResp, err := m.provider.client.DoJSONResponse(ctx, internalhttp.Request{
		Method:           http.MethodPost,
		Path:             "/v1/scaledown",
		Body:             body,
		Headers:          headers,
		ValidateResponse: validateScaleDownDecisionResponse,
	}, &response)
	if err != nil {
		return nil, scaledownHTTPError(err, decisionProviderName)
	}

	var rawBody interface{}
	_ = jsonexact.Unmarshal(httpResp.Body, &rawBody)

	answerIDs := make([]string, 0, len(response.Answers))
	for id := range response.Answers {
		answerIDs = append(answerIDs, id)
	}
	sort.Strings(answerIDs)

	answers := make(map[string]provider.DecisionAnswer, len(answerIDs))
	confidence := map[string]interface{}{}
	legend := map[string]interface{}{}
	for _, id := range answerIDs {
		a := response.Answers[id]
		switch a.Type {
		case "noul":
			p := *a.Noul
			answers[id] = provider.DecisionAnswer{Type: "boolean", Probability: &p}
		case "choice":
			answers[id] = provider.DecisionAnswer{Type: "choice", Choice: *a.Choice, Probabilities: a.Probabilities}
		case "score":
			s := *a.Score
			answers[id] = provider.DecisionAnswer{Type: "score", Score: &s, Probabilities: a.Probabilities}
			if a.Legend != nil {
				legend[id] = a.Legend
			}
		}
		if a.Type != "noul" && a.Confidence != nil {
			confidence[id] = *a.Confidence
		}
	}
	metadata := map[string]interface{}{"confidence": confidence, "legend": legend}
	usage := &provider.DecisionUsage{}
	if response.Usage != nil {
		usage.InputTokens = scaledownTokenCount(response.Usage.InputTokens)
		usage.OutputTokens = scaledownTokenCount(response.Usage.OutputTokens)
		if response.Usage.Cost != nil {
			metadata["cost"] = *response.Usage.Cost
		}
	}

	var warnings []types.Warning
	if opts.MaxOutputTokens != nil {
		warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "maxOutputTokens"})
	}
	if opts.Reasoning != nil {
		warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "reasoning"})
	}
	// Go-typed literals read like their JSON form (OVC-01).
	if raw, ok := providerutils.NormalizeOptionsJSON(opts.ProviderOptions)["scaledown"].(map[string]interface{}); ok {
		keys := make([]string, 0, len(raw))
		for k := range raw {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "providerOptions.scaledown." + k})
		}
	}

	modelID := m.modelID
	// TS `value.model ?? this.modelId`: only null or absent falls back.
	if response.Model != nil {
		modelID = *response.Model
	}
	return &provider.DecisionResult{
		Answers:          answers,
		Usage:            usage,
		Warnings:         warnings,
		ProviderMetadata: map[string]interface{}{"scaledown": metadata},
		Response: &provider.DecisionResponseInfo{
			ModelID: modelID,
			Headers: lowerHeaders(providerutils.ExtractHeaders(httpResp.Headers)),
			Body:    rawBody,
		},
	}, nil
}

// scaledownTokenCount maps a usage count to the integral Go usage field:
// truncated toward zero and clamped to the int range.
func scaledownTokenCount(v *float64) *int {
	if v == nil {
		return nil
	}
	f := math.Trunc(*v)
	var n int
	switch {
	case f >= math.MaxInt:
		n = math.MaxInt
	case f <= math.MinInt:
		n = math.MinInt
	default:
		n = int(f)
	}
	return &n
}

// scaledownDecisionResponse is the decoded successful response (validated by
// scaledownResponseNode first).
type scaledownDecisionResponse struct {
	Model   *string                            `json:"model"`
	Answers map[string]scaledownDecisionAnswer `json:"answers"`
	Usage   *struct {
		InputTokens  *float64 `json:"input_tokens"`
		OutputTokens *float64 `json:"output_tokens"`
		Cost         *float64 `json:"cost"`
	} `json:"usage"`
}

type scaledownDecisionAnswer struct {
	Type          string             `json:"type"`
	Noul          *float64           `json:"noul"`
	Choice        *string            `json:"choice"`
	Score         *float64           `json:"score"`
	Probabilities map[string]float64 `json:"probabilities"`
	Confidence    *float64           `json:"confidence"`
	Legend        map[string]string  `json:"legend"`
}

// lowerHeaders lowercases header names, as TS response headers are.
func lowerHeaders(in map[string]string) map[string]string {
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[strings.ToLower(k)] = v
	}
	return out
}
