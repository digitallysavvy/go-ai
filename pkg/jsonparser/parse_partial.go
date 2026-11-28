package jsonparser

import (
	"encoding/json"

	"github.com/gatewaytofredom/pjson"
)

// ParseState represents the state of JSON parsing
type ParseState string

const (
	// ParseStateUndefinedInput indicates the input was undefined.
	ParseStateUndefinedInput ParseState = "undefined-input"

	// ParseStateSuccessful indicates JSON was parsed successfully without repair
	ParseStateSuccessful ParseState = "successful-parse"

	// ParseStateRepaired indicates JSON was repaired and then parsed successfully
	ParseStateRepaired ParseState = "repaired-parse"

	// ParseStateFailed indicates parsing failed even after repair
	ParseStateFailed ParseState = "failed-parse"
)

// ParseResult contains the result of partial JSON parsing
type ParseResult struct {
	// Value is the parsed JSON value (can be any JSON type)
	Value interface{}

	// State indicates how the JSON was parsed
	State ParseState

	// Error contains the error if parsing failed
	Error error
}

// ParsePartialJSON attempts to parse potentially incomplete JSON.
// It uses pjson to scan and auto-close incomplete structures in a single pass.
func ParsePartialJSON(jsonText string) ParseResult {
	if jsonText == "" {
		var value interface{}
		err := json.Unmarshal([]byte(jsonText), &value)
		return ParseResult{
			Value: nil,
			State: ParseStateFailed,
			Error: err,
		}
	}

	p := pjson.New(
		pjson.WithMaxDepth(512),
		pjson.WithCompleteLiterals(),
		pjson.WithInitialCapacity(len(jsonText)+16),
	)
	if _, err := p.WriteString(jsonText); err != nil {
		var value interface{}
		return ParseResult{
			Value: nil,
			State: ParseStateFailed,
			Error: json.Unmarshal([]byte(jsonText), &value),
		}
	}

	var value interface{}
	if p.Done() {
		if err := json.Unmarshal([]byte(jsonText), &value); err == nil {
			return ParseResult{
				Value: value,
				State: ParseStateSuccessful,
				Error: nil,
			}
		}
	}

	snap := p.CompleteJSON()
	if len(snap) == 0 {
		return ParseResult{
			Value: nil,
			State: ParseStateFailed,
			Error: json.Unmarshal([]byte(jsonText), &value),
		}
	}

	if err := json.Unmarshal(snap, &value); err == nil {
		return ParseResult{
			Value: value,
			State: ParseStateRepaired,
			Error: nil,
		}
	}

	return ParseResult{
		Value: nil,
		State: ParseStateFailed,
		Error: json.Unmarshal([]byte(jsonText), &value),
	}
}
