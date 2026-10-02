// Open Responses extension codecs (row 9a68261, OR-EXT).
//
// An Extension lets a provider that embeds the Open Responses spec (LM
// Studio, Ollama, and similar servers) plug in encode/decode logic for its
// own namespaced tools, output items, and streaming events, without the
// core LanguageModel needing to know about them ahead of time. Namespaced
// wire types use "<implementor>:<type>" (e.g. "lmstudio:code_execution");
// extension ids use "<implementor>.<extension>" (e.g.
// "lmstudio.code_execution") and double as the tool's ProviderID.
//
// This is an advanced, opt-in mechanism (Config.Extensions): most Open
// Responses servers need none of it.
package openresponses

import (
	"fmt"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// ExtensionRecord is a JSON object carrying a namespaced "type" field, the
// wire shape shared by extension tools, items, and streaming events.
type ExtensionRecord map[string]interface{}

// Type returns the record's namespaced "type" field, or "" if missing or
// not a string.
func (r ExtensionRecord) Type() string {
	t, _ := r["type"].(string)
	return t
}

// ID returns the record's "id" field, or "" if missing or not a string.
func (r ExtensionRecord) ID() string {
	id, _ := r["id"].(string)
	return id
}

// ExtensionItem is a completed namespaced output item: an ExtensionRecord
// that also carries "id" and "status".
type ExtensionItem = ExtensionRecord

// ExtensionEvent is a namespaced streaming event: an ExtensionRecord that
// also carries "sequence_number".
type ExtensionEvent = ExtensionRecord

// IsNamespacedType reports whether value has the "<namespace>:<type>" shape.
func IsNamespacedType(value string) bool {
	return strings.Contains(value, ":")
}

// IsExtensionRecord reports whether value is a JSON object with a string
// "type" field. Loosened from requiring a namespaced type (TS commit
// 8cf3f5bb1e, #19939) so a registered bare (non-namespaced) extension type
// also passes this gate; actual dispatch is still only ever to a type
// registered in the extension registry (see decodeExtensionItem/Event),
// so this broadening does not risk misclassifying a core wire type as an
// extension record -- it only widens what's eligible to be looked up.
func IsExtensionRecord(value map[string]interface{}) bool {
	_, ok := value["type"].(string)
	return ok
}

// IsExtensionItem reports whether value is an ExtensionRecord that also
// carries string "id" and "status" fields.
func IsExtensionItem(value map[string]interface{}) bool {
	if !IsExtensionRecord(value) {
		return false
	}
	_, idOK := value["id"].(string)
	_, statusOK := value["status"].(string)
	return idOK && statusOK
}

// IsExtensionEvent reports whether value is an ExtensionRecord that also
// carries a numeric "sequence_number" field (JSON numbers decode to
// float64).
func IsExtensionEvent(value map[string]interface{}) bool {
	if !IsExtensionRecord(value) {
		return false
	}
	switch value["sequence_number"].(type) {
	case float64, int, int64:
		return true
	default:
		return false
	}
}

// Extension defines how a provider encodes and decodes one Open Responses
// extension. Tool, item, and event codecs can be registered independently;
// see NewExtensionRegistry for the pairing rules each requires.
type Extension struct {
	// ID is the extension id, in "<implementor>.<extension>" format. For
	// extensions that encode a provider tool, this also doubles as the
	// tool's ProviderID (types.Tool.ProviderID).
	ID string

	// ToolType is the namespaced Open Responses tool type
	// ("<implementor>:<tool>"). Must be set together with EncodeTool.
	// Mutually exclusive with BareToolType.
	ToolType string

	// ItemTypes are the namespaced item types this extension decodes. Must
	// be set together with DecodeItem.
	ItemTypes []string

	// EventTypes are the namespaced streaming event types this extension
	// decodes. Must be set together with DecodeEvent.
	EventTypes []string

	// AllowBareTypes explicitly opts in to registering non-portable bare
	// (non-namespaced) wire discriminators via BareToolType/BareItemTypes/
	// BareEventTypes, for a documented implementation extension whose wire
	// types cannot be expressed in the "<namespace>:<type>" form (TS commit
	// 8cf3f5bb1e, #19939). Must be set to true exactly when at least one of
	// those bare fields is non-empty.
	AllowBareTypes bool

	// BareToolType is an exact, non-namespaced tool type. Must be set
	// together with EncodeTool and AllowBareTypes: true. Mutually exclusive
	// with ToolType.
	BareToolType string

	// BareItemTypes are exact, non-namespaced item types this extension
	// decodes. Must be set together with DecodeItem and AllowBareTypes:
	// true.
	BareItemTypes []string

	// BareEventTypes are exact, non-namespaced streaming event types this
	// extension decodes. Must be set together with DecodeEvent and
	// AllowBareTypes: true.
	BareEventTypes []string

	// EncodeTool encodes a provider-defined tool's args into the
	// extension's wire fields. Returning (nil, nil) means the tool can't be
	// encoded, reported by the caller as an "unsupported" warning; ToolType
	// is added by the caller, not by this function.
	EncodeTool func(name string, args map[string]interface{}) (map[string]interface{}, error)

	// EncodeToolChoice encodes a specific tool_choice targeting this
	// extension's tool. When nil, the caller defaults to
	// {"type": ToolType}.
	EncodeToolChoice func(name string, args map[string]interface{}) (map[string]interface{}, error)

	// DecodeItem decodes a completed namespaced output item into content
	// parts, for both "generate" and "stream" mode. The caller adds a
	// replay carrier (a types.CustomContent with Kind
	// "open-responses.extension-replay") preserving the original item, plus
	// a light {id, itemId} reference on each returned part, so a later turn
	// can round-trip the item without needing EncodeInputItem.
	DecodeItem func(item ExtensionItem, mode string) ([]types.ContentPart, error)

	// EncodeInputItem encodes an AI SDK tool-call/tool-result content part
	// back into one or more wire items, used only when no original wire
	// item was preserved for replay (e.g. a tool call synthesized fresh,
	// never decoded from a response). Every returned item must use one of
	// ItemTypes.
	EncodeInputItem func(part types.ContentPart, tool types.Tool) ([]ExtensionItem, error)

	// DecodeEvent decodes a namespaced streaming event into stream chunks.
	// state persists for the lifetime of one response stream and is shared
	// across all DecodeEvent calls for that stream.
	DecodeEvent func(event ExtensionEvent, state map[string]interface{}) ([]*provider.StreamChunk, error)
}

// ExtensionRegistry indexes a set of Extensions for fast lookup during
// request preparation and response decoding.
type ExtensionRegistry struct {
	ByEventType      map[string]*Extension
	ByExtensionID    map[string]*Extension
	ByItemType       map[string]*Extension
	ByProviderToolID map[string]*Extension
	ByToolType       map[string]*Extension
}

// NewExtensionRegistry validates extensions and builds a lookup registry
// from them, mirroring TS createOpenResponsesExtensionRegistry.
func NewExtensionRegistry(extensions []Extension) (*ExtensionRegistry, error) {
	registry := &ExtensionRegistry{
		ByEventType:      map[string]*Extension{},
		ByExtensionID:    map[string]*Extension{},
		ByItemType:       map[string]*Extension{},
		ByProviderToolID: map[string]*Extension{},
		ByToolType:       map[string]*Extension{},
	}

	for i := range extensions {
		ext := &extensions[i]

		dotIndex := strings.Index(ext.ID, ".")
		if dotIndex <= 0 {
			return nil, fmt.Errorf("Open Responses extension ID %s must use <implementor>.<extension> format.", ext.ID) //nolint:staticcheck // matches TS SDK's exact error text
		}
		namespace := ext.ID[:dotIndex]

		if err := registerUniqueExtension(registry.ByExtensionID, ext.ID, ext, "id"); err != nil {
			return nil, err
		}

		hasBareTypes := ext.BareToolType != "" || ext.BareItemTypes != nil || ext.BareEventTypes != nil
		if hasBareTypes != ext.AllowBareTypes {
			return nil, fmt.Errorf("Open Responses extension %s must set allowBareTypes to true exactly when registering bare types.", ext.ID) //nolint:staticcheck // matches TS SDK's exact error text
		}
		if ext.ToolType != "" && ext.BareToolType != "" {
			return nil, fmt.Errorf("Open Responses extension %s cannot provide toolType and bareToolType together.", ext.ID) //nolint:staticcheck // matches TS SDK's exact error text
		}

		toolType := ext.ToolType
		if toolType == "" {
			toolType = ext.BareToolType
		}
		hasToolType := toolType != ""
		hasToolEncoder := ext.EncodeTool != nil
		if hasToolType != hasToolEncoder {
			typeField := "toolType"
			if ext.BareToolType != "" {
				typeField = "bareToolType"
			}
			return nil, fmt.Errorf("Open Responses extension %s must provide %s and encodeTool together.", ext.ID, typeField) //nolint:staticcheck // matches TS SDK's exact error text
		}
		if ext.EncodeToolChoice != nil && !hasToolEncoder {
			return nil, fmt.Errorf("Open Responses extension %s cannot provide encodeToolChoice without a tool type and encodeTool.", ext.ID) //nolint:staticcheck // matches TS SDK's exact error text
		}
		if hasToolType && hasToolEncoder {
			if ext.ToolType != "" {
				if err := assertNamespacedExtensionType(ext.ID, namespace, ext.ToolType, "toolType"); err != nil {
					return nil, err
				}
			} else if err := assertBareExtensionType(ext.ID, openResponsesCoreToolTypes, ext.BareToolType, "bareToolType"); err != nil {
				return nil, err
			}
			if err := registerUniqueExtension(registry.ByProviderToolID, ext.ID, ext, "provider-tool id"); err != nil {
				return nil, err
			}
			if err := registerUniqueExtension(registry.ByToolType, toolType, ext, "tool type"); err != nil {
				return nil, err
			}
		}

		// itemTypesProvided/eventTypesProvided track whether either field was
		// set at all (a non-nil slice, even if empty), distinct from
		// "non-empty" -- an extension that sets ItemTypes: []string{} must
		// get the dedicated "must register at least one item type" error
		// below, not the "must provide itemTypes and decodeItem together"
		// mismatch error.
		itemTypesProvided := ext.ItemTypes != nil || ext.BareItemTypes != nil
		hasItemDecoder := ext.DecodeItem != nil
		if itemTypesProvided != hasItemDecoder {
			return nil, fmt.Errorf("Open Responses extension %s must provide itemTypes or bareItemTypes together with decodeItem.", ext.ID) //nolint:staticcheck // matches TS SDK's exact error text
		}
		if ext.EncodeInputItem != nil && !hasItemDecoder {
			return nil, fmt.Errorf("Open Responses extension %s cannot provide encodeInputItem without itemTypes or bareItemTypes and decodeItem.", ext.ID) //nolint:staticcheck // matches TS SDK's exact error text
		}
		if itemTypesProvided && hasItemDecoder {
			if len(ext.ItemTypes)+len(ext.BareItemTypes) == 0 {
				return nil, fmt.Errorf("Open Responses extension %s must register at least one item type.", ext.ID) //nolint:staticcheck // matches TS SDK's exact error text
			}
			for _, itemType := range ext.ItemTypes {
				if err := assertNamespacedExtensionType(ext.ID, namespace, itemType, "itemTypes"); err != nil {
					return nil, err
				}
				if err := registerUniqueExtension(registry.ByItemType, itemType, ext, "item type"); err != nil {
					return nil, err
				}
			}
			for _, itemType := range ext.BareItemTypes {
				if err := assertBareExtensionType(ext.ID, openResponsesCoreItemTypes, itemType, "bareItemTypes"); err != nil {
					return nil, err
				}
				if err := registerUniqueExtension(registry.ByItemType, itemType, ext, "item type"); err != nil {
					return nil, err
				}
			}
		}

		eventTypesProvided := ext.EventTypes != nil || ext.BareEventTypes != nil
		hasEventDecoder := ext.DecodeEvent != nil
		if eventTypesProvided != hasEventDecoder {
			return nil, fmt.Errorf("Open Responses extension %s must provide eventTypes or bareEventTypes together with decodeEvent.", ext.ID) //nolint:staticcheck // matches TS SDK's exact error text
		}
		if eventTypesProvided && hasEventDecoder {
			if len(ext.EventTypes)+len(ext.BareEventTypes) == 0 {
				return nil, fmt.Errorf("Open Responses extension %s must register at least one event type.", ext.ID) //nolint:staticcheck // matches TS SDK's exact error text
			}
			for _, eventType := range ext.EventTypes {
				if err := assertNamespacedExtensionType(ext.ID, namespace, eventType, "eventTypes"); err != nil {
					return nil, err
				}
				if err := registerUniqueExtension(registry.ByEventType, eventType, ext, "event type"); err != nil {
					return nil, err
				}
			}
			for _, eventType := range ext.BareEventTypes {
				if err := assertBareExtensionType(ext.ID, openResponsesCoreEventTypes, eventType, "bareEventTypes"); err != nil {
					return nil, err
				}
				if err := registerUniqueExtension(registry.ByEventType, eventType, ext, "event type"); err != nil {
					return nil, err
				}
			}
		}

		if !hasToolEncoder && !hasItemDecoder && !hasEventDecoder {
			return nil, fmt.Errorf("Open Responses extension %s must register a tool, item, or event capability.", ext.ID) //nolint:staticcheck // matches TS SDK's exact error text
		}
	}

	return registry, nil
}

// openResponsesCoreToolTypes, openResponsesCoreItemTypes, and
// openResponsesCoreEventTypes are the wire discriminators a bare extension
// registration must not collide with, mirroring TS's coreToolTypes/
// coreItemTypes/coreEventTypes (open-responses-extension.ts).
var (
	openResponsesCoreToolTypes = map[string]bool{
		"allowed_tools": true,
		"function":      true,
	}
	openResponsesCoreItemTypes = map[string]bool{
		"function_call":        true,
		"function_call_output": true,
		"item_reference":       true,
		"message":              true,
		"reasoning":            true,
	}
	openResponsesCoreEventTypes = map[string]bool{
		"error":                                  true,
		"response.completed":                     true,
		"response.content_part.added":            true,
		"response.content_part.done":             true,
		"response.created":                       true,
		"response.failed":                        true,
		"response.function_call_arguments.delta": true,
		"response.function_call_arguments.done":  true,
		"response.in_progress":                   true,
		"response.incomplete":                    true,
		"response.output_item.added":             true,
		"response.output_item.done":              true,
		"response.output_text.delta":             true,
		"response.output_text.done":              true,
		"response.reasoning_summary_part.added":  true,
		"response.reasoning_summary_part.done":   true,
		"response.reasoning_summary_text.delta":  true,
		"response.reasoning_summary_text.done":   true,
		"response.reasoning_text.delta":          true,
		"response.refusal.delta":                 true,
		"response.refusal.done":                  true,
	}
)

func assertNamespacedExtensionType(extensionID, namespace, wireType, field string) error {
	colonIndex := strings.Index(wireType, ":")
	if colonIndex < 0 || wireType[:colonIndex] != namespace {
		return fmt.Errorf("Open Responses extension %s has invalid %s value %s. Extension wire types must use the %s: namespace.", extensionID, field, wireType, namespace) //nolint:staticcheck // matches TS SDK's exact error text
	}
	return nil
}

// assertBareExtensionType validates a bare (non-namespaced) wire type
// registration, mirroring TS's assertBareType: non-empty, no colon
// (colons are reserved for namespaced types), and not a core wire type.
func assertBareExtensionType(extensionID string, coreTypes map[string]bool, wireType, field string) error {
	if wireType == "" || strings.Contains(wireType, ":") {
		return fmt.Errorf("Open Responses extension %s has invalid %s value %s. Bare extension wire types must be non-empty and must not contain a colon.", extensionID, field, wireType) //nolint:staticcheck // matches TS SDK's exact error text
	}
	if coreTypes[wireType] {
		return fmt.Errorf("Open Responses extension %s cannot register core %s value %s.", extensionID, field, wireType) //nolint:staticcheck // matches TS SDK's exact error text
	}
	return nil
}

// ExtensionToolType returns ext's effective tool wire type, whichever of
// ToolType/BareToolType is set (they are mutually exclusive), or "" if
// neither is, mirroring TS's getOpenResponsesExtensionToolType.
func ExtensionToolType(ext *Extension) string {
	if ext.ToolType != "" {
		return ext.ToolType
	}
	return ext.BareToolType
}

// ExtensionItemTypes returns the union of ext's namespaced and bare item
// types, mirroring TS's getOpenResponsesExtensionItemTypes.
func ExtensionItemTypes(ext *Extension) []string {
	if len(ext.BareItemTypes) == 0 {
		return ext.ItemTypes
	}
	out := make([]string, 0, len(ext.ItemTypes)+len(ext.BareItemTypes))
	out = append(out, ext.ItemTypes...)
	out = append(out, ext.BareItemTypes...)
	return out
}

func registerUniqueExtension(m map[string]*Extension, key string, ext *Extension, field string) error {
	if existing, ok := m[key]; ok {
		return fmt.Errorf("Open Responses extension %s cannot register %s %s because it is already registered by %s.", ext.ID, field, key, existing.ID) //nolint:staticcheck // matches TS SDK's exact error text
	}
	m[key] = ext
	return nil
}
