package jsonparser

import (
	"strings"
)

// FixJSON repairs incomplete or malformed JSON by closing unclosed structures
// This uses a simplified stack-based approach to track open braces/brackets
func FixJSON(jsonText string) string {
	if jsonText == "" {
		return ""
	}

	// Track what's open
	var openStack []rune
	inString := false
	escaped := false
	lastValidIndex := -1

	for i := 0; i < len(jsonText); i++ {
		char := rune(jsonText[i])

		if escaped {
			escaped = false
			lastValidIndex = i
			continue
		}

		if char == '\\' && inString {
			escaped = true
			lastValidIndex = i
			continue
		}

		if char == '"' {
			if inString {
				inString = false
			} else {
				inString = true
			}
			lastValidIndex = i
			continue
		}

		if inString {
			lastValidIndex = i
			continue
		}

		// Not in string, track braces and brackets
		switch char {
		case '{':
			openStack = append(openStack, '{')
			lastValidIndex = i
		case '[':
			openStack = append(openStack, '[')
			lastValidIndex = i
		case '}':
			if len(openStack) > 0 && openStack[len(openStack)-1] == '{' {
				openStack = openStack[:len(openStack)-1]
				lastValidIndex = i
			}
		case ']':
			if len(openStack) > 0 && openStack[len(openStack)-1] == '[' {
				openStack = openStack[:len(openStack)-1]
				lastValidIndex = i
			}
		case ',', ':', ' ', '\t', '\n', '\r', '0', '1', '2', '3', '4', '5', '6', '7', '8', '9',
			'-', '.', 'e', 'E', '+', 't', 'r', 'u', 'f', 'a', 'l', 's', 'n':
			// Valid JSON characters
			lastValidIndex = i
		}
	}

	// If nothing valid was found
	if lastValidIndex < 0 {
		return ""
	}

	// Start with valid portion
	result := jsonText[:lastValidIndex+1]

	// Close unclosed string
	if inString {
		result += "\""
	}

	// Handle incomplete literals at the end
	result = completeLiterals(result)

	// Close any open braces/brackets in reverse order
	for i := len(openStack) - 1; i >= 0; i-- {
		if openStack[i] == '{' {
			result += "}"
		} else if openStack[i] == '[' {
			result += "]"
		}
	}

	return result
}

// completeLiterals completes incomplete boolean/null literals at the end of the string
func completeLiterals(s string) string {
	// Check last few characters for incomplete literals
	// This handles cases like: {"active":tr -> {"active":true}

	// Find the last non-whitespace, non-punctuation sequence
	i := len(s) - 1
	for i >= 0 && (s[i] == ' ' || s[i] == '\t' || s[i] == '\n' || s[i] == '\r') {
		i--
	}

	if i < 0 {
		return s
	}

	// Extract potential partial literal (up to 5 chars for "false")
	start := i
	for start > 0 && s[start-1] >= 'a' && s[start-1] <= 'z' {
		start--
	}

	if start == i+1 {
		return s // No literal found
	}

	partial := s[start : i+1]

	// Check if it's a partial literal
	if strings.HasPrefix("true", partial) && partial != "true" {
		return s[:start] + "true"
	}
	if strings.HasPrefix("false", partial) && partial != "false" {
		return s[:start] + "false"
	}
	if strings.HasPrefix("null", partial) && partial != "null" {
		return s[:start] + "null"
	}

	return s
}
