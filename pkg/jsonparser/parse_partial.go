package jsonparser

import (
	"encoding/json"
)

// ParseState represents the state of JSON parsing
type ParseState string

const (
	// ParseStateUndefinedInput indicates the input was undefined/empty
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

// ParsePartialJSON attempts to parse potentially incomplete JSON
// It first tries to parse the JSON as-is, and if that fails, uses FixJSON
// to repair incomplete structures before parsing again
func ParsePartialJSON(jsonText string) ParseResult {
	// Handle empty/undefined input
	if jsonText == "" {
		return ParseResult{
			Value: nil,
			State: ParseStateUndefinedInput,
			Error: nil,
		}
	}

	// Phase 1: Try direct parsing
	var value interface{}
	err := json.Unmarshal([]byte(jsonText), &value)
	if err == nil {
		return ParseResult{
			Value: value,
			State: ParseStateSuccessful,
			Error: nil,
		}
	}

	// Phase 2: Try repair and parse
	repairedJSON := FixJSON(jsonText)
	if repairedJSON == "" {
		return ParseResult{
			Value: nil,
			State: ParseStateFailed,
			Error: err,
		}
	}

	err = json.Unmarshal([]byte(repairedJSON), &value)
	if err == nil {
		return ParseResult{
			Value: value,
			State: ParseStateRepaired,
			Error: nil,
		}
	}

	// Both attempts failed
	return ParseResult{
		Value: nil,
		State: ParseStateFailed,
		Error: err,
	}
}
