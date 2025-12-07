package jsonutil

import (
	"encoding/json"
	"strings"
)

// StreamingParser handles incremental JSON parsing for streaming scenarios
type StreamingParser struct {
	buffer      strings.Builder
	lastValid   interface{}
	accumulated string
}

// NewStreamingParser creates a new streaming JSON parser
func NewStreamingParser() *StreamingParser {
	return &StreamingParser{}
}

// Append adds more data to the parser
func (p *StreamingParser) Append(data string) {
	p.buffer.WriteString(data)
	p.accumulated = p.buffer.String()
}

// TryParse attempts to parse the accumulated data
// Returns the parsed result and a boolean indicating if parsing succeeded
func (p *StreamingParser) TryParse() (interface{}, bool) {
	current := p.accumulated

	if current == "" {
		return nil, false
	}

	// Try direct parse
	var result interface{}
	if err := json.Unmarshal([]byte(current), &result); err == nil {
		p.lastValid = result
		return result, true
	}

	// Try with partial parsing
	if partial, err := ParsePartialJSON(current); err == nil && partial != nil {
		p.lastValid = partial
		return partial, true
	}

	return p.lastValid, false
}

// GetCurrent returns the current accumulated string
func (p *StreamingParser) GetCurrent() string {
	return p.accumulated
}

// GetLastValid returns the last successfully parsed value
func (p *StreamingParser) GetLastValid() interface{} {
	return p.lastValid
}

// Reset clears the parser state
func (p *StreamingParser) Reset() {
	p.buffer.Reset()
	p.lastValid = nil
	p.accumulated = ""
}

// ObjectStreamingParser handles streaming of objects with progressive field updates
type ObjectStreamingParser struct {
	buffer      strings.Builder
	accumulated string
	fields      map[string]interface{}
}

// NewObjectStreamingParser creates a new object streaming parser
func NewObjectStreamingParser() *ObjectStreamingParser {
	return &ObjectStreamingParser{
		fields: make(map[string]interface{}),
	}
}

// Append adds more data to the parser
func (p *ObjectStreamingParser) Append(data string) {
	p.buffer.WriteString(data)
	p.accumulated = p.buffer.String()

	// Try to extract completed fields
	p.extractFields()
}

// extractFields attempts to extract completed fields from partial JSON
func (p *ObjectStreamingParser) extractFields() {
	current := p.accumulated

	// Try parsing as partial object
	if partial, err := ParsePartialJSON(current); err == nil {
		if obj, ok := partial.(map[string]interface{}); ok {
			// Update fields
			for k, v := range obj {
				p.fields[k] = v
			}
		}
	}
}

// GetFields returns the currently extracted fields
func (p *ObjectStreamingParser) GetFields() map[string]interface{} {
	return p.fields
}

// GetField returns a specific field value if available
func (p *ObjectStreamingParser) GetField(name string) (interface{}, bool) {
	val, ok := p.fields[name]
	return val, ok
}

// GetCurrent returns the current accumulated string
func (p *ObjectStreamingParser) GetCurrent() string {
	return p.accumulated
}

// Reset clears the parser state
func (p *ObjectStreamingParser) Reset() {
	p.buffer.Reset()
	p.accumulated = ""
	p.fields = make(map[string]interface{})
}

// ArrayStreamingParser handles streaming of arrays with progressive element extraction
type ArrayStreamingParser struct {
	buffer      strings.Builder
	accumulated string
	elements    []interface{}
}

// NewArrayStreamingParser creates a new array streaming parser
func NewArrayStreamingParser() *ArrayStreamingParser {
	return &ArrayStreamingParser{
		elements: make([]interface{}, 0),
	}
}

// Append adds more data to the parser
func (p *ArrayStreamingParser) Append(data string) {
	p.buffer.WriteString(data)
	p.accumulated = p.buffer.String()

	// Try to extract completed elements
	p.extractElements()
}

// extractElements attempts to extract completed array elements from partial JSON
func (p *ArrayStreamingParser) extractElements() {
	current := p.accumulated

	// Try parsing as partial array
	if partial, err := ParsePartialJSON(current); err == nil {
		if arr, ok := partial.([]interface{}); ok {
			// Only update if we have more elements
			if len(arr) > len(p.elements) {
				p.elements = arr
			}
		}
	}
}

// GetElements returns the currently extracted elements
func (p *ArrayStreamingParser) GetElements() []interface{} {
	return p.elements
}

// GetElementCount returns the number of extracted elements
func (p *ArrayStreamingParser) GetElementCount() int {
	return len(p.elements)
}

// GetCurrent returns the current accumulated string
func (p *ArrayStreamingParser) GetCurrent() string {
	return p.accumulated
}

// Reset clears the parser state
func (p *ArrayStreamingParser) Reset() {
	p.buffer.Reset()
	p.accumulated = ""
	p.elements = make([]interface{}, 0)
}
