package grokbuild

import (
	"encoding/json"
	"errors"
	"math"
	"strconv"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/acp"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

type grokBuildQuestionOption struct {
	Label       string  `json:"label"`
	Description string  `json:"description"`
	Preview     *string `json:"preview,omitempty"`
}

type grokBuildQuestion struct {
	Question    string                    `json:"question"`
	Options     []grokBuildQuestionOption `json:"options"`
	MultiSelect *bool                     `json:"multi_select,omitempty"`
}

type grokBuildQuestionRequest struct {
	SessionID  string              `json:"sessionId"`
	ToolCallID string              `json:"toolCallId"`
	Questions  []grokBuildQuestion `json:"questions"`
	Mode       string              `json:"mode"`
}

// AskUserQuestions is TS `grokBuildAskUserQuestions`, in the shape
// acp.CreateACP's Settings.AskUserQuestions field requires.
var AskUserQuestions = acp.AskUserQuestionsSettings{
	RequestMethod:        "_x.ai/ask_user_question",
	FromNativeRequest:    fromNativeRequest,
	ToNativeResponse:     toNativeResponse,
	MatchesNativeRequest: matchesNativeRequest,
}

// parseGrokBuildQuestionRequest mirrors TS
// `grokBuildQuestionRequestSchema.safeParse`: nativeRequest arrives already
// JSON-decoded (Go `any`, TS `unknown`), so parsing is a round trip through
// JSON to apply the schema's shape/required-field checks.
func parseGrokBuildQuestionRequest(nativeRequest any) (*grokBuildQuestionRequest, bool) {
	if !validGrokBuildQuestionRequestShape(nativeRequest) {
		return nil, false
	}
	data, err := json.Marshal(nativeRequest)
	if err != nil {
		return nil, false
	}
	var req grokBuildQuestionRequest
	if err := json.Unmarshal(data, &req); err != nil {
		return nil, false
	}
	if req.Mode != "default" && req.Mode != "plan" {
		return nil, false
	}
	return &req, true
}

func fromNativeRequest(nativeRequest any, _ *acp.ToolCall) *harness.ToolCallPart {
	req, ok := parseGrokBuildQuestionRequest(nativeRequest)
	if !ok {
		return nil
	}
	input := toHarnessQuestionsInput(req)
	inputJSON, err := json.Marshal(input)
	if err != nil {
		return nil
	}
	return &harness.ToolCallPart{
		ToolCallID:       req.ToolCallID,
		ToolName:         "askUserQuestions",
		NativeName:       "ask_user_question",
		Input:            string(inputJSON),
		ProviderExecuted: false,
	}
}

func toHarnessQuestionsInput(req *grokBuildQuestionRequest) harness.QuestionsToolInput {
	questions := make([]harness.Question, len(req.Questions))
	for i, q := range req.Questions {
		options := make([]harness.QuestionOption, len(q.Options))
		for j, o := range q.Options {
			options[j] = harness.QuestionOption{
				ID:          optionID(j),
				Label:       o.Label,
				Description: o.Description,
			}
			if o.Preview != nil {
				options[j].Preview = *o.Preview
			}
		}
		question := harness.Question{
			ID:            questionID(i),
			Question:      q.Question,
			Options:       options,
			AllowFreeForm: &harness.AllowFreeForm{Enabled: true},
		}
		if q.MultiSelect != nil {
			question.AllowMultiple = q.MultiSelect
		}
		questions[i] = question
	}
	return harness.QuestionsToolInput{AllowPartialAnswers: true, Questions: questions}
}

// toNativeResponse mirrors TS `toNativeResponse`: TS parses nativeRequest
// with the throwing `.parse()` (the request was already validated once by
// fromNativeRequest, so this is not expected to fail in practice) and
// panics, like the TS throw, if it somehow does; acp.AskUserQuestionsSettings
// has no error return for this callback.
func toNativeResponse(nativeRequest any, toolResult types.ToolResultContent) any {
	req, ok := parseGrokBuildQuestionRequest(nativeRequest)
	if !ok {
		panic("grokbuild: invalid native ask_user_question request")
	}
	output, err := parseQuestionsOutput(toolResult)
	if err != nil {
		panic(err)
	}
	if output.Action == harness.QuestionsActionCancelled {
		return map[string]any{"outcome": "cancelled"}
	}

	var answers map[string][]string
	annotations := map[string]map[string]string{}
	if output.Action == harness.QuestionsActionDeclined {
		answers = map[string][]string{}
	} else {
		answers, annotations = toNativeAnswers(req, output)
	}

	requestedOutcome := ""
	if toolResult.ProviderOptions != nil {
		if grokOpts, ok := toolResult.ProviderOptions["grok-build"].(map[string]any); ok {
			requestedOutcome, _ = grokOpts["outcome"].(string)
		}
	}
	if requestedOutcome == "chat_about_this" || requestedOutcome == "skip_interview" {
		partial := make(map[string]string, len(answers))
		for question, values := range answers {
			partial[question] = strings.Join(values, ", ")
		}
		return map[string]any{"outcome": requestedOutcome, "partial_answers": partial}
	}
	if output.Action == harness.QuestionsActionDeclined {
		return map[string]any{"outcome": "skip_interview", "partial_answers": map[string]string{}}
	}
	result := map[string]any{"outcome": "accepted", "answers": answers}
	if len(annotations) > 0 {
		result["annotations"] = annotations
	}
	return result
}

func toNativeAnswers(req *grokBuildQuestionRequest, output *harness.QuestionsToolOutput) (map[string][]string, map[string]map[string]string) {
	answers := map[string][]string{}
	annotations := map[string]map[string]string{}
	for i, q := range req.Questions {
		answer, ok := output.Answers[questionID(i)]
		if !ok {
			continue
		}
		// Non-nil: TS flatMap yields [] (JSON `[]`), never null.
		labels := []string{}
		for _, id := range answer.OptionIDs {
			idx, ok := positionalIDIndex(id, "option-")
			if !ok || idx < 0 || idx >= len(q.Options) {
				continue
			}
			labels = append(labels, q.Options[idx].Label)
		}
		if answer.Freeform != nil {
			labels = append(labels, "Other")
			annotations[q.Question] = map[string]string{"notes": *answer.Freeform}
		}
		answers[q.Question] = labels
	}
	return answers, annotations
}

func parseQuestionsOutput(toolResult types.ToolResultContent) (*harness.QuestionsToolOutput, error) {
	if toolResult.Output == nil || (toolResult.Output.Type != types.ToolResultOutputJSON && toolResult.Output.Type != types.ToolResultOutputErrorJSON) {
		return nil, errInvalidToolResult
	}
	data, err := json.Marshal(toolResult.Output.Value)
	if err != nil {
		return nil, err
	}
	return harness.ParseQuestionsToolOutput(data)
}

var errInvalidToolResult = errors.New("Grok Build askUserQuestions requires a JSON tool result.") //nolint:staticcheck // matches TS SDK's exact error text

func matchesNativeRequest(previousNativeRequest, nativeRequest any) bool {
	_, ok1 := parseGrokBuildQuestionRequest(previousNativeRequest)
	_, ok2 := parseGrokBuildQuestionRequest(nativeRequest)
	if !ok1 || !ok2 {
		return false
	}
	return questionFingerprint(previousNativeRequest) == questionFingerprint(nativeRequest)
}

// questionFingerprint mirrors TS `JSON.stringify({ questions, mode })` over the
// zod-parsed request. Zod strips unknown keys, emits the remaining keys in
// schema order, and keeps an explicit `null` for the nullable().optional()
// fields (`preview`, `multi_select`) while an absent key stays absent, so a
// request with `preview: null` fingerprints differently from one without it.
// The request has already passed validGrokBuildQuestionRequestShape.
func questionFingerprint(raw any) string {
	root, _ := raw.(map[string]any)
	questions, _ := root["questions"].([]any)
	var b strings.Builder
	b.WriteString(`{"questions":[`)
	for i, q := range questions {
		qm, _ := q.(map[string]any)
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(`{"question":`)
		b.WriteString(fingerprintJSON(qm["question"]))
		b.WriteString(`,"options":[`)
		options, _ := qm["options"].([]any)
		for j, o := range options {
			om, _ := o.(map[string]any)
			if j > 0 {
				b.WriteByte(',')
			}
			b.WriteString(`{"label":`)
			b.WriteString(fingerprintJSON(om["label"]))
			b.WriteString(`,"description":`)
			b.WriteString(fingerprintJSON(om["description"]))
			if v, present := om["preview"]; present {
				b.WriteString(`,"preview":`)
				b.WriteString(fingerprintJSON(v))
			}
			b.WriteByte('}')
		}
		b.WriteByte(']')
		if v, present := qm["multi_select"]; present {
			b.WriteString(`,"multi_select":`)
			b.WriteString(fingerprintJSON(v))
		}
		b.WriteByte('}')
	}
	b.WriteString(`],"mode":`)
	b.WriteString(fingerprintJSON(root["mode"]))
	b.WriteByte('}')
	return b.String()
}

func fingerprintJSON(v any) string {
	data, _ := json.Marshal(v)
	return string(data)
}

func questionID(index int) string { return "question-" + strconv.Itoa(index+1) }
func optionID(index int) string   { return "option-" + strconv.Itoa(index+1) }

// positionalIDIndex mirrors TS positionalIdIndex: `Number(suffix) - 1` must be
// a non-negative integer. JS Number() semantics apply to the suffix: it is
// whitespace-trimmed, empty is 0, and decimal ("2.0", "1e0") and 0x/0o/0b
// integer forms are accepted.
func positionalIDIndex(id, prefix string) (int, bool) {
	if !strings.HasPrefix(id, prefix) {
		return 0, false
	}
	n, ok := jsNumber(id[len(prefix):])
	if !ok {
		return 0, false
	}
	index := n - 1
	if index != math.Trunc(index) || index < 0 || index > math.MaxInt32 {
		return 0, false
	}
	return int(index), true
}

func jsNumber(s string) (float64, bool) {
	s = providerutils.JSTrim(s)
	if s == "" {
		return 0, true
	}
	if len(s) > 2 && s[0] == '0' {
		base := 0
		switch s[1] {
		case 'x', 'X':
			base = 16
		case 'o', 'O':
			base = 8
		case 'b', 'B':
			base = 2
		}
		if base != 0 {
			u, err := strconv.ParseUint(s[2:], base, 53)
			if err != nil || strings.ContainsAny(s[2:], "_+-") {
				return 0, false
			}
			return float64(u), true
		}
	}
	// Reject Go-only float syntax (underscores, hex floats, inf/nan words).
	for _, c := range s {
		if (c < '0' || c > '9') && c != '.' && c != 'e' && c != 'E' && c != '+' && c != '-' {
			return 0, false
		}
	}
	f, err := strconv.ParseFloat(s, 64)
	if err != nil || math.IsInf(f, 0) || math.IsNaN(f) {
		return 0, false
	}
	return f, true
}

// validGrokBuildQuestionRequestShape enforces the zod schema's required
// fields and types (json.Unmarshal alone accepts missing fields, nulls and
// absent arrays, which `safeParse` rejects).
func validGrokBuildQuestionRequestShape(v any) bool {
	root, ok := v.(map[string]any)
	if !ok {
		return false
	}
	isStr := func(m map[string]any, k string) bool { _, ok := m[k].(string); return ok }
	if !isStr(root, "sessionId") || !isStr(root, "toolCallId") || !isStr(root, "mode") {
		return false
	}
	questions, ok := root["questions"].([]any)
	if !ok {
		return false
	}
	// nullable().optional(): absent or null or the typed value.
	optional := func(m map[string]any, k string, typed func(any) bool) bool {
		val, present := m[k]
		return !present || val == nil || typed(val)
	}
	for _, q := range questions {
		qm, ok := q.(map[string]any)
		if !ok || !isStr(qm, "question") {
			return false
		}
		options, ok := qm["options"].([]any)
		if !ok {
			return false
		}
		for _, o := range options {
			om, ok := o.(map[string]any)
			if !ok || !isStr(om, "label") || !isStr(om, "description") {
				return false
			}
			if !optional(om, "preview", func(x any) bool { _, ok := x.(string); return ok }) {
				return false
			}
		}
		if !optional(qm, "multi_select", func(x any) bool { _, ok := x.(bool); return ok }) {
			return false
		}
	}
	return true
}
