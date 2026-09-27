package codemode

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
)

// jsonSchemaMap is a JSON Schema document represented as a generic Go map,
// matching how TypeScript's tool-prompt.ts treats `Record<string, unknown>`.
type jsonSchemaMap = map[string]interface{}

// schemaContext threads the schema root (for $ref resolution), a
// depth counter, and a seen-refs set through recursive schema walks.
// Mirrors TypeScript's SchemaContext.
type schemaContext struct {
	root     jsonSchemaMap
	seenRefs map[string]bool
	depth    int
}

const (
	maxSchemaDepth             = 8
	maxCompactObjectTypeLength = 120
)

// BuildCodeModeToolDescription builds the tool description exposed to the
// model for the code-mode tool, either embedding TypeScript signatures for
// tools (discovery == ToolDiscoveryDescription, the default) or referring
// the model to conversation-delivered capability updates (discovery ==
// ToolDiscoveryConversation). Mirrors TypeScript's
// buildCodeModeToolDescription (code-mode/src/tool-prompt.ts).
func BuildCodeModeToolDescription(tools ToolSet, discovery ToolDiscovery) string {
	fetchLine := "Use exact names/types below. `JSON.parse`/`JSON.stringify` are available."
	if discovery == ToolDiscoveryConversation {
		fetchLine = "Use exact names/types from the latest capability update. `JSON.parse`/`JSON.stringify` are available."
	}

	sections := []string{
		"Execute code-mode TypeScript in an isolated sandbox.",
		"",
		"Put the full program in `js`; top-level `await`/`return` work. Return a JSON-serializable result.",
		"Call host tools only as async `tools.name(input)`; await each or use `Promise.all` for independent calls.",
		fetchLine,
		"Fetch: `fetch` is not available.",
	}

	if discovery == ToolDiscoveryConversation {
		sections = append(sections,
			"",
			"Tools:",
			`The current host-tool API is provided in "Code mode capability update" user messages. Follow the latest catalog and ignore earlier catalogs.`,
		)
		return strings.Join(sections, "\n")
	}

	typeBlock, exampleBlock := renderToolCatalog(tools)
	sections = append(sections, "", "Tools:", typeBlock)
	if exampleBlock != "" {
		sections = append(sections, exampleBlock)
	}
	return strings.Join(sections, "\n")
}

// BuildCodeModeToolCatalogMessage builds a "capability update" message
// announcing the current host-tool catalog, for ToolDiscoveryConversation.
// Mirrors TypeScript's buildCodeModeToolCatalogMessage.
func BuildCodeModeToolCatalogMessage(tools ToolSet) string {
	typeBlock, exampleBlock := renderToolCatalog(tools)
	sections := []string{
		"Code mode capability update.",
		"",
		"This catalog replaces all previous code mode capability catalogs. Only the tools listed below are currently available through `tools`.",
		"",
		"Tools:",
		typeBlock,
	}
	if exampleBlock != "" {
		sections = append(sections, exampleBlock)
	}
	return strings.Join(sections, "\n")
}

// sortedToolNames returns tool names in deterministic order. Go maps have
// no defined iteration order (TypeScript's ToolSet is a plain object, whose
// keys iterate in declaration/insertion order); this port sorts
// alphabetically instead so output is reproducible. This is a deliberate,
// documented deviation from TypeScript's insertion-order behavior.
func sortedToolNames(tools ToolSet) []string {
	names := make([]string, 0, len(tools))
	for name := range tools {
		names = append(names, name)
	}
	sort.Strings(names)
	return names
}

func renderToolCatalog(tools ToolSet) (typeBlock, exampleBlock string) {
	names := sortedToolNames(tools)

	if len(names) == 0 {
		return "No host tools. Do not call `tools.*`.", ""
	}

	lines := []string{"```ts", "declare const tools: {"}
	for _, name := range names {
		lines = append(lines, renderToolType(name, tools[name])...)
	}
	lines = append(lines, "};", "```")
	typeBlock = strings.Join(lines, "\n")

	exampleLines := []string{"", "Tool call examples:", "```ts"}
	exampleLines = append(exampleLines, renderToolExamples(names, tools)...)
	exampleLines = append(exampleLines, "```")
	exampleBlock = strings.Join(exampleLines, "\n")

	return typeBlock, exampleBlock
}

func renderToolType(name string, tool types.Tool) []string {
	inputSchema, hasInput := resolveInputSchema(tool)
	inputType := "unknown"
	if hasInput {
		inputType = schemaToType(inputSchema, inputSchema)
	}
	outputSchema, hasOutput := resolveOutputSchema(tool)
	outputType := "unknown"
	if hasOutput {
		outputType = schemaToType(outputSchema, outputSchema)
	}

	description := firstNonEmpty(tool.Description, tool.Title)

	var lines []string
	if description != "" {
		lines = append(lines, fmt.Sprintf("  /** %s */", toComment(description)))
	}
	lines = append(lines, fmt.Sprintf(
		"  %s: (input: %s) => Promise<%s>;",
		formatObjectKey(name),
		indentType(inputType, 2),
		indentType(outputType, 2),
	))
	return lines
}

func renderToolExamples(names []string, tools ToolSet) []string {
	if len(names) == 1 {
		name := names[0]
		return []string{
			fmt.Sprintf("const result = await %s;", renderToolExampleCall(name, tools[name])),
			fmt.Sprintf("return %s;", renderToolProjection("result", tools[name])),
		}
	}

	variableNames := uniqueToolVariableNames(names)
	lines := []string{fmt.Sprintf("const [%s] = await Promise.all([", strings.Join(variableNames, ", "))}
	for _, name := range names {
		lines = append(lines, fmt.Sprintf("  %s,", renderToolExampleCall(name, tools[name])))
	}
	lines = append(lines, "]);", "return {")
	for i, name := range names {
		lines = append(lines, fmt.Sprintf("  %s: %s,", formatObjectKey(name), renderToolProjection(variableNames[i], tools[name])))
	}
	lines = append(lines, "};")
	return lines
}

func renderToolExampleCall(name string, tool types.Tool) string {
	input := firstInputExample(tool)
	if input == nil {
		if schema, ok := resolveInputSchema(tool); ok {
			input = sampleFromSchema(schema, schema)
		} else {
			input = map[string]interface{}{}
		}
	}
	encoded, err := json.Marshal(input)
	if err != nil {
		encoded = []byte("{}")
	}
	return fmt.Sprintf("tools%s(%s)", formatPropertyAccess(name), string(encoded))
}

func renderToolProjection(variableName string, tool types.Tool) string {
	outputSchema, ok := resolveOutputSchema(tool)
	if !ok {
		return variableName
	}
	field := firstObjectProperty(outputSchema)
	if field == "" {
		return variableName
	}
	return fmt.Sprintf("{ %s: %s%s }", formatObjectKey(field), variableName, formatPropertyAccess(field))
}

func firstObjectProperty(s jsonSchemaMap) string {
	props, ok := s["properties"].(jsonSchemaMap)
	if !ok {
		return ""
	}
	names := make([]string, 0, len(props))
	for name, v := range props {
		if _, ok := v.(jsonSchemaMap); ok {
			names = append(names, name)
		}
	}
	if len(names) == 0 {
		return ""
	}
	sort.Strings(names)
	return names[0]
}

func resolveInputSchema(tool types.Tool) (jsonSchemaMap, bool) {
	return toJSONSchemaMap(tool.Parameters)
}

func resolveOutputSchema(tool types.Tool) (jsonSchemaMap, bool) {
	return toJSONSchemaMap(tool.OutputSchema)
}

func toJSONSchemaMap(v interface{}) (jsonSchemaMap, bool) {
	switch s := v.(type) {
	case jsonSchemaMap:
		if len(s) == 0 {
			return nil, false
		}
		return s, true
	case schema.Schema:
		if s == nil {
			return nil, false
		}
		js := s.Validator().JSONSchema()
		if len(js) == 0 {
			return nil, false
		}
		return js, true
	default:
		return nil, false
	}
}

func schemaToType(s jsonSchemaMap, root jsonSchemaMap) string {
	return schemaToTypeInner(s, schemaContext{root: root, seenRefs: map[string]bool{}, depth: 0})
}

func schemaToTypeInner(s jsonSchemaMap, ctx schemaContext) string {
	if ctx.depth > maxSchemaDepth {
		return "unknown"
	}

	if ref, ok := stringField(s, "$ref"); ok {
		resolved := resolveRef(ref, ctx.root, ctx.seenRefs)
		if resolved == nil {
			return "unknown"
		}
		return schemaToTypeInner(resolved, nextContext(ctx))
	}

	if constVal, ok := s["const"]; ok && constVal != nil {
		return literalType(constVal)
	}

	if enumVals, ok := s["enum"].([]interface{}); ok {
		types := make([]string, len(enumVals))
		for i, v := range enumVals {
			types[i] = literalType(v)
		}
		return union(types)
	}

	if oneOf, ok := schemaArray(s["oneOf"]); ok {
		return union(mapSchemaToType(oneOf, ctx))
	}
	if anyOf, ok := schemaArray(s["anyOf"]); ok {
		return union(mapSchemaToType(anyOf, ctx))
	}
	if allOf, ok := schemaArray(s["allOf"]); ok {
		parts := mapSchemaToType(allOf, ctx)
		return strings.Join(parts, " & ")
	}

	switch t := s["type"].(type) {
	case []interface{}:
		parts := make([]string, len(t))
		for i, part := range t {
			variant := jsonSchemaMap{}
			for k, v := range s {
				variant[k] = v
			}
			variant["type"] = part
			parts[i] = schemaToTypeInner(variant, ctx)
		}
		return union(parts)
	case string:
		switch t {
		case "object":
			return objectType(s, ctx)
		case "array":
			return arrayType(s, ctx)
		case "string":
			return "string"
		case "number", "integer":
			return "number"
		case "boolean":
			return "boolean"
		case "null":
			return "null"
		}
	}

	if _, ok := s["properties"].(jsonSchemaMap); ok {
		return objectType(s, ctx)
	}
	if _, hasItems := s["items"]; hasItems {
		return arrayType(s, ctx)
	}

	return "unknown"
}

func mapSchemaToType(schemas []jsonSchemaMap, ctx schemaContext) []string {
	out := make([]string, len(schemas))
	for i, s := range schemas {
		out[i] = schemaToTypeInner(s, nextContext(ctx))
	}
	return out
}

func objectType(s jsonSchemaMap, ctx schemaContext) string {
	props, _ := s["properties"].(jsonSchemaMap)
	required := map[string]bool{}
	if req, ok := s["required"].([]interface{}); ok {
		for _, v := range req {
			if str, ok := v.(string); ok {
				required[str] = true
			}
		}
	}

	names := make([]string, 0, len(props))
	for name, v := range props {
		if _, ok := v.(jsonSchemaMap); ok {
			names = append(names, name)
		}
	}
	sort.Strings(names)

	if len(names) == 0 {
		switch additional := s["additionalProperties"].(type) {
		case jsonSchemaMap:
			return fmt.Sprintf("Record<string, %s>", schemaToTypeInner(additional, nextContext(ctx)))
		case bool:
			if additional {
				return "Record<string, unknown>"
			}
		}
		return "{}"
	}

	var lines []string
	for _, name := range names {
		value := props[name].(jsonSchemaMap)
		if desc, ok := stringField(value, "description"); ok {
			lines = append(lines, fmt.Sprintf("  /** %s */", toComment(desc)))
		}
		optional := ""
		if !required[name] {
			optional = "?"
		}
		lines = append(lines, fmt.Sprintf("  %s%s: %s;", formatObjectKey(name), optional, indentType(schemaToTypeInner(value, nextContext(ctx)), 2)))
	}

	switch additional := s["additionalProperties"].(type) {
	case jsonSchemaMap:
		lines = append(lines, fmt.Sprintf("  [key: string]: %s;", schemaToTypeInner(additional, nextContext(ctx))))
	case bool:
		if additional {
			lines = append(lines, "  [key: string]: unknown;")
		}
	}

	if compact, ok := compactObjectType(lines); ok {
		return compact
	}
	return "{\n" + strings.Join(lines, "\n") + "\n}"
}

func arrayType(s jsonSchemaMap, ctx schemaContext) string {
	switch items := s["items"].(type) {
	case []interface{}:
		parts := make([]string, len(items))
		for i, item := range items {
			if m, ok := item.(jsonSchemaMap); ok {
				parts[i] = schemaToTypeInner(m, nextContext(ctx))
			} else {
				parts[i] = "unknown"
			}
		}
		return "[" + strings.Join(parts, ", ") + "]"
	case jsonSchemaMap:
		itemType := schemaToTypeInner(items, nextContext(ctx))
		if needsParentheses(itemType) || strings.Contains(itemType, "\n") {
			return fmt.Sprintf("Array<%s>", itemType)
		}
		return itemType + "[]"
	default:
		return "unknown[]"
	}
}

func sampleFromSchema(s jsonSchemaMap, root jsonSchemaMap) interface{} {
	return sampleFromSchemaInner(s, schemaContext{root: root, seenRefs: map[string]bool{}, depth: 0})
}

func sampleFromSchemaInner(s jsonSchemaMap, ctx schemaContext) interface{} {
	if ctx.depth > maxSchemaDepth {
		return nil
	}
	if def, ok := s["default"]; ok && def != nil {
		return def
	}
	if ref, ok := stringField(s, "$ref"); ok {
		resolved := resolveRef(ref, ctx.root, ctx.seenRefs)
		if resolved == nil {
			return nil
		}
		return sampleFromSchemaInner(resolved, nextContext(ctx))
	}
	if constVal, ok := s["const"]; ok {
		return constVal
	}
	if enumVals, ok := s["enum"].([]interface{}); ok {
		if len(enumVals) > 0 {
			return enumVals[0]
		}
		return nil
	}
	if oneOf, ok := schemaArray(s["oneOf"]); ok {
		if len(oneOf) > 0 {
			return sampleFromSchemaInner(oneOf[0], nextContext(ctx))
		}
		return sampleFromSchemaInner(jsonSchemaMap{}, nextContext(ctx))
	}
	if anyOf, ok := schemaArray(s["anyOf"]); ok {
		if len(anyOf) > 0 {
			return sampleFromSchemaInner(anyOf[0], nextContext(ctx))
		}
		return sampleFromSchemaInner(jsonSchemaMap{}, nextContext(ctx))
	}

	typeVal := s["type"]
	if arr, ok := typeVal.([]interface{}); ok {
		chosen := interface{}(nil)
		if len(arr) > 0 {
			chosen = arr[0]
		}
		for _, part := range arr {
			if str, ok := part.(string); ok && str != "null" {
				chosen = part
				break
			}
		}
		variant := jsonSchemaMap{}
		for k, v := range s {
			variant[k] = v
		}
		variant["type"] = chosen
		return sampleFromSchemaInner(variant, ctx)
	}

	typeStr, _ := typeVal.(string)
	_, hasProps := s["properties"].(jsonSchemaMap)
	if typeStr == "object" || hasProps {
		props, _ := s["properties"].(jsonSchemaMap)
		out := map[string]interface{}{}
		for name, v := range props {
			if m, ok := v.(jsonSchemaMap); ok {
				out[name] = sampleFromSchemaInner(m, nextContext(ctx))
			}
		}
		return out
	}

	_, hasItems := s["items"]
	if typeStr == "array" || hasItems {
		var itemSchema jsonSchemaMap
		switch items := s["items"].(type) {
		case []interface{}:
			if len(items) > 0 {
				itemSchema, _ = items[0].(jsonSchemaMap)
			}
		case jsonSchemaMap:
			itemSchema = items
		}
		if itemSchema != nil {
			return []interface{}{sampleFromSchemaInner(itemSchema, nextContext(ctx))}
		}
		return []interface{}{nil}
	}

	switch typeStr {
	case "number", "integer":
		return 1
	case "boolean":
		return true
	case "null":
		return nil
	case "string", "":
		if examples, ok := s["examples"].([]interface{}); ok && len(examples) > 0 {
			return examples[0]
		}
		format, _ := stringField(s, "format")
		switch format {
		case "uri", "url":
			return "https://example.com"
		case "date-time":
			return "2026-01-01T00:00:00.000Z"
		case "date":
			return "2026-01-01"
		}
		return "string"
	}
	return nil
}

func firstInputExample(tool types.Tool) interface{} {
	if len(tool.InputExamples) == 0 {
		return nil
	}
	return interfaceMap(tool.InputExamples[0].Input)
}

func interfaceMap(m map[string]interface{}) interface{} {
	if m == nil {
		return nil
	}
	return m
}

func resolveRef(ref string, root jsonSchemaMap, seenRefs map[string]bool) jsonSchemaMap {
	if !strings.HasPrefix(ref, "#/") || seenRefs[ref] {
		return nil
	}
	seenRefs[ref] = true

	parts := strings.Split(ref[2:], "/")
	var current interface{} = root
	for _, part := range parts {
		part = strings.ReplaceAll(part, "~1", "/")
		part = strings.ReplaceAll(part, "~0", "~")
		m, ok := current.(jsonSchemaMap)
		if !ok {
			return nil
		}
		current = m[part]
	}
	if m, ok := current.(jsonSchemaMap); ok {
		return m
	}
	return nil
}

func nextContext(ctx schemaContext) schemaContext {
	return schemaContext{root: ctx.root, seenRefs: ctx.seenRefs, depth: ctx.depth + 1}
}

func union(parts []string) string {
	seen := map[string]bool{}
	var unique []string
	for _, p := range parts {
		if !seen[p] {
			seen[p] = true
			unique = append(unique, p)
		}
	}
	if len(unique) == 0 {
		return "unknown"
	}
	return strings.Join(unique, " | ")
}

func literalType(value interface{}) string {
	switch v := value.(type) {
	case string:
		encoded, _ := json.Marshal(v)
		return string(encoded)
	case float64:
		return formatNumber(v)
	case bool:
		if v {
			return "true"
		}
		return "false"
	case nil:
		return "null"
	default:
		return "unknown"
	}
}

func formatNumber(v float64) string {
	if v == float64(int64(v)) {
		return fmt.Sprintf("%d", int64(v))
	}
	return fmt.Sprintf("%g", v)
}

func indentType(t string, spaces int) string {
	if !strings.Contains(t, "\n") {
		return t
	}
	padding := strings.Repeat(" ", spaces)
	return strings.ReplaceAll(t, "\n", "\n"+padding)
}

func needsParentheses(t string) bool {
	return strings.Contains(t, " | ") || strings.Contains(t, " & ")
}

var identifierPattern = regexp.MustCompile(`^[A-Za-z_$][A-Za-z0-9_$]*$`)

func isIdentifier(s string) bool {
	return identifierPattern.MatchString(s)
}

func formatObjectKey(key string) string {
	if isIdentifier(key) {
		return key
	}
	encoded, _ := json.Marshal(key)
	return string(encoded)
}

func formatPropertyAccess(key string) string {
	if isIdentifier(key) {
		return "." + key
	}
	encoded, _ := json.Marshal(key)
	return "[" + string(encoded) + "]"
}

func uniqueToolVariableNames(names []string) []string {
	seen := map[string]int{}
	out := make([]string, len(names))
	for i, name := range names {
		base := toIdentifier(name)
		count := seen[base]
		seen[base] = count + 1
		if count == 0 {
			out[i] = base
		} else {
			out[i] = fmt.Sprintf("%s%d", base, count+1)
		}
	}
	return out
}

var (
	nonIdentifierChars   = regexp.MustCompile(`[^A-Za-z0-9_$]`)
	leadingNonIdentStart = regexp.MustCompile(`^[^A-Za-z_$]+`)
)

func toIdentifier(value string) string {
	identifier := nonIdentifierChars.ReplaceAllString(value, "_")
	identifier = leadingNonIdentStart.ReplaceAllString(identifier, "")
	if identifier == "" {
		return "tool"
	}
	return identifier
}

func compactObjectType(lines []string) (string, bool) {
	if len(lines) == 0 {
		return "", false
	}
	for _, line := range lines {
		if strings.Contains(line, "/**") {
			return "", false
		}
	}
	fields := make([]string, len(lines))
	for i, line := range lines {
		fields[i] = strings.TrimSpace(line)
		if strings.Contains(fields[i], "\n") {
			return "", false
		}
	}
	compact := "{ " + strings.Join(fields, " ") + " }"
	if len(compact) <= maxCompactObjectTypeLength {
		return compact, true
	}
	return "", false
}

func toComment(value string) string {
	value = strings.ReplaceAll(value, "*/", "* /")
	fields := strings.Fields(value)
	return strings.Join(fields, " ")
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func stringField(s jsonSchemaMap, key string) (string, bool) {
	v, ok := s[key]
	if !ok {
		return "", false
	}
	str, ok := v.(string)
	return str, ok
}

func schemaArray(v interface{}) ([]jsonSchemaMap, bool) {
	arr, ok := v.([]interface{})
	if !ok {
		return nil, false
	}
	out := make([]jsonSchemaMap, 0, len(arr))
	for _, item := range arr {
		m, ok := item.(jsonSchemaMap)
		if !ok {
			return nil, false
		}
		out = append(out, m)
	}
	return out, true
}
