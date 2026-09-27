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

// IsExtensionRecord reports whether value is a JSON object with a
// namespaced "type" field.
func IsExtensionRecord(value map[string]interface{}) bool {
	return IsNamespacedType(ExtensionRecord(value).Type())
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
	ToolType string

	// ItemTypes are the namespaced item types this extension decodes. Must
	// be set together with DecodeItem.
	ItemTypes []string

	// EventTypes are the namespaced streaming event types this extension
	// decodes. Must be set together with DecodeEvent.
	EventTypes []string

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
			return nil, fmt.Errorf("Open Responses extension ID %s must use <implementor>.<extension> format.", ext.ID)
		}
		namespace := ext.ID[:dotIndex]

		if err := registerUniqueExtension(registry.ByExtensionID, ext.ID, ext, "id"); err != nil {
			return nil, err
		}

		hasToolType := ext.ToolType != ""
		hasToolEncoder := ext.EncodeTool != nil
		if hasToolType != hasToolEncoder {
			return nil, fmt.Errorf("Open Responses extension %s must provide toolType and encodeTool together.", ext.ID)
		}
		if ext.EncodeToolChoice != nil && !hasToolEncoder {
			return nil, fmt.Errorf("Open Responses extension %s cannot provide encodeToolChoice without toolType and encodeTool.", ext.ID)
		}
		if hasToolType && hasToolEncoder {
			if err := assertNamespacedExtensionType(ext.ID, namespace, ext.ToolType, "toolType"); err != nil {
				return nil, err
			}
			if err := registerUniqueExtension(registry.ByProviderToolID, ext.ID, ext, "provider-tool id"); err != nil {
				return nil, err
			}
			if err := registerUniqueExtension(registry.ByToolType, ext.ToolType, ext, "toolType"); err != nil {
				return nil, err
			}
		}

		// itemTypesProvided/eventTypesProvided track whether the field was
		// set at all (a non-nil slice, even if empty), distinct from
		// "non-empty" -- an extension that sets ItemTypes: []string{} must
		// get the dedicated "must register at least one item type" error
		// below, not the "must provide ItemTypes and DecodeItem together"
		// mismatch error.
		itemTypesProvided := ext.ItemTypes != nil
		hasItemDecoder := ext.DecodeItem != nil
		if itemTypesProvided != hasItemDecoder {
			return nil, fmt.Errorf("Open Responses extension %s must provide itemTypes and decodeItem together.", ext.ID)
		}
		if ext.EncodeInputItem != nil && !hasItemDecoder {
			return nil, fmt.Errorf("Open Responses extension %s cannot provide encodeInputItem without itemTypes and decodeItem.", ext.ID)
		}
		if itemTypesProvided && hasItemDecoder {
			if len(ext.ItemTypes) == 0 {
				return nil, fmt.Errorf("Open Responses extension %s must register at least one item type.", ext.ID)
			}
			for _, itemType := range ext.ItemTypes {
				if err := assertNamespacedExtensionType(ext.ID, namespace, itemType, "itemTypes"); err != nil {
					return nil, err
				}
				if err := registerUniqueExtension(registry.ByItemType, itemType, ext, "item type"); err != nil {
					return nil, err
				}
			}
		}

		eventTypesProvided := ext.EventTypes != nil
		hasEventDecoder := ext.DecodeEvent != nil
		if eventTypesProvided != hasEventDecoder {
			return nil, fmt.Errorf("Open Responses extension %s must provide eventTypes and decodeEvent together.", ext.ID)
		}
		if eventTypesProvided && hasEventDecoder {
			if len(ext.EventTypes) == 0 {
				return nil, fmt.Errorf("Open Responses extension %s must register at least one event type.", ext.ID)
			}
			for _, eventType := range ext.EventTypes {
				if err := assertNamespacedExtensionType(ext.ID, namespace, eventType, "eventTypes"); err != nil {
					return nil, err
				}
				if err := registerUniqueExtension(registry.ByEventType, eventType, ext, "event type"); err != nil {
					return nil, err
				}
			}
		}

		if !hasToolEncoder && !hasItemDecoder && !hasEventDecoder {
			return nil, fmt.Errorf("Open Responses extension %s must register a tool, item, or event capability.", ext.ID)
		}
	}

	return registry, nil
}

func assertNamespacedExtensionType(extensionID, namespace, wireType, field string) error {
	colonIndex := strings.Index(wireType, ":")
	if colonIndex < 0 || wireType[:colonIndex] != namespace {
		return fmt.Errorf("Open Responses extension %s has invalid %s value %s. Extension wire types must use the %s: namespace.", extensionID, field, wireType, namespace)
	}
	return nil
}

func registerUniqueExtension(m map[string]*Extension, key string, ext *Extension, field string) error {
	if existing, ok := m[key]; ok {
		return fmt.Errorf("Open Responses extension %s cannot register %s %s because it is already registered by %s.", ext.ID, field, key, existing.ID)
	}
	m[key] = ext
	return nil
}
