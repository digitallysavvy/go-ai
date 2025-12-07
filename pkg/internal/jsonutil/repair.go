package jsonutil

import (
	"encoding/json"
	"regexp"
	"strings"
)

// FixJSON attempts to repair malformed JSON strings
// Common fixes include:
// - Removing trailing commas
// - Adding missing closing brackets/braces
// - Fixing unquoted keys
// - Removing comments
func FixJSON(s string) (string, error) {
	// Try parsing as-is first
	var test interface{}
	if err := json.Unmarshal([]byte(s), &test); err == nil {
		return s, nil
	}

	fixed := s

	// Remove trailing commas before closing brackets/braces
	fixed = removeTrailingCommas(fixed)

	// Remove JavaScript-style comments
	fixed = removeComments(fixed)

	// Fix unquoted keys
	fixed = fixUnquotedKeys(fixed)

	// Fix single quotes to double quotes
	fixed = fixSingleQuotes(fixed)

	// Balance brackets and braces
	fixed = balanceBrackets(fixed)

	// Try parsing again
	if err := json.Unmarshal([]byte(fixed), &test); err == nil {
		return fixed, nil
	}

	// If still failing, try more aggressive fixes
	fixed = aggressiveFix(fixed)

	// Final parse attempt
	if err := json.Unmarshal([]byte(fixed), &test); err != nil {
		return s, err // Return original if we can't fix it
	}

	return fixed, nil
}

// removeTrailingCommas removes commas before closing brackets/braces
func removeTrailingCommas(s string) string {
	// Remove comma before }
	re1 := regexp.MustCompile(`,\s*}`)
	s = re1.ReplaceAllString(s, "}")

	// Remove comma before ]
	re2 := regexp.MustCompile(`,\s*]`)
	s = re2.ReplaceAllString(s, "]")

	return s
}

// removeComments removes JavaScript-style comments
func removeComments(s string) string {
	// Remove single-line comments
	re1 := regexp.MustCompile(`//.*`)
	s = re1.ReplaceAllString(s, "")

	// Remove multi-line comments
	re2 := regexp.MustCompile(`/\*[\s\S]*?\*/`)
	s = re2.ReplaceAllString(s, "")

	return s
}

// fixUnquotedKeys adds quotes to unquoted object keys
func fixUnquotedKeys(s string) string {
	// Match unquoted keys like: {key: "value"}
	// This is a simplified version - may not handle all edge cases
	re := regexp.MustCompile(`(\{|,)\s*([a-zA-Z_][a-zA-Z0-9_]*)\s*:`)
	s = re.ReplaceAllString(s, `$1"$2":`)
	return s
}

// fixSingleQuotes replaces single quotes with double quotes
// Note: This is a naive implementation that may not work for strings containing single quotes
func fixSingleQuotes(s string) string {
	// Simple replacement - doesn't handle escaped quotes
	inString := false
	var result strings.Builder

	for i := 0; i < len(s); i++ {
		ch := s[i]

		if ch == '\'' {
			result.WriteByte('"')
			inString = !inString
		} else if ch == '\\' && i+1 < len(s) {
			// Handle escaped characters
			result.WriteByte(ch)
			i++
			if i < len(s) {
				result.WriteByte(s[i])
			}
		} else {
			result.WriteByte(ch)
		}
	}

	return result.String()
}

// balanceBrackets attempts to balance unclosed brackets and braces
func balanceBrackets(s string) string {
	s = strings.TrimSpace(s)

	openBraces := 0
	openBrackets := 0

	for _, ch := range s {
		switch ch {
		case '{':
			openBraces++
		case '}':
			openBraces--
		case '[':
			openBrackets++
		case ']':
			openBrackets--
		}
	}

	// Add missing closing characters
	for i := 0; i < openBrackets; i++ {
		s += "]"
	}
	for i := 0; i < openBraces; i++ {
		s += "}"
	}

	return s
}

// aggressiveFix attempts more aggressive repairs
func aggressiveFix(s string) string {
	s = strings.TrimSpace(s)

	// Remove any trailing incomplete content after last valid object/array
	lastBrace := strings.LastIndex(s, "}")
	lastBracket := strings.LastIndex(s, "]")

	cutoff := -1
	if lastBrace > lastBracket {
		cutoff = lastBrace + 1
	} else if lastBracket > lastBrace {
		cutoff = lastBracket + 1
	}

	if cutoff > 0 && cutoff < len(s) {
		s = s[:cutoff]
	}

	return s
}

// ParsePartialJSON attempts to parse incomplete JSON
// Useful for streaming scenarios where JSON is being built incrementally
func ParsePartialJSON(s string) (interface{}, error) {
	s = strings.TrimSpace(s)

	if s == "" {
		return nil, nil
	}

	// Try parsing as complete JSON first
	var result interface{}
	if err := json.Unmarshal([]byte(s), &result); err == nil {
		return result, nil
	}

	// Try fixing and parsing
	fixed, err := FixJSON(s)
	if err == nil {
		if err := json.Unmarshal([]byte(fixed), &result); err == nil {
			return result, nil
		}
	}

	// If it's an incomplete object/array, try to complete it
	if strings.HasPrefix(s, "{") {
		balanced := balanceBrackets(s)
		if err := json.Unmarshal([]byte(balanced), &result); err == nil {
			return result, nil
		}
	} else if strings.HasPrefix(s, "[") {
		balanced := balanceBrackets(s)
		if err := json.Unmarshal([]byte(balanced), &result); err == nil {
			return result, nil
		}
	}

	return nil, json.Unmarshal([]byte(s), &result)
}

// IsPartiallyValid checks if a string looks like it could be valid JSON if completed
func IsPartiallyValid(s string) bool {
	s = strings.TrimSpace(s)

	if s == "" {
		return false
	}

	// Check if it starts with valid JSON opening
	if !strings.HasPrefix(s, "{") && !strings.HasPrefix(s, "[") && !strings.HasPrefix(s, "\"") {
		return false
	}

	// Try to parse as partial
	_, err := ParsePartialJSON(s)
	return err == nil
}
