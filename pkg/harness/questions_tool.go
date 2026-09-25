package harness

import (
	"encoding/json"
	"errors"
	"fmt"
)

// QuestionsToolDescription is the description of the askUserQuestions tool.
const QuestionsToolDescription = "Ask the user one or more questions"

// QuestionOption is one selectable answer for a Question.
type QuestionOption struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description,omitempty"`
	Preview     string `json:"preview,omitempty"`
}

// AllowFreeForm is `boolean | { secret: boolean }`.
type AllowFreeForm struct {
	// Enabled is the boolean form (ignored when Secret is set).
	Enabled bool
	// Secret, when non-nil, selects the `{ secret }` object form.
	Secret *bool
}

// MarshalJSON encodes either a boolean or `{ "secret": bool }`.
func (a AllowFreeForm) MarshalJSON() ([]byte, error) {
	if a.Secret != nil {
		return json.Marshal(struct {
			Secret bool `json:"secret"`
		}{*a.Secret})
	}
	return json.Marshal(a.Enabled)
}

// UnmarshalJSON decodes either a boolean or `{ "secret": bool }`.
func (a *AllowFreeForm) UnmarshalJSON(data []byte) error {
	var b bool
	if err := json.Unmarshal(data, &b); err == nil {
		*a = AllowFreeForm{Enabled: b}
		return nil
	}
	var obj struct {
		Secret *bool `json:"secret"`
	}
	if err := json.Unmarshal(data, &obj); err != nil || obj.Secret == nil {
		return errors.New("allowFreeForm must be a boolean or { secret: boolean }")
	}
	*a = AllowFreeForm{Enabled: true, Secret: obj.Secret}
	return nil
}

// Question is one question in QuestionsToolInput.
type Question struct {
	ID            string           `json:"id"`
	Question      string           `json:"question"`
	Header        string           `json:"header,omitempty"`
	Options       []QuestionOption `json:"options,omitempty"`
	AllowMultiple *bool            `json:"allowMultiple,omitempty"`
	AllowFreeForm *AllowFreeForm   `json:"allowFreeForm,omitempty"`
}

// QuestionsToolInput is the askUserQuestions input. Mirrors TS
// `HarnessV1QuestionsToolInput`.
type QuestionsToolInput struct {
	AllowPartialAnswers bool       `json:"allowPartialAnswers"`
	Questions           []Question `json:"questions"`
}

// ParseQuestionsToolInput decodes and validates input like
// `harnessV1QuestionsToolInputSchema.parse`.
func ParseQuestionsToolInput(data []byte) (*QuestionsToolInput, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return nil, fmt.Errorf("invalid askUserQuestions input: %w", err)
	}
	if raw, ok := fields["allowPartialAnswers"]; !ok || string(raw) == "null" {
		return nil, errors.New("invalid askUserQuestions input: allowPartialAnswers is required")
	}
	var input QuestionsToolInput
	if err := json.Unmarshal(data, &input); err != nil {
		return nil, fmt.Errorf("invalid askUserQuestions input: %w", err)
	}
	if err := input.Validate(); err != nil {
		return nil, err
	}
	return &input, nil
}

// Validate applies the zod min-length constraints.
func (in *QuestionsToolInput) Validate() error {
	if len(in.Questions) == 0 {
		return errors.New("invalid askUserQuestions input: at least one question is required")
	}
	for i, q := range in.Questions {
		if q.ID == "" || q.Question == "" {
			return fmt.Errorf("invalid askUserQuestions input: question %d requires a non-empty id and question", i)
		}
		for j, o := range q.Options {
			if o.ID == "" || o.Label == "" {
				return fmt.Errorf("invalid askUserQuestions input: question %d option %d requires a non-empty id and label", i, j)
			}
		}
	}
	return nil
}

// Questions tool output actions.
const (
	QuestionsActionAnswered          = "answered"
	QuestionsActionPartiallyAnswered = "partially-answered"
	QuestionsActionDeclined          = "declined"
	QuestionsActionCancelled         = "cancelled"
)

// QuestionAnswer is the answer to one question.
type QuestionAnswer struct {
	OptionIDs []string `json:"optionIds"`
	Freeform  *string  `json:"freeform,omitempty"`
}

// MarshalJSON always emits optionIds as an array.
func (a QuestionAnswer) MarshalJSON() ([]byte, error) {
	type alias QuestionAnswer
	b := alias(a)
	if b.OptionIDs == nil {
		b.OptionIDs = []string{}
	}
	return json.Marshal(b)
}

// QuestionsToolOutput is the askUserQuestions output (discriminated on
// action). Answers is only serialized for answered / partially-answered.
type QuestionsToolOutput struct {
	Action  string                    `json:"action"`
	Answers map[string]QuestionAnswer `json:"answers,omitempty"`
}

// MarshalJSON emits `answers` exactly for the answered variants.
func (o QuestionsToolOutput) MarshalJSON() ([]byte, error) {
	switch o.Action {
	case QuestionsActionAnswered, QuestionsActionPartiallyAnswered:
		answers := o.Answers
		if answers == nil {
			answers = map[string]QuestionAnswer{}
		}
		return json.Marshal(struct {
			Action  string                    `json:"action"`
			Answers map[string]QuestionAnswer `json:"answers"`
		}{o.Action, answers})
	default:
		return json.Marshal(struct {
			Action string `json:"action"`
		}{o.Action})
	}
}

// ParseQuestionsToolOutput decodes and validates output like
// `harnessV1QuestionsToolOutputSchema.parse`.
func ParseQuestionsToolOutput(data []byte) (*QuestionsToolOutput, error) {
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(data, &fields); err != nil {
		return nil, fmt.Errorf("invalid askUserQuestions output: %w", err)
	}
	var out QuestionsToolOutput
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, fmt.Errorf("invalid askUserQuestions output: %w", err)
	}
	switch out.Action {
	case QuestionsActionAnswered, QuestionsActionPartiallyAnswered:
		if raw, ok := fields["answers"]; !ok || string(raw) == "null" {
			return nil, errors.New("invalid askUserQuestions output: answers is required")
		}
		for id, answer := range out.Answers {
			if answer.OptionIDs == nil {
				return nil, fmt.Errorf("invalid askUserQuestions output: answer %q requires optionIds", id)
			}
		}
	case QuestionsActionDeclined, QuestionsActionCancelled:
		out.Answers = nil
	default:
		return nil, fmt.Errorf("invalid askUserQuestions output action %q", out.Action)
	}
	return &out, nil
}

// QuestionsToolInputJSONSchema returns the JSON Schema of the input (the
// JSON Schema zod produces for `harnessV1QuestionsToolInputSchema`).
func QuestionsToolInputJSONSchema() map[string]any {
	option := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id":          map[string]any{"type": "string", "minLength": 1},
			"label":       map[string]any{"type": "string", "minLength": 1},
			"description": map[string]any{"type": "string"},
			"preview":     map[string]any{"type": "string"},
		},
		"required":             []any{"id", "label"},
		"additionalProperties": false,
	}
	question := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"id":       map[string]any{"type": "string", "minLength": 1},
			"question": map[string]any{"type": "string", "minLength": 1},
			"header":   map[string]any{"type": "string"},
			"options":  map[string]any{"type": "array", "items": option},
			"allowMultiple": map[string]any{
				"type": "boolean",
			},
			"allowFreeForm": map[string]any{
				"anyOf": []any{
					map[string]any{"type": "boolean"},
					map[string]any{
						"type":                 "object",
						"properties":           map[string]any{"secret": map[string]any{"type": "boolean"}},
						"required":             []any{"secret"},
						"additionalProperties": false,
					},
				},
			},
		},
		"required":             []any{"id", "question"},
		"additionalProperties": false,
	}
	return map[string]any{
		"$schema": "http://json-schema.org/draft-07/schema#",
		"type":    "object",
		"properties": map[string]any{
			"allowPartialAnswers": map[string]any{"type": "boolean"},
			"questions":           map[string]any{"type": "array", "items": question, "minItems": 1},
		},
		"required":             []any{"allowPartialAnswers", "questions"},
		"additionalProperties": false,
	}
}

// QuestionsToolOutputJSONSchema returns the JSON Schema of the output.
func QuestionsToolOutputJSONSchema() map[string]any {
	answer := map[string]any{
		"type": "object",
		"properties": map[string]any{
			"optionIds": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			"freeform":  map[string]any{"type": "string"},
		},
		"required":             []any{"optionIds"},
		"additionalProperties": false,
	}
	answered := func(action string) map[string]any {
		return map[string]any{
			"type": "object",
			"properties": map[string]any{
				"action":  map[string]any{"type": "string", "const": action},
				"answers": map[string]any{"type": "object", "additionalProperties": answer},
			},
			"required":             []any{"action", "answers"},
			"additionalProperties": false,
		}
	}
	bare := func(action string) map[string]any {
		return map[string]any{
			"type":                 "object",
			"properties":           map[string]any{"action": map[string]any{"type": "string", "const": action}},
			"required":             []any{"action"},
			"additionalProperties": false,
		}
	}
	return map[string]any{
		"$schema": "http://json-schema.org/draft-07/schema#",
		"oneOf": []any{
			answered(QuestionsActionAnswered),
			answered(QuestionsActionPartiallyAnswered),
			bare(QuestionsActionDeclined),
			bare(QuestionsActionCancelled),
		},
	}
}
