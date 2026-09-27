// Extension item/event decode helpers (row 9a68261, OR-EXT).
package openresponses

import (
	"encoding/json"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// extensionReplayKind is the Kind of the synthetic CustomContent part added
// alongside an extension's decoded content, preserving the original wire
// item so a later turn can resend it verbatim instead of re-encoding it via
// Extension.EncodeInputItem.
const extensionReplayKind = "open-responses.extension-replay"

// decodeExtensionItem looks up rawItem's namespaced type in the registry and,
// if a matching extension is registered, decodes it into content parts.
// handled reports whether an extension matched (regardless of decode
// success), mirroring TS's decodeExtensionItem + isOpenResponsesExtensionItem
// gate.
func decodeExtensionItem(registry *ExtensionRegistry, rawItem json.RawMessage, mode, providerName string) (parts []types.ContentPart, handled bool, err error) {
	if registry == nil || len(rawItem) == 0 {
		return nil, false, nil
	}
	var record ExtensionRecord
	if err := json.Unmarshal(rawItem, &record); err != nil {
		return nil, false, nil
	}
	if !IsExtensionItem(record) {
		return nil, false, nil
	}
	ext, ok := registry.ByItemType[record.Type()]
	if !ok || ext.DecodeItem == nil {
		return nil, false, nil
	}

	decoded, err := ext.DecodeItem(record, mode)
	if err != nil {
		return nil, true, err
	}
	if decoded == nil {
		return nil, true, nil
	}

	itemID := record.ID()
	result := make([]types.ContentPart, 0, len(decoded)+1)
	result = append(result, buildExtensionReplayCarrier(ext.ID, record, providerName))
	for _, part := range decoded {
		result = append(result, attachExtensionItemReference(part, ext.ID, itemID, providerName))
	}
	return result, true, nil
}

// buildExtensionReplayCarrier builds the synthetic CustomContent part that
// preserves item verbatim for replay, mirroring TS createExtensionReplayCarrier.
func buildExtensionReplayCarrier(extensionID string, item ExtensionRecord, providerName string) types.CustomContent {
	raw, _ := json.Marshal(map[string]interface{}{
		providerName: map[string]interface{}{
			"openResponsesExtension": map[string]interface{}{
				"id":   extensionID,
				"item": map[string]interface{}(item),
			},
		},
	})
	return types.CustomContent{
		Kind:             extensionReplayKind,
		ProviderMetadata: raw,
	}
}

// attachExtensionItemReference merges a light {id, itemId} extension
// reference into part's provider metadata, mirroring TS
// addExtensionItemReferenceMetadata, which runs unconditionally over every
// part in OpenResponsesExtensionContentPart --
// Extract<LanguageModelV4Content, LanguageModelV4StreamPart> -- i.e. every
// full (non-delta) content part EXCEPT text and reasoning (those only exist
// as -start/-delta/-end stream parts, never as an aggregated shape a
// decodeItem could return). tool-call, tool-result, custom, file,
// reasoning-file, tool-approval-request, and source are all covered; text
// and reasoning content is returned unchanged since DecodeItem is not
// expected to (and structurally cannot, in TS) return those.
func attachExtensionItemReference(part types.ContentPart, extensionID, itemID, providerName string) types.ContentPart {
	reference := map[string]interface{}{"id": extensionID, "itemId": itemID}

	switch p := part.(type) {
	case types.ToolCallContent:
		p.ProviderMetadata = mergeExtensionReference(p.ProviderMetadata, providerName, reference)
		return p
	case types.ToolResultContent:
		p.ProviderMetadata = mergeExtensionReference(p.ProviderMetadata, providerName, reference)
		return p
	case types.CustomContent:
		p.ProviderMetadata = mergeExtensionReference(p.ProviderMetadata, providerName, reference)
		return p
	case types.GeneratedFileContent:
		p.ProviderMetadata = mergeExtensionReference(p.ProviderMetadata, providerName, reference)
		return p
	case types.ReasoningFileContent:
		p.ProviderMetadata = mergeExtensionReference(p.ProviderMetadata, providerName, reference)
		return p
	// types.ToolApprovalRequestContent has no top-level ProviderMetadata
	// field to merge a reference into (Go models its provider metadata only
	// on the nested ToolCall), so it is returned unchanged like text/
	// reasoning; an extension that decodes to this type cannot round-trip
	// via the lightweight {id, itemId} reference and must rely on the
	// replay carrier instead.
	case types.SourceContent:
		p.ProviderMetadata = mergeExtensionReference(p.ProviderMetadata, providerName, reference)
		return p
	default:
		return part
	}
}

func mergeExtensionReference(existing json.RawMessage, providerName string, reference map[string]interface{}) json.RawMessage {
	metadata := map[string]interface{}{}
	if len(existing) > 0 {
		_ = json.Unmarshal(existing, &metadata) //nolint:errcheck
	}
	providerMeta, ok := metadata[providerName].(map[string]interface{})
	if !ok {
		providerMeta = map[string]interface{}{}
	}
	providerMeta["openResponsesExtension"] = reference
	metadata[providerName] = providerMeta
	raw, err := json.Marshal(metadata)
	if err != nil {
		return existing
	}
	return raw
}

// extensionToolCallProviderMetadata converts a decoded ToolCallContent's
// json.RawMessage provider metadata into the map[string]interface{} shape
// types.ToolCall.ProviderMetadata expects.
func extensionToolCallProviderMetadata(raw json.RawMessage) map[string]interface{} {
	if len(raw) == 0 {
		return nil
	}
	var metadata map[string]interface{}
	if err := json.Unmarshal(raw, &metadata); err != nil {
		return nil
	}
	return metadata
}

// extensionReferenceInfo extracts the light {id, itemId} extension reference
// attached by attachExtensionItemReference from a decoded content part's
// provider metadata, for input replay.
func extensionReferenceInfo(metadata json.RawMessage, providerName string) (extensionID, itemID string) {
	if len(metadata) == 0 {
		return "", ""
	}
	var wrapper map[string]interface{}
	if err := json.Unmarshal(metadata, &wrapper); err != nil {
		return "", ""
	}
	providerData, ok := wrapper[providerName].(map[string]interface{})
	if !ok {
		return "", ""
	}
	extData, ok := providerData["openResponsesExtension"].(map[string]interface{})
	if !ok {
		return "", ""
	}
	extensionID, _ = extData["id"].(string)
	itemID, _ = extData["itemId"].(string)
	return extensionID, itemID
}

// extensionReplayInputItem extracts the original wire item preserved by an
// "open-responses.extension-replay" CustomContent's provider metadata (see
// buildExtensionReplayCarrier), ready to resend verbatim as an input item.
func extensionReplayInputItem(metadata json.RawMessage, providerName string) (item map[string]interface{}, itemID string) {
	if len(metadata) == 0 {
		return nil, ""
	}
	var wrapper map[string]interface{}
	if err := json.Unmarshal(metadata, &wrapper); err != nil {
		return nil, ""
	}
	providerData, ok := wrapper[providerName].(map[string]interface{})
	if !ok {
		return nil, ""
	}
	extData, ok := providerData["openResponsesExtension"].(map[string]interface{})
	if !ok {
		return nil, ""
	}
	rawItem, ok := extData["item"].(map[string]interface{})
	if !ok {
		return nil, ""
	}
	id, _ := rawItem["id"].(string)
	return rawItem, id
}

// encodeExtensionInputItems re-encodes an AI SDK tool-call/tool-result
// content part into wire items via its extension's EncodeInputItem, used
// when no original wire item was preserved for replay (row 9a68261,
// OR-EXT). Returns nil when extensionID is empty, no matching extension (or
// EncodeInputItem) is registered, or encoding fails.
func encodeExtensionInputItems(extensionID string, part types.ContentPart, opts openResponsesExtensionOptions) []interface{} {
	if extensionID == "" || opts.Registry == nil {
		return nil
	}
	ext, ok := opts.Registry.ByExtensionID[extensionID]
	if !ok || ext.EncodeInputItem == nil {
		return nil
	}
	tool := findToolByProviderID(opts.Tools, extensionID)
	encoded, err := ext.EncodeInputItem(part, tool)
	if err != nil || len(encoded) == 0 {
		return nil
	}
	items := make([]interface{}, 0, len(encoded))
	for _, item := range encoded {
		items = append(items, map[string]interface{}(item))
	}
	return items
}

func findToolByProviderID(tools []types.Tool, providerID string) types.Tool {
	for _, t := range tools {
		if t.ProviderID == providerID {
			return t
		}
	}
	return types.Tool{}
}

// decodeExtensionEvent looks up rawEvent's namespaced type in the registry
// and, if a matching extension is registered, decodes it into stream
// chunks. handled reports whether an extension matched.
func decodeExtensionEvent(registry *ExtensionRegistry, rawEvent []byte, state map[string]interface{}) (chunks []*provider.StreamChunk, handled bool, err error) {
	if registry == nil || len(rawEvent) == 0 {
		return nil, false, nil
	}
	var record ExtensionRecord
	if err := json.Unmarshal(rawEvent, &record); err != nil {
		return nil, false, nil
	}
	if !IsExtensionEvent(record) {
		return nil, false, nil
	}
	ext, ok := registry.ByEventType[record.Type()]
	if !ok || ext.DecodeEvent == nil {
		return nil, true, nil
	}
	decoded, err := ext.DecodeEvent(record, state)
	return decoded, true, err
}
