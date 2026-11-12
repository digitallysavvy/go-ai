package telemetry

import (
	"errors"
	"reflect"
)

// filteredErrorText replaces every error a telemetry event carries when
// either recording flag is off (TS filterTelemetryEvent: raw errors can
// contain inputs or outputs).
const filteredErrorText = "AI SDK operation failed"

var errorInterface = reflect.TypeOf((*error)(nil)).Elem()

// hiddenToolError stands in for a tool error whose content RecordOutputs
// hides. TS keeps `toolOutput.type: 'tool-error'` and sets `error`
// undefined, so its spans get an error status with no message or
// exception; Go's flat tool event has no type, so the error stays non-nil
// and the built-in integrations record it the TS way (see
// isHiddenToolError).
type hiddenToolError struct{}

func (hiddenToolError) Error() string { return filteredErrorText }

// isHiddenToolError reports whether err is the placeholder for a hidden tool
// error (a span records only the error status, as TS recordErrorOnSpan /
// recordSpanError do for a non-Error value).
func isHiddenToolError(err error) bool {
	var h hiddenToolError
	return errors.As(err, &h)
}

// FilterEvent returns a copy of event with the content the recording flags
// exclude removed, mirroring TS filterTelemetryEvent (8da48db187,
// 883fcde5f9). It is applied by every Fire* function before the event
// reaches PublishDiagnostic or any integration, so custom integrations and
// diagnostic subscribers never see what RecordInputs / RecordOutputs hide.
// The caller's event is never modified; application callbacks and results
// keep their full content.
//
// Known provider credentials are then redacted in the copy (TS
// redactTelemetryCredentials, see redactCredentials), whatever the flags.
//
// When neither flag is false nothing else changes. Otherwise,
// field by name (the Go spelling of the TS keys):
//   - inputs hidden: Instructions, InstructionMessages, System, Prompt,
//     PromptString, Tools, StepTools, Schema, SchemaName, SchemaDescription, Args and a
//     non-string Output are cleared; Messages, PromptMessages,
//     ExperimentalSteeringMessages, Values, Documents and ProviderOptions
//     become empty; Value and Query become empty; State becomes an empty
//     list, "" or an empty object by its kind; Questions an empty object;
//     Text only for "ai.generateSpeech"; the Document of each Ranking entry
//     becomes "" or an empty object.
//   - outputs hidden: Content, Files, Sources, the tool call and tool result
//     lists, ResponseMessages, Embedding(s), Ranking, Segments, Words,
//     ReasoningParts and ReasoningFiles become empty; Text (except speech),
//     Reasoning, ReasoningText, Object and ObjectText are cleared; Answers
//     becomes an empty object; a tool event's Result is cleared and its
//     Error becomes a placeholder spans record as a bare error status.
//   - either hidden: ProviderMetadata is cleared, every non-nil Error
//     becomes errors.New("AI SDK operation failed"), Request / Response /
//     Responses lose their Body (and their Messages when the matching flag is
//     off), and each of Steps / FinalStep is filtered the same way. A
//     non-tool Result (a Go-only aggregate holding inputs and outputs) is
//     cleared.
//
// Fields that are nil stay nil. RuntimeContext, ToolsContext and Headers
// have their own inclusion policy and are left alone.
func FilterEvent[E any](event E, settings *Options) E {
	hideInputs := !settings.ShouldRecordInputs()
	hideOutputs := !settings.ShouldRecordOutputs()
	if hideInputs || hideOutputs {
		v := reflect.ValueOf(&event).Elem()
		if out, ok := filterAny(v, hideInputs, hideOutputs); ok {
			v.Set(out)
		}
	}
	// TS augmentEvent: redactTelemetryCredentials(filterTelemetryEvent(...)).
	return redactCredentials(event)
}

// filterAny filters a struct, a pointer to a struct or an interface holding
// either, returning a new value (never mutating the input).
func filterAny(v reflect.Value, hideInputs, hideOutputs bool) (reflect.Value, bool) {
	switch v.Kind() {
	case reflect.Struct:
		cp := reflect.New(v.Type()).Elem()
		cp.Set(v)
		filterStruct(cp, hideInputs, hideOutputs)
		return cp, true
	case reflect.Pointer:
		if v.IsNil() || v.Elem().Kind() != reflect.Struct {
			return v, false
		}
		cp := reflect.New(v.Elem().Type())
		cp.Elem().Set(v.Elem())
		filterStruct(cp.Elem(), hideInputs, hideOutputs)
		return cp, true
	case reflect.Interface:
		if v.IsNil() {
			return v, false
		}
		inner, ok := filterAny(v.Elem(), hideInputs, hideOutputs)
		if !ok {
			return v, false
		}
		out := reflect.New(v.Type()).Elem()
		out.Set(inner)
		return out, true
	}
	return v, false
}

func settableField(s reflect.Value, name string) (reflect.Value, bool) {
	f := s.FieldByName(name)
	if !f.IsValid() || !f.CanSet() {
		return reflect.Value{}, false
	}
	return f, true
}

func stringField(s reflect.Value, name string) string {
	f := s.FieldByName(name)
	if f.IsValid() && f.Kind() == reflect.String {
		return f.String()
	}
	return ""
}

func hasField(s reflect.Value, name string) bool {
	return s.FieldByName(name).IsValid()
}

// clearFields sets each named field to its zero value.
func clearFields(s reflect.Value, names ...string) {
	for _, name := range names {
		if f, ok := settableField(s, name); ok {
			f.Set(reflect.Zero(f.Type()))
		}
	}
}

// emptyFields replaces each named non-nil list or object with an empty one
// of the same type (TS `[]` / `{}`); a string becomes "".
func emptyFields(s reflect.Value, names ...string) {
	for _, name := range names {
		if f, ok := settableField(s, name); ok {
			f.Set(emptyLike(f))
		}
	}
}

func emptyLike(f reflect.Value) reflect.Value {
	switch f.Kind() {
	case reflect.Slice:
		if f.IsNil() {
			return f
		}
		return reflect.MakeSlice(f.Type(), 0, 0)
	case reflect.Map:
		if f.IsNil() {
			return f
		}
		return reflect.MakeMap(f.Type())
	case reflect.String:
		return reflect.Zero(f.Type())
	case reflect.Interface:
		if f.IsNil() {
			return f
		}
		inner := f.Elem()
		var e reflect.Value
		switch inner.Kind() {
		case reflect.Slice:
			e = reflect.MakeSlice(inner.Type(), 0, 0)
		case reflect.Map:
			e = reflect.MakeMap(inner.Type())
		case reflect.String:
			e = reflect.Zero(inner.Type())
		default:
			return reflect.Zero(f.Type())
		}
		out := reflect.New(f.Type()).Elem()
		out.Set(e)
		return out
	}
	return reflect.Zero(f.Type())
}

// emptyObjectLike gives TS `{}`: an empty map of the field's map type, or an
// empty map[string]interface{} for an interface field holding anything but
// a list or a string (State keeps its list / string kind).
func emptyObjectLike(f reflect.Value, keepListAndString bool) reflect.Value {
	switch f.Kind() {
	case reflect.Interface:
		if f.IsNil() {
			return f
		}
		inner := f.Elem()
		if keepListAndString && (inner.Kind() == reflect.Slice || inner.Kind() == reflect.Array || inner.Kind() == reflect.String) {
			if inner.Kind() == reflect.Array {
				out := reflect.New(f.Type()).Elem()
				out.Set(reflect.MakeSlice(reflect.SliceOf(inner.Type().Elem()), 0, 0))
				return out
			}
			return emptyLike(f)
		}
		if inner.Kind() == reflect.Map {
			return emptyLike(f)
		}
		obj := reflect.ValueOf(map[string]interface{}{})
		if !obj.Type().AssignableTo(f.Type()) {
			return reflect.Zero(f.Type())
		}
		out := reflect.New(f.Type()).Elem()
		out.Set(obj)
		return out
	case reflect.Slice, reflect.Map, reflect.String:
		return emptyLike(f)
	}
	return reflect.Zero(f.Type())
}

func filterStruct(s reflect.Value, hideInputs, hideOutputs bool) {
	operation := stringField(s, "OperationType")
	if operation == "" {
		operation = stringField(s, "OperationID")
	}
	isSpeech := operation == "ai.generateSpeech"
	isToolEvent := hasField(s, "ToolCallID") && hasField(s, "ToolName")

	if hideInputs {
		clearFields(s, "Instructions", "System", "Prompt", "PromptString", "Tools", "StepTools",
			"Schema", "SchemaName", "SchemaDescription", "Args")
		emptyFields(s, "InstructionMessages", "Messages", "PromptMessages",
			"ExperimentalSteeringMessages", "Values", "Documents",
			"ProviderOptions", "Value", "Query")
		if f, ok := settableField(s, "Output"); ok && f.Kind() != reflect.String {
			if f.Kind() != reflect.Interface || f.IsNil() || f.Elem().Kind() != reflect.String {
				f.Set(reflect.Zero(f.Type()))
			}
		}
		if f, ok := settableField(s, "State"); ok {
			f.Set(emptyObjectLike(f, true))
		}
		if f, ok := settableField(s, "Questions"); ok {
			f.Set(emptyObjectLike(f, false))
		}
		if isSpeech {
			clearFields(s, "Text")
		}
		if f, ok := settableField(s, "Ranking"); ok && f.Kind() == reflect.Slice && !f.IsNil() {
			f.Set(blankRankingDocuments(f))
		}
	}

	if hideOutputs {
		emptyFields(s, "Content", "Files", "Sources", "ToolCalls",
			"StaticToolCalls", "DynamicToolCalls", "ToolResults",
			"StaticToolResults", "DynamicToolResults", "ResponseMessages",
			"Embedding", "Embeddings", "Ranking", "Segments", "Words",
			"ReasoningParts", "ReasoningFiles")
		if !isSpeech {
			clearFields(s, "Text")
		}
		if f, ok := settableField(s, "Reasoning"); ok {
			if f.Kind() == reflect.Slice {
				f.Set(emptyLike(f))
			} else {
				f.Set(reflect.Zero(f.Type()))
			}
		}
		clearFields(s, "ReasoningText", "Object", "ObjectText")
		if f, ok := settableField(s, "Answers"); ok {
			f.Set(emptyObjectLike(f, false))
		}
		if isToolEvent {
			clearFields(s, "Result")
			if f, ok := settableField(s, "Error"); ok && f.Kind() == reflect.Interface &&
				f.Type().Implements(errorInterface) && !f.IsNil() {
				f.Set(reflect.ValueOf(hiddenToolError{}))
			}
		}
	}

	if !isToolEvent {
		clearFields(s, "Result")
	}
	clearFields(s, "ProviderMetadata")
	if f, ok := settableField(s, "Error"); ok && f.Kind() == reflect.Interface &&
		f.Type().Implements(errorInterface) && !f.IsNil() && !isHiddenToolError(f.Interface().(error)) {
		f.Set(reflect.ValueOf(errors.New(filteredErrorText)))
	}

	for _, key := range []string{"Request", "Response", "Responses"} {
		if f, ok := settableField(s, key); ok {
			hideMessages := hideOutputs
			if key == "Request" {
				hideMessages = hideInputs
			}
			f.Set(filterMetadata(f, key == "Request", hideMessages))
		}
	}

	if f, ok := settableField(s, "Steps"); ok && f.Kind() == reflect.Slice && !f.IsNil() {
		steps := reflect.MakeSlice(f.Type(), f.Len(), f.Len())
		for i := 0; i < f.Len(); i++ {
			if out, ok := filterAny(f.Index(i), hideInputs, hideOutputs); ok {
				steps.Index(i).Set(out)
			} else {
				steps.Index(i).Set(f.Index(i))
			}
		}
		f.Set(steps)
	}
	if f, ok := settableField(s, "FinalStep"); ok {
		if out, ok := filterAny(f, hideInputs, hideOutputs); ok {
			f.Set(out)
		}
	}
}

// blankRankingDocuments copies a ranking list with each entry's Document
// replaced by "" (string) or an empty object (TS `{...entry, document: ”
// | {}}`).
func blankRankingDocuments(f reflect.Value) reflect.Value {
	out := reflect.MakeSlice(f.Type(), f.Len(), f.Len())
	for i := 0; i < f.Len(); i++ {
		entry := reflect.New(f.Type().Elem()).Elem()
		entry.Set(f.Index(i))
		if entry.Kind() == reflect.Struct {
			if doc, ok := settableField(entry, "Document"); ok {
				if doc.Kind() == reflect.Interface && !doc.IsNil() && doc.Elem().Kind() == reflect.String {
					doc.Set(reflect.ValueOf("").Convert(doc.Type()))
				} else {
					doc.Set(emptyObjectLike(doc, false))
				}
			}
		}
		out.Index(i).Set(entry)
	}
	return out
}

// filterMetadata drops Body from request / response metadata (a struct, a
// pointer to one, an interface holding either, or a list of them) and, when
// hideMessages, empties Messages and clears Instructions.
func filterMetadata(f reflect.Value, isRequest, hideMessages bool) reflect.Value {
	switch f.Kind() {
	case reflect.Slice:
		if f.IsNil() {
			return f
		}
		out := reflect.MakeSlice(f.Type(), f.Len(), f.Len())
		for i := 0; i < f.Len(); i++ {
			out.Index(i).Set(filterMetadata(f.Index(i), isRequest, hideMessages))
		}
		return out
	case reflect.Struct:
		cp := reflect.New(f.Type()).Elem()
		cp.Set(f)
		filterMetadataStruct(cp, hideMessages)
		return cp
	case reflect.Pointer:
		if f.IsNil() || f.Elem().Kind() != reflect.Struct {
			return f
		}
		cp := reflect.New(f.Elem().Type())
		cp.Elem().Set(f.Elem())
		filterMetadataStruct(cp.Elem(), hideMessages)
		return cp
	case reflect.Interface:
		if f.IsNil() {
			return f
		}
		inner := filterMetadata(f.Elem(), isRequest, hideMessages)
		out := reflect.New(f.Type()).Elem()
		out.Set(inner)
		return out
	}
	return f
}

func filterMetadataStruct(s reflect.Value, hideMessages bool) {
	clearFields(s, "Body")
	if hideMessages {
		emptyFields(s, "Messages")
		clearFields(s, "Instructions")
	}
}
