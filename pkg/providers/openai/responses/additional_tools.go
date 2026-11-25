package responses

import (
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf16"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
	"github.com/digitallysavvy/go-ai/pkg/providerutils/jsonstr"
	providertool "github.com/digitallysavvy/go-ai/pkg/providerutils/tool"
)

// additionalToolEntry is one validated providerOptions additionalTools entry.
// Conversation history is replayable and client-supplied, so an entry is only
// a reference to a tool registered for the request (TS 5aa836fe1d): the type
// and the name select the tool, and the nested names of a namespace entry
// select which of its registered tools to load. strict and defer_loading are
// kept only to warn when they differ from the registered tool.
type additionalToolEntry struct {
	Type               string
	Name               string
	Strict             *bool
	DeferLoading       *bool
	NamespaceToolNames []string
}

// AdditionalToolRef names the registered tool an additionalTools entry
// selects.
type AdditionalToolRef struct {
	Type string // "function", "custom" or "namespace"
	Name string
}

// AdditionalToolResolution is the result of resolving an AdditionalToolRef.
// Definition is an AdditionalToolDef, AdditionalCustomToolDef or
// AdditionalNamespaceToolDef, or nil when the entry must not be loaded (the
// warnings then say why).
type AdditionalToolResolution struct {
	Definition interface{}
	Warnings   []types.Warning
}

// FindAdditionalToolOptions configures FindAdditionalToolDefinition.
type FindAdditionalToolOptions struct {
	Tools               []types.Tool
	ProviderOptionsName string
	Ref                 AdditionalToolRef
	// AllowedToolNames holds the provider tool names the request's
	// allowedTools option permits; nil means there is no allowedTools option.
	AllowedToolNames map[string]bool
	// NormalizeSchema normalizes a tool's input / output schema the way the
	// request tools are normalized (TS normalizeOpenAIJsonSchema). The
	// responses package cannot import the openai package, so the model
	// supplies it; nil sends schemas as given.
	NormalizeSchema func(schema interface{}) (interface{}, []types.Warning, error)
}

func unsupportedAdditionalToolWarning(feature, details string) types.Warning {
	return types.Warning{Type: "unsupported", Feature: feature, Details: details}
}

// isAdditionalFunctionTool reports whether t is a plain function tool (TS
// tool.type === 'function'): not a provider-defined tool and not an OpenAI
// custom tool.
func isAdditionalFunctionTool(t types.Tool) bool {
	if t.Type == types.ToolTypeProviderDefined || isOpenAICustomTool(t) {
		return false
	}
	id := t.ProviderID
	if id == "" {
		id = t.Name
	}
	switch id {
	case "openai.local_shell", "openai.shell", "openai.apply_patch", "openai.computer",
		"openai.code_interpreter", "openai.file_search", "openai.image_generation",
		"openai.web_search", "openai.web_search_preview", "openai.mcp",
		"openai.tool_search", "openai.programmatic_tool_calling":
		return false
	}
	return true
}

// prepareAdditionalFunctionTool is TS prepareFunctionTool narrowed to the
// fields an additional tool definition carries (name, description,
// parameters, strict, defer_loading). Schema normalization warnings (input,
// then output schema) are appended to toolWarnings.
func prepareAdditionalFunctionTool(t types.Tool, normalize func(interface{}) (interface{}, []types.Warning, error), toolWarnings *[]types.Warning) (AdditionalToolDef, error) {
	parameters := defaultFunctionParameters(t.Parameters)
	if normalize != nil {
		normalized, warnings, err := normalize(parameters)
		if err != nil {
			return AdditionalToolDef{}, err
		}
		parameters = normalized
		*toolWarnings = append(*toolWarnings, warnings...)
	}
	if normalize != nil {
		if outputSchema, ok := functionToolOutputSchema(t.ProviderOptions); ok {
			_, warnings, err := normalize(outputSchema)
			if err != nil {
				return AdditionalToolDef{}, err
			}
			*toolWarnings = append(*toolWarnings, warnings...)
		}
	}
	strict := false
	if t.Strict != nil {
		strict = *t.Strict
	}
	def := AdditionalToolDef{Type: "function", Name: t.Name, Parameters: parameters, Strict: &strict}
	if t.Description != "" {
		description := t.Description
		def.Description = &description
	}
	if deferLoading, ok := functionToolDeferLoading(t.ProviderOptions); ok {
		def.DeferLoading = &deferLoading
	}
	return def, nil
}

// scopeOneTool resolves the schema of one tool and exposes its options under
// providerOptionsName as the "openai" entry, like PrepareToolsWithOptionsName
// does for every request tool. Only tools an entry references are prepared.
func scopeOneTool(t types.Tool, providerOptionsName string) types.Tool {
	return scopeFunctionToolOptions(providertool.ResolveToolSchemas([]types.Tool{t}), providerOptionsName)[0]
}

func hasAllowedCallers(t types.Tool) bool {
	options, ok := functionToolOpenAIOptions(t.ProviderOptions)
	return ok && options["allowedCallers"] != nil
}

func hasNamespaceOption(t types.Tool) bool {
	options, ok := functionToolOpenAIOptions(t.ProviderOptions)
	return ok && options["namespace"] != nil
}

// FindAdditionalToolDefinition resolves an additionalTools entry from the
// prompt to the definition of the tool registered for the request (TS
// findAdditionalToolDefinition). A nil result means no registered tool
// matches. A supplied entry only selects a registered tool: the definition is
// derived from that tool, never from the history text. Only the selected tool
// is prepared.
func FindAdditionalToolDefinition(o FindAdditionalToolOptions) (*AdditionalToolResolution, error) {
	providerOptionsName := o.ProviderOptionsName
	if providerOptionsName == "" {
		providerOptionsName = "openai"
	}
	var warnings []types.Warning

	if o.Ref.Type == "namespace" {
		var namespaceTools []AdditionalToolDef
		var description *string
		for _, registered := range o.Tools {
			if !isAdditionalFunctionTool(registered) {
				continue
			}
			t := scopeOneTool(registered, providerOptionsName)
			namespace, ok := functionToolNamespace(t.ProviderOptions)
			if !ok || namespace.Name != o.Ref.Name {
				continue
			}
			if description == nil {
				d := namespace.Description
				description = &d
			} else if *description != namespace.Description {
				warnings = append(warnings, unsupportedAdditionalToolWarning(
					fmt.Sprintf("conflicting descriptions for OpenAI tool namespace \"%s\"", namespace.Name),
					"the first registered description is used for the loaded namespace"))
			}
			// A definition loaded from history cannot express an
			// allowed-callers restriction, so leave such tools out.
			if hasAllowedCallers(t) {
				warnings = append(warnings, unsupportedAdditionalToolWarning(
					fmt.Sprintf("additionalTools namespace \"%s\"", o.Ref.Name),
					fmt.Sprintf("tool \"%s\" restricts allowed callers, which a definition loaded from history cannot express; it is not loaded", t.Name)))
				continue
			}
			// Tools in the request tools were already prepared with their
			// warnings; only report the warnings of hidden tools here.
			var discarded []types.Warning
			toolWarnings := &discarded
			if functionToolOmitFromRequestTools(t.ProviderOptions) {
				toolWarnings = &warnings
			}
			def, err := prepareAdditionalFunctionTool(t, o.NormalizeSchema, toolWarnings)
			if err != nil {
				return nil, err
			}
			namespaceTools = append(namespaceTools, def)
		}
		if description == nil {
			return nil, nil
		}
		if o.AllowedToolNames != nil {
			return &AdditionalToolResolution{Warnings: []types.Warning{unsupportedAdditionalToolWarning(
				fmt.Sprintf("additionalTools namespace \"%s\"", o.Ref.Name),
				"tool namespaces cannot be loaded while allowedTools restricts the callable tools; the entry is not loaded")}}, nil
		}
		if len(namespaceTools) == 0 {
			// Every member was filtered out; the warnings explain why.
			return &AdditionalToolResolution{Warnings: warnings}, nil
		}
		return &AdditionalToolResolution{
			Definition: AdditionalNamespaceToolDef{Type: "namespace", Name: o.Ref.Name, Description: *description, Tools: namespaceTools},
			Warnings:   warnings,
		}, nil
	}

	notAllowed := func(name string) *AdditionalToolResolution {
		return &AdditionalToolResolution{Warnings: []types.Warning{unsupportedAdditionalToolWarning(
			fmt.Sprintf("additionalTools entry \"%s\"", name),
			"the tool is not part of allowedTools for this request; the entry is not loaded")}}
	}
	for _, registered := range o.Tools {
		if o.Ref.Type == "function" && isAdditionalFunctionTool(registered) {
			t := scopeOneTool(registered, providerOptionsName)
			// Namespaced functions are only sent as part of their namespace.
			if t.Name != o.Ref.Name || hasNamespaceOption(t) {
				continue
			}
			if o.AllowedToolNames != nil && !o.AllowedToolNames[t.Name] {
				return notAllowed(t.Name), nil
			}
			if hasAllowedCallers(t) {
				return &AdditionalToolResolution{Warnings: []types.Warning{unsupportedAdditionalToolWarning(
					fmt.Sprintf("additionalTools entry \"%s\"", t.Name),
					"the registered tool restricts allowed callers, which a definition loaded from history cannot express; the entry is not loaded")}}, nil
			}
			omit := functionToolOmitFromRequestTools(t.ProviderOptions)
			var discarded []types.Warning
			toolWarnings := &discarded
			if omit {
				toolWarnings = &warnings
			}
			def, err := prepareAdditionalFunctionTool(t, o.NormalizeSchema, toolWarnings)
			if err != nil {
				return nil, err
			}
			return &AdditionalToolResolution{Definition: def, Warnings: warnings}, nil
		}
		if o.Ref.Type == "custom" && isOpenAICustomTool(registered) && registered.Name == o.Ref.Name {
			if o.AllowedToolNames != nil && !o.AllowedToolNames[registered.Name] {
				return notAllowed(registered.Name), nil
			}
			if err := validateOpenAIToolArgsSchema(registered); err != nil {
				return nil, err
			}
			custom := convertCustomTool(registered)
			return &AdditionalToolResolution{
				Definition: AdditionalCustomToolDef{Type: "custom", Name: registered.Name, Description: custom.Description, Format: custom.Format},
				Warnings:   warnings,
			}, nil
		}
	}
	return nil, nil
}

// parseAdditionalToolEntries validates an additionalTools option value like
// the TS strict zod schema (openaiResponsesAdditionalToolSchema): a
// discriminated union of function (with defer_loading), custom and namespace
// definitions; unknown keys, a present null for an optional key and a wrong
// type are rejected.
func parseAdditionalToolEntries(raw interface{}, field string) ([]additionalToolEntry, error) {
	invalid := func(message string) error {
		return &providererrors.InvalidArgumentError{Field: field, Message: message}
	}
	if raw == nil {
		return nil, invalid("expected an array of tool definitions")
	}
	encoded, err := json.Marshal(raw)
	if err != nil {
		return nil, invalid(err.Error())
	}
	var rawEntries []json.RawMessage
	if err := json.Unmarshal(encoded, &rawEntries); err != nil || rawEntries == nil {
		return nil, invalid("expected an array of tool definitions")
	}
	entries := make([]additionalToolEntry, 0, len(rawEntries))
	for i, rawEntry := range rawEntries {
		entryErr := func(message string) error { return invalid(fmt.Sprintf("entry %d: %s", i, message)) }
		var object map[string]json.RawMessage
		if err := json.Unmarshal(rawEntry, &object); err != nil || object == nil {
			return nil, entryErr("not a valid tool definition: expected an object")
		}
		var entryType string
		if err := json.Unmarshal(object["type"], &entryType); err != nil {
			return nil, entryErr("type must be \"function\", \"custom\" or \"namespace\"")
		}
		var entry additionalToolEntry
		var err error
		switch entryType {
		case "function":
			entry, err = parseAdditionalFunctionEntry(object)
		case "custom":
			entry, err = parseAdditionalCustomEntry(object)
		case "namespace":
			entry, err = parseAdditionalNamespaceEntry(object)
		default:
			return nil, entryErr("type must be \"function\", \"custom\" or \"namespace\"")
		}
		if err != nil {
			return nil, entryErr(err.Error())
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

func checkAdditionalToolKeys(object map[string]json.RawMessage, allowed ...string) error {
	for key := range object {
		known := false
		for _, name := range allowed {
			if key == name {
				known = true
				break
			}
		}
		if !known {
			return fmt.Errorf("unrecognized key %q", key)
		}
	}
	return nil
}

func additionalToolName(object map[string]json.RawMessage) (string, error) {
	var name string
	if err := json.Unmarshal(object["name"], &name); err != nil || name == "" {
		return "", fmt.Errorf("name must be a non-empty string")
	}
	return name, nil
}

// optionalAdditionalToolString validates an `.optional()` string key: absent
// is fine, a present null or non-string is invalid.
func optionalAdditionalToolString(object map[string]json.RawMessage, key string) error {
	value, present := object[key]
	if !present {
		return nil
	}
	var s string
	if err := json.Unmarshal(value, &s); err != nil || string(value) == "null" {
		return fmt.Errorf("%s must be a string", key)
	}
	return nil
}

func optionalAdditionalToolBool(object map[string]json.RawMessage, key string) (*bool, error) {
	value, present := object[key]
	if !present {
		return nil, nil
	}
	var b bool
	if err := json.Unmarshal(value, &b); err != nil || string(value) == "null" {
		return nil, fmt.Errorf("%s must be a boolean", key)
	}
	return &b, nil
}

func parseAdditionalFunctionEntry(object map[string]json.RawMessage) (additionalToolEntry, error) {
	if err := checkAdditionalToolKeys(object, "type", "name", "description", "parameters", "strict", "defer_loading"); err != nil {
		return additionalToolEntry{}, err
	}
	name, err := additionalToolName(object)
	if err != nil {
		return additionalToolEntry{}, err
	}
	if err := optionalAdditionalToolString(object, "description"); err != nil {
		return additionalToolEntry{}, err
	}
	var parameters map[string]interface{}
	if err := json.Unmarshal(object["parameters"], &parameters); err != nil || parameters == nil {
		return additionalToolEntry{}, fmt.Errorf("parameters must be an object")
	}
	strict, err := optionalAdditionalToolBool(object, "strict")
	if err != nil {
		return additionalToolEntry{}, err
	}
	deferLoading, err := optionalAdditionalToolBool(object, "defer_loading")
	if err != nil {
		return additionalToolEntry{}, err
	}
	return additionalToolEntry{Type: "function", Name: name, Strict: strict, DeferLoading: deferLoading}, nil
}

func parseAdditionalCustomEntry(object map[string]json.RawMessage) (additionalToolEntry, error) {
	if err := checkAdditionalToolKeys(object, "type", "name", "description", "format"); err != nil {
		return additionalToolEntry{}, err
	}
	name, err := additionalToolName(object)
	if err != nil {
		return additionalToolEntry{}, err
	}
	if err := optionalAdditionalToolString(object, "description"); err != nil {
		return additionalToolEntry{}, err
	}
	if rawFormat, present := object["format"]; present {
		var format map[string]json.RawMessage
		if err := json.Unmarshal(rawFormat, &format); err != nil || format == nil {
			return additionalToolEntry{}, fmt.Errorf("format must be an object")
		}
		var formatType string
		_ = json.Unmarshal(format["type"], &formatType)
		switch formatType {
		case "text":
			if err := checkAdditionalToolKeys(format, "type"); err != nil {
				return additionalToolEntry{}, err
			}
		case "grammar":
			if err := checkAdditionalToolKeys(format, "type", "syntax", "definition"); err != nil {
				return additionalToolEntry{}, err
			}
			var syntax, definition string
			if err := json.Unmarshal(format["syntax"], &syntax); err != nil || (syntax != "regex" && syntax != "lark") {
				return additionalToolEntry{}, fmt.Errorf("format.syntax must be \"regex\" or \"lark\"")
			}
			if err := json.Unmarshal(format["definition"], &definition); err != nil || string(format["definition"]) == "null" {
				return additionalToolEntry{}, fmt.Errorf("format.definition must be a string")
			}
		default:
			return additionalToolEntry{}, fmt.Errorf("format.type must be \"text\" or \"grammar\"")
		}
	}
	return additionalToolEntry{Type: "custom", Name: name}, nil
}

func parseAdditionalNamespaceEntry(object map[string]json.RawMessage) (additionalToolEntry, error) {
	if err := checkAdditionalToolKeys(object, "type", "name", "description", "tools"); err != nil {
		return additionalToolEntry{}, err
	}
	name, err := additionalToolName(object)
	if err != nil {
		return additionalToolEntry{}, err
	}
	var description string
	if err := json.Unmarshal(object["description"], &description); err != nil || string(object["description"]) == "null" {
		return additionalToolEntry{}, fmt.Errorf("description must be a string")
	}
	var rawTools []json.RawMessage
	if err := json.Unmarshal(object["tools"], &rawTools); err != nil || len(rawTools) == 0 {
		return additionalToolEntry{}, fmt.Errorf("tools must be a non-empty array of function definitions")
	}
	names := make([]string, 0, len(rawTools))
	for j, rawTool := range rawTools {
		var tool map[string]json.RawMessage
		if err := json.Unmarshal(rawTool, &tool); err != nil || tool == nil {
			return additionalToolEntry{}, fmt.Errorf("tools[%d] must be a function definition", j)
		}
		var toolType string
		_ = json.Unmarshal(tool["type"], &toolType)
		if toolType != "function" {
			return additionalToolEntry{}, fmt.Errorf("tools[%d]: type must be \"function\"", j)
		}
		nested, err := parseAdditionalFunctionEntry(tool)
		if err != nil {
			return additionalToolEntry{}, fmt.Errorf("tools[%d]: %w", j, err)
		}
		names = append(names, nested.Name)
	}
	return additionalToolEntry{Type: "namespace", Name: name, NamespaceToolNames: names}, nil
}

// toolResultAdditionalTools reads providerOptions.<name>.additionalTools of a
// tool result, falling back to providerOptions.openai when the request's own
// provider name does not set it (TS parseToolResultAdditionalTools).
func toolResultAdditionalTools(part types.ToolResultContent, providerName string) ([]additionalToolEntry, error) {
	names := []string{providerName}
	if providerName != "openai" {
		names = append(names, "openai")
	}
	for _, name := range names {
		options, ok := providerutils.NormalizeProviderOptionsObject(part.ProviderOptions[name])
		if !ok {
			continue
		}
		raw, present := options["additionalTools"]
		if !present {
			continue
		}
		return parseAdditionalToolEntries(raw, "providerOptions."+name+".additionalTools")
	}
	return nil, nil
}

// additionalToolsState is the per-conversion state TS keeps in
// seenAdditionalToolWarnings and droppedAdditionalToolNames.
type additionalToolsState struct {
	seenWarnings map[string]bool
	dropped      []string
	droppedSet   map[string]bool
}

func newAdditionalToolsState() *additionalToolsState {
	return &additionalToolsState{seenWarnings: map[string]bool{}, droppedSet: map[string]bool{}}
}

func (s *additionalToolsState) drop(name string) {
	if !s.droppedSet[name] {
		s.droppedSet[name] = true
		s.dropped = append(s.dropped, name)
	}
}

func (s *additionalToolsState) warn(warnings *[]types.Warning, w types.Warning) {
	signature := w.Type + "\x00" + w.Feature + "\x00" + w.Details + "\x00" + w.Message
	if !s.seenWarnings[signature] {
		s.seenWarnings[signature] = true
		*warnings = append(*warnings, w)
	}
}

// droppedWarning is the single end-of-conversion warning for entries whose
// tool is not registered.
func (s *additionalToolsState) droppedWarning() *types.Warning {
	if s == nil || len(s.dropped) == 0 {
		return nil
	}
	return &types.Warning{
		Type:    "unsupported",
		Feature: "additionalTools",
		Details: "additional tool definitions are only sent for tools registered in the request; dropped the definitions of unregistered tools: " + formatDroppedToolNames(s.dropped),
	}
}

// filterAdditionalTools replaces additionalTools entries with the definitions
// of the tools registered for the request (TS filterAdditionalTools). Entries
// whose tool is not registered are dropped and recorded for one warning per
// conversion. Without a resolver every entry is dropped.
func filterAdditionalTools(entries []additionalToolEntry, opts ConvertOptions, warnings *[]types.Warning) ([]interface{}, error) {
	if len(entries) == 0 {
		return nil, nil
	}
	state := opts.additionalTools
	var resolved []interface{}
	seenDefinitions := map[string]bool{}
	for _, entry := range entries {
		var resolution *AdditionalToolResolution
		if opts.FindAdditionalToolDefinition != nil {
			var err error
			resolution, err = opts.FindAdditionalToolDefinition(AdditionalToolRef{Type: entry.Type, Name: entry.Name})
			if err != nil {
				return nil, err
			}
		}
		if resolution == nil {
			state.drop(entry.Name)
			continue
		}
		for _, w := range resolution.Warnings {
			state.warn(warnings, w)
		}
		if resolution.Definition == nil {
			continue
		}
		pushDefinitionMismatchWarning(entry, resolution.Definition, state, warnings)
		definition := narrowNamespaceDefinition(resolution.Definition, entry, state)
		if definition == nil {
			continue
		}
		// Replayed history can repeat an entry; send each definition once.
		encoded, err := jsonstr.Marshal(definition)
		if err != nil {
			return nil, err
		}
		if seenDefinitions[string(encoded)] {
			continue
		}
		seenDefinitions[string(encoded)] = true
		resolved = append(resolved, definition)
	}
	return resolved, nil
}

// narrowNamespaceDefinition sends only the registered tools of a namespace
// that the entry names, so an entry cannot load more of a namespace than it
// asked for.
func narrowNamespaceDefinition(registered interface{}, entry additionalToolEntry, state *additionalToolsState) interface{} {
	namespace, ok := registered.(AdditionalNamespaceToolDef)
	if !ok || entry.Type != "namespace" {
		return registered
	}
	requested := map[string]bool{}
	for _, name := range entry.NamespaceToolNames {
		requested[name] = true
	}
	registeredNames := map[string]bool{}
	for _, tool := range namespace.Tools {
		registeredNames[tool.Name] = true
	}
	for _, name := range entry.NamespaceToolNames {
		if !registeredNames[name] {
			state.drop(name)
		}
	}
	var tools []AdditionalToolDef
	for _, tool := range namespace.Tools {
		if requested[tool.Name] {
			tools = append(tools, tool)
		}
	}
	if len(tools) == 0 {
		return nil
	}
	namespace.Tools = tools
	return namespace
}

// pushDefinitionMismatchWarning warns when a function entry's strict or
// defer_loading value differs from the registered tool: the entry is not sent,
// so the ignored value must not be silent.
func pushDefinitionMismatchWarning(entry additionalToolEntry, definition interface{}, state *additionalToolsState, warnings *[]types.Warning) {
	function, ok := definition.(AdditionalToolDef)
	if !ok || entry.Type != "function" {
		return
	}
	var mismatched []string
	registeredStrict := function.Strict
	if entry.Strict != nil && (registeredStrict == nil || *entry.Strict != *registeredStrict) {
		mismatched = append(mismatched, "strict")
	}
	registeredDefer := false
	if function.DeferLoading != nil {
		registeredDefer = *function.DeferLoading
	}
	if entry.DeferLoading != nil && *entry.DeferLoading != registeredDefer {
		mismatched = append(mismatched, "defer_loading")
	}
	if len(mismatched) == 0 {
		return
	}
	state.warn(warnings, types.Warning{
		Type:    "unsupported",
		Feature: fmt.Sprintf("additionalTools entry \"%s\"", function.Name),
		Details: fmt.Sprintf("the history entry sets %s differently from the registered tool; the registered definition is sent", strings.Join(mismatched, " and ")),
	})
}

// formatDroppedToolNames lists at most five dropped names, each cut to 80
// UTF-16 code units, as JSON strings (TS formatDroppedToolNames).
func formatDroppedToolNames(names []string) string {
	const maxNames, maxNameLength = 5, 80
	shown := names
	if len(shown) > maxNames {
		shown = shown[:maxNames]
	}
	formatted := make([]string, 0, len(shown))
	for _, name := range shown {
		formatted = append(formatted, jsonStringTruncated(name, maxNameLength))
	}
	if len(names) > maxNames {
		return fmt.Sprintf("%s and %d more", strings.Join(formatted, ", "), len(names)-maxNames)
	}
	return strings.Join(formatted, ", ")
}

// jsonStringTruncated is JSON.stringify(name.length > max ? name.slice(0,
// max) + '...' : name). The slice counts UTF-16 code units, so a cut inside a
// surrogate pair leaves a lone surrogate, which JSON.stringify writes as a
// \uXXXX escape.
func jsonStringTruncated(name string, max int) string {
	units := utf16.Encode([]rune(name))
	if len(units) <= max {
		encoded, _ := jsonstr.Marshal(name)
		return string(encoded)
	}
	cut := units[:max]
	lone := ""
	if last := cut[len(cut)-1]; last >= 0xD800 && last <= 0xDBFF {
		cut = cut[:len(cut)-1]
		lone = fmt.Sprintf("\\u%04x", last)
	}
	encoded, _ := jsonstr.Marshal(string(utf16.Decode(cut)))
	return strings.TrimSuffix(string(encoded), "\"") + lone + "...\""
}
