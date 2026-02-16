package jsonutil

import (
	"testing"
)

func TestStreamingParser_Basic(t *testing.T) {
	t.Parallel()

	p := NewStreamingParser()

	// Append partial JSON
	p.Append(`{"name": "J`)
	_, ok := p.TryParse()
	if !ok {
		// Might not parse yet, that's okay
		t.Log("partial JSON not parseable yet")
	}

	// Complete the JSON
	p.Append(`ohn", "age": 30}`)
	result, ok := p.TryParse()

	if !ok {
		t.Error("expected successful parse")
	}
	if result == nil {
		t.Error("expected non-nil result")
	}
}

func TestStreamingParser_Empty(t *testing.T) {
	t.Parallel()

	p := NewStreamingParser()
	result, ok := p.TryParse()

	if ok {
		t.Error("expected parse to fail on empty input")
	}
	if result != nil {
		t.Error("expected nil result")
	}
}

func TestStreamingParser_GetCurrent(t *testing.T) {
	t.Parallel()

	p := NewStreamingParser()
	p.Append(`{"test": `)
	p.Append(`"value"}`)

	current := p.GetCurrent()
	if current != `{"test": "value"}` {
		t.Errorf("expected accumulated string, got %s", current)
	}
}

func TestStreamingParser_GetLastValid(t *testing.T) {
	t.Parallel()

	p := NewStreamingParser()
	p.Append(`{"name": "John"}`)
	p.TryParse()

	lastValid := p.GetLastValid()
	if lastValid == nil {
		t.Error("expected last valid to be set")
	}
}

func TestStreamingParser_Reset(t *testing.T) {
	t.Parallel()

	p := NewStreamingParser()
	p.Append(`{"test": "value"}`)
	p.TryParse()

	p.Reset()

	if p.GetCurrent() != "" {
		t.Error("expected empty current after reset")
	}
	if p.GetLastValid() != nil {
		t.Error("expected nil last valid after reset")
	}
}

func TestObjectStreamingParser_Basic(t *testing.T) {
	t.Parallel()

	p := NewObjectStreamingParser()

	// Add partial object
	p.Append(`{"name": "Jo`)
	fields := p.GetFields()
	// Might not have extracted yet
	t.Logf("fields after partial: %v", fields)

	// Complete
	p.Append(`hn", "age": 30}`)
	fields = p.GetFields()

	if len(fields) == 0 {
		t.Error("expected some fields to be extracted")
	}
}

func TestObjectStreamingParser_GetField(t *testing.T) {
	t.Parallel()

	p := NewObjectStreamingParser()
	p.Append(`{"name": "John", "age": 30}`)

	name, ok := p.GetField("name")
	if !ok {
		t.Error("expected to find 'name' field")
	}
	if name != "John" {
		t.Errorf("expected 'John', got %v", name)
	}

	_, ok = p.GetField("nonexistent")
	if ok {
		t.Error("expected nonexistent field to return false")
	}
}

func TestObjectStreamingParser_GetCurrent(t *testing.T) {
	t.Parallel()

	p := NewObjectStreamingParser()
	p.Append(`{"key": "value"}`)

	if p.GetCurrent() != `{"key": "value"}` {
		t.Errorf("unexpected current: %s", p.GetCurrent())
	}
}

func TestObjectStreamingParser_Reset(t *testing.T) {
	t.Parallel()

	p := NewObjectStreamingParser()
	p.Append(`{"test": "value"}`)

	p.Reset()

	if p.GetCurrent() != "" {
		t.Error("expected empty current after reset")
	}
	if len(p.GetFields()) != 0 {
		t.Error("expected empty fields after reset")
	}
}

func TestArrayStreamingParser_Basic(t *testing.T) {
	t.Parallel()

	p := NewArrayStreamingParser()

	// Add partial array
	p.Append(`["a", "b`)
	elements := p.GetElements()
	// May or may not have parsed yet
	t.Logf("elements after partial: %v", elements)

	// Complete
	p.Append(`", "c"]`)
	elements = p.GetElements()

	if len(elements) < 1 {
		t.Error("expected some elements to be extracted")
	}
}

func TestArrayStreamingParser_GetElementCount(t *testing.T) {
	t.Parallel()

	p := NewArrayStreamingParser()
	p.Append(`[1, 2, 3, 4, 5]`)

	count := p.GetElementCount()
	if count != 5 {
		t.Errorf("expected 5 elements, got %d", count)
	}
}

func TestArrayStreamingParser_GetCurrent(t *testing.T) {
	t.Parallel()

	p := NewArrayStreamingParser()
	p.Append(`[1, 2, 3]`)

	if p.GetCurrent() != `[1, 2, 3]` {
		t.Errorf("unexpected current: %s", p.GetCurrent())
	}
}

func TestArrayStreamingParser_Reset(t *testing.T) {
	t.Parallel()

	p := NewArrayStreamingParser()
	p.Append(`[1, 2, 3]`)

	p.Reset()

	if p.GetCurrent() != "" {
		t.Error("expected empty current after reset")
	}
	if len(p.GetElements()) != 0 {
		t.Error("expected empty elements after reset")
	}
}

func TestStreamingParser_PartialToComplete(t *testing.T) {
	t.Parallel()

	p := NewStreamingParser()

	// Feed data in small chunks
	chunks := []string{
		`{`,
		`"name": `,
		`"John"`,
		`, "items": [`,
		`1, 2, 3`,
		`]`,
		`}`,
	}

	for _, chunk := range chunks {
		p.Append(chunk)
		result, ok := p.TryParse()
		t.Logf("After %q: ok=%v, result=%v", chunk, ok, result)
	}

	// Final result should be complete
	result, ok := p.TryParse()
	if !ok {
		t.Error("expected successful parse after all chunks")
	}
	if result == nil {
		t.Error("expected non-nil result")
	}
}

func TestObjectStreamingParser_NestedObject(t *testing.T) {
	t.Parallel()

	p := NewObjectStreamingParser()
	p.Append(`{"user": {"name": "John", "email": "john@example.com"}}`)

	user, ok := p.GetField("user")
	if !ok {
		t.Error("expected to find 'user' field")
	}
	if user == nil {
		t.Error("expected non-nil user")
	}
}

func TestArrayStreamingParser_NestedArrays(t *testing.T) {
	t.Parallel()

	p := NewArrayStreamingParser()
	p.Append(`[[1, 2], [3, 4], [5, 6]]`)

	elements := p.GetElements()
	if len(elements) != 3 {
		t.Errorf("expected 3 elements, got %d", len(elements))
	}
}

func TestStreamingParser_InvalidJSON(t *testing.T) {
	t.Parallel()

	p := NewStreamingParser()
	p.Append(`not valid json at all`)

	_, ok := p.TryParse()
	if ok {
		t.Error("expected parse to fail for invalid JSON")
	}
}

func TestObjectStreamingParser_InvalidJSON(t *testing.T) {
	t.Parallel()

	p := NewObjectStreamingParser()
	p.Append(`not an object`)

	fields := p.GetFields()
	if len(fields) != 0 {
		t.Error("expected empty fields for invalid JSON")
	}
}

func TestArrayStreamingParser_InvalidJSON(t *testing.T) {
	t.Parallel()

	p := NewArrayStreamingParser()
	p.Append(`not an array`)

	elements := p.GetElements()
	if len(elements) != 0 {
		t.Error("expected empty elements for invalid JSON")
	}
}
