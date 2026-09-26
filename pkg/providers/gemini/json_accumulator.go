package gemini

import (
	"encoding/json"
	"regexp"
	"strconv"
	"strings"
)

// PartialArg mirrors TS `PartialArg` (google-json-accumulator.ts): one
// streamed leaf value contributing to a function call's arguments, addressed
// by a `$.`-prefixed JSON path.
type PartialArg struct {
	JSONPath     string
	StringValue  *string
	NumberValue  *float64
	BoolValue    *bool
	HasNullValue bool
	WillContinue *bool
}

// UnmarshalJSON decodes a PartialArg, additionally tracking whether the
// `nullValue` key was present at all (TS: `'nullValue' in arg`), since a
// present-but-empty value there still means "set this leaf to null".
func (p *PartialArg) UnmarshalJSON(data []byte) error {
	var raw struct {
		JSONPath     string   `json:"jsonPath"`
		StringValue  *string  `json:"stringValue"`
		NumberValue  *float64 `json:"numberValue"`
		BoolValue    *bool    `json:"boolValue"`
		WillContinue *bool    `json:"willContinue"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}
	p.JSONPath = raw.JSONPath
	p.StringValue = raw.StringValue
	p.NumberValue = raw.NumberValue
	p.BoolValue = raw.BoolValue
	p.WillContinue = raw.WillContinue

	var presence map[string]json.RawMessage
	if err := json.Unmarshal(data, &presence); err == nil {
		if _, ok := presence["nullValue"]; ok {
			p.HasNullValue = true
		}
	}
	return nil
}

// stackEntry is one "open" container (object or array) in the accumulator's
// JSON output, mirroring TS `StackEntry`.
type stackEntry struct {
	segment    interface{} // string (object key) or int (array index)
	isArray    bool
	childCount int
}

// accumulatorResult is the return value of ProcessPartialArgs, mirroring TS
// `{ currentJSON, textDelta }`.
type accumulatorResult struct {
	CurrentJSON map[string]interface{}
	TextDelta   string
}

// GoogleJSONAccumulator ports TS `GoogleJSONAccumulator`
// (google-json-accumulator.ts): incrementally builds a JSON object from
// Gemini's streaming `partialArgs` chunks emitted during tool-call function
// calling. It tracks both the structured value and a running JSON text
// representation so callers can emit text deltas that, concatenated, form
// valid nested JSON identical to JSON.stringify(currentJSON) — which
// requires object keys to stay in insertion order (see orderedMap).
//
// Input: [{jsonPath:"$.location",stringValue:"Boston"}]
// Output: textDelta `{"location":"Boston"`, then Finalize() → closingDelta `}`
type GoogleJSONAccumulator struct {
	accumulatedArgs *orderedMap
	jsonText        string

	// pathStack represents the currently "open" containers in the JSON
	// output. Entry 0 is always the root `{` object once the first value is
	// written.
	pathStack []stackEntry

	// stringOpen is true when a string value is currently "open"
	// (willContinue was true), meaning the closing quote has not yet been
	// emitted.
	stringOpen bool
}

// NewGoogleJSONAccumulator returns a fresh accumulator.
func NewGoogleJSONAccumulator() *GoogleJSONAccumulator {
	return &GoogleJSONAccumulator{accumulatedArgs: newOrderedMap()}
}

// ProcessPartialArgs applies one chunk's partialArgs, returning the current
// full value and the JSON text fragment newly appended.
//
// Input: [{jsonPath:"$.brightness",numberValue:50}]
// Output: { CurrentJSON:{brightness:50}, TextDelta:`{"brightness":50` }
func (g *GoogleJSONAccumulator) ProcessPartialArgs(partialArgs []PartialArg) accumulatorResult {
	var delta strings.Builder

	for _, arg := range partialArgs {
		rawPath := strings.TrimPrefix(arg.JSONPath, "$.")
		if rawPath == "" {
			continue
		}

		segments := parseJSONPath(rawPath)

		existingValue, existingOK := getNestedValue(g.accumulatedArgs, segments)
		isStringContinuation := arg.StringValue != nil && existingOK

		if isStringContinuation {
			escaped := jsonStringBody(*arg.StringValue)
			existingStr, _ := existingValue.(string)
			setNestedValue(g.accumulatedArgs, segments, existingStr+*arg.StringValue)
			delta.WriteString(escaped)
			continue
		}

		value, valueJSON, ok := resolvePartialArgValue(arg)
		if !ok {
			continue
		}

		setNestedValue(g.accumulatedArgs, segments, value)
		delta.WriteString(g.emitNavigationTo(segments, arg, valueJSON))
	}

	textDelta := delta.String()
	g.jsonText += textDelta

	current, _ := toPlainJSON(g.accumulatedArgs).(map[string]interface{})
	if current == nil {
		current = map[string]interface{}{}
	}
	return accumulatorResult{CurrentJSON: current, TextDelta: textDelta}
}

// Finalize returns the complete JSON string plus the text fragment still
// needed to complete it, mirroring TS `finalize()`.
//
// Input: jsonText=`{"brightness":50`, accumulatedArgs={brightness:50}
// Output: { finalJSON:`{"brightness":50}`, closingDelta:`}` }
func (g *GoogleJSONAccumulator) Finalize() (finalJSON string, closingDelta string) {
	finalJSON = marshalOrdered(g.accumulatedArgs)
	if len(g.jsonText) > len(finalJSON) {
		return finalJSON, ""
	}
	return finalJSON, finalJSON[len(g.jsonText):]
}

// ensureRoot emits the opening `{` the first time any value is written.
func (g *GoogleJSONAccumulator) ensureRoot() string {
	if len(g.pathStack) == 0 {
		g.pathStack = append(g.pathStack, stackEntry{segment: ""})
		return "{"
	}
	return ""
}

// emitNavigationTo emits the JSON text fragment needed to navigate from the
// current open path to the new leaf at targetSegments, then writes the value.
//
// Input: targetSegments=["recipe","name"], valueJSON=`"Lasagna"`
// Output: `{"recipe":{"name":"Lasagna"`
func (g *GoogleJSONAccumulator) emitNavigationTo(target []interface{}, arg PartialArg, valueJSON string) string {
	var fragment strings.Builder

	if g.stringOpen {
		fragment.WriteByte('"')
		g.stringOpen = false
	}

	fragment.WriteString(g.ensureRoot())

	targetContainer := target[:len(target)-1]
	leaf := target[len(target)-1]

	commonDepth := g.findCommonStackDepth(targetContainer)

	fragment.WriteString(g.closeDownTo(commonDepth))
	fragment.WriteString(g.openDownTo(targetContainer, leaf))
	fragment.WriteString(g.emitLeaf(leaf, arg, valueJSON))

	return fragment.String()
}

// findCommonStackDepth returns the stack depth to preserve when navigating to
// a new target container path. Always >= 1 (the root is never popped).
func (g *GoogleJSONAccumulator) findCommonStackDepth(targetContainer []interface{}) int {
	maxDepth := len(g.pathStack) - 1
	if len(targetContainer) < maxDepth {
		maxDepth = len(targetContainer)
	}
	common := 0
	for i := 0; i < maxDepth; i++ {
		if g.pathStack[i+1].segment == targetContainer[i] {
			common++
		} else {
			break
		}
	}
	return common + 1
}

// closeDownTo closes containers from the current stack depth back down to
// targetDepth.
func (g *GoogleJSONAccumulator) closeDownTo(targetDepth int) string {
	var fragment strings.Builder
	for len(g.pathStack) > targetDepth {
		n := len(g.pathStack) - 1
		entry := g.pathStack[n]
		g.pathStack = g.pathStack[:n]
		if entry.isArray {
			fragment.WriteByte(']')
		} else {
			fragment.WriteByte('}')
		}
	}
	return fragment.String()
}

// openDownTo opens containers from the current stack depth down to the full
// target container path, emitting opening `{`, `[`, keys, and commas as
// needed. leafSegment determines whether the innermost container is an array.
func (g *GoogleJSONAccumulator) openDownTo(targetContainer []interface{}, leafSegment interface{}) string {
	var fragment strings.Builder

	startIdx := len(g.pathStack) - 1

	for i := startIdx; i < len(targetContainer); i++ {
		pathSegment := targetContainer[i]
		parent := &g.pathStack[len(g.pathStack)-1]

		if parent.childCount > 0 {
			fragment.WriteByte(',')
		}
		parent.childCount++

		if key, ok := pathSegment.(string); ok {
			fragment.WriteString(marshalJSONCompact(key))
			fragment.WriteByte(':')
		}

		var childSeg interface{}
		if i+1 < len(targetContainer) {
			childSeg = targetContainer[i+1]
		} else {
			childSeg = leafSegment
		}
		_, isArray := childSeg.(int)

		if isArray {
			fragment.WriteByte('[')
		} else {
			fragment.WriteByte('{')
		}

		g.pathStack = append(g.pathStack, stackEntry{segment: pathSegment, isArray: isArray})
	}

	return fragment.String()
}

// emitLeaf emits the comma, key, and value for a leaf entry in the current
// container.
func (g *GoogleJSONAccumulator) emitLeaf(leafSegment interface{}, arg PartialArg, valueJSON string) string {
	var fragment strings.Builder
	container := &g.pathStack[len(g.pathStack)-1]

	if container.childCount > 0 {
		fragment.WriteByte(',')
	}
	container.childCount++

	if key, ok := leafSegment.(string); ok {
		fragment.WriteString(marshalJSONCompact(key))
		fragment.WriteByte(':')
	}

	if arg.StringValue != nil && arg.WillContinue != nil && *arg.WillContinue {
		fragment.WriteString(valueJSON[:len(valueJSON)-1])
		g.stringOpen = true
	} else {
		fragment.WriteString(valueJSON)
	}

	return fragment.String()
}

// bracketIndexRe matches one `[N]` array-index segment in a JSON path.
var bracketIndexRe = regexp.MustCompile(`\[(\d+)\]`)

// parseJSONPath splits a dotted/bracketed JSON path like
// `recipe.ingredients[0].name` into segments: ["recipe","ingredients",0,"name"].
func parseJSONPath(rawPath string) []interface{} {
	var segments []interface{}
	for _, part := range strings.Split(rawPath, ".") {
		bracketIdx := strings.Index(part, "[")
		if bracketIdx == -1 {
			segments = append(segments, part)
			continue
		}
		if bracketIdx > 0 {
			segments = append(segments, part[:bracketIdx])
		}
		for _, m := range bracketIndexRe.FindAllStringSubmatch(part[bracketIdx:], -1) {
			n, _ := strconv.Atoi(m[1])
			segments = append(segments, n)
		}
	}
	return segments
}

// getNestedValue traverses root along segments and returns the leaf value.
func getNestedValue(root *orderedMap, segments []interface{}) (interface{}, bool) {
	var current interface{} = root
	for _, seg := range segments {
		switch s := seg.(type) {
		case string:
			m, ok := current.(*orderedMap)
			if !ok {
				return nil, false
			}
			v, ok := m.get(s)
			if !ok {
				return nil, false
			}
			current = v
		case int:
			arr, ok := current.([]interface{})
			if !ok {
				return nil, false
			}
			if s < 0 || s >= len(arr) {
				return nil, false
			}
			current = arr[s]
		default:
			return nil, false
		}
	}
	return current, true
}

// setNestedValue sets a value at a nested path inside root, creating
// intermediate ordered objects/arrays as needed.
//
// Input: root={}, segments=["recipe","ingredients",0,"name"], value="Noodles"
// Output: root={recipe:{ingredients:[{name:"Noodles"}]}}
func setNestedValue(root *orderedMap, segments []interface{}, value interface{}) {
	if len(segments) == 0 {
		return
	}
	// The root is always a JSON object, so the first segment is always a
	// string key (a top-level array root never occurs for tool-call args).
	key, ok := segments[0].(string)
	if !ok {
		return
	}
	if len(segments) == 1 {
		root.set(key, value)
		return
	}
	existing, _ := root.get(key)
	child := ensureContainer(existing, segments[1])
	root.set(key, setContainerValue(child, segments[1:], value))
}

// ensureContainer returns existing when it is already set, otherwise creates
// a new ordered object or array depending on the next path segment's kind.
func ensureContainer(existing interface{}, nextSeg interface{}) interface{} {
	if existing != nil {
		return existing
	}
	if _, isIndex := nextSeg.(int); isIndex {
		return []interface{}{}
	}
	return newOrderedMap()
}

// setContainerValue sets value at segments within container (an *orderedMap
// or []interface{}), returning the container — a new slice header when an
// array had to grow.
func setContainerValue(container interface{}, segments []interface{}, value interface{}) interface{} {
	seg := segments[0]
	if len(segments) == 1 {
		switch key := seg.(type) {
		case string:
			container.(*orderedMap).set(key, value)
			return container
		case int:
			arr := growSlice(container.([]interface{}), key)
			arr[key] = value
			return arr
		}
		return container
	}
	switch key := seg.(type) {
	case string:
		m := container.(*orderedMap)
		existing, _ := m.get(key)
		child := ensureContainer(existing, segments[1])
		m.set(key, setContainerValue(child, segments[1:], value))
		return m
	case int:
		arr := growSlice(container.([]interface{}), key)
		child := ensureContainer(arr[key], segments[1])
		arr[key] = setContainerValue(child, segments[1:], value)
		return arr
	}
	return container
}

// growSlice extends arr with nils until index idx is addressable.
func growSlice(arr []interface{}, idx int) []interface{} {
	for len(arr) <= idx {
		arr = append(arr, nil)
	}
	return arr
}

// jsonStringBody returns the JSON-escaped body of s without the surrounding
// quotes, mirroring TS `JSON.stringify(s).slice(1, -1)`.
func jsonStringBody(s string) string {
	quoted := marshalJSONCompact(s)
	if len(quoted) < 2 {
		return quoted
	}
	return quoted[1 : len(quoted)-1]
}

// resolvePartialArgValue extracts the first non-nil typed value from a
// partial arg and returns it with its JSON representation, mirroring TS
// `resolvePartialArgValue`'s `stringValue ?? numberValue ?? boolValue` chain
// (checked in that precedence order regardless of falsy-but-present values
// like "", 0, or false) and its `'nullValue' in arg` fallback.
func resolvePartialArgValue(arg PartialArg) (value interface{}, valueJSON string, ok bool) {
	switch {
	case arg.StringValue != nil:
		return *arg.StringValue, marshalJSONCompact(*arg.StringValue), true
	case arg.NumberValue != nil:
		return *arg.NumberValue, marshalJSONCompact(*arg.NumberValue), true
	case arg.BoolValue != nil:
		return *arg.BoolValue, marshalJSONCompact(*arg.BoolValue), true
	case arg.HasNullValue:
		return nil, "null", true
	default:
		return nil, "", false
	}
}
