package grokbuild

import (
	"encoding/json"
	"errors"
	"strconv"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/harness"
	"github.com/digitallysavvy/go-ai/pkg/harness/acp"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
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
		var labels []string
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

var errInvalidToolResult = errors.New("Grok Build askUserQuestions requires a JSON tool result.")

func matchesNativeRequest(previousNativeRequest, nativeRequest any) bool {
	previous, ok1 := parseGrokBuildQuestionRequest(previousNativeRequest)
	current, ok2 := parseGrokBuildQuestionRequest(nativeRequest)
	if !ok1 || !ok2 {
		return false
	}
	return questionFingerprint(previous) == questionFingerprint(current)
}

func questionFingerprint(req *grokBuildQuestionRequest) string {
	data, _ := json.Marshal(map[string]any{"questions": req.Questions, "mode": req.Mode})
	return string(data)
}

func questionID(index int) string { return "question-" + strconv.Itoa(index+1) }
func optionID(index int) string   { return "option-" + strconv.Itoa(index+1) }

func positionalIDIndex(id, prefix string) (int, bool) {
	if !strings.HasPrefix(id, prefix) {
		return 0, false
	}
	n, err := strconv.Atoi(id[len(prefix):])
	if err != nil {
		return 0, false
	}
	index := n - 1
	return index, index >= 0
}
