package prompt

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/digitallysavvy/go-ai/pkg/internal/media"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// This file mirrors packages/anthropic/src/convert-to-anthropic-prompt.ts
// (ai@7.0.113). Keep the structure of ConvertToAnthropicPrompt aligned with the
// TypeScript function so future diffs can be ported block by block.

// Anthropic beta flags that the prompt conversion itself can require.
const (
	AnthropicBetaFilesAPI                   = "files-api-2025-04-14"
	AnthropicBetaPDFs                       = "pdfs-2024-09-25"
	AnthropicBetaCompact                    = "compact-2026-09-04"
	AnthropicBetaMidConversationSystem      = "mid-conversation-system-2026-04-07"
	AnthropicBetaMidConversationToolChanges = "mid-conversation-tool-changes-2026-07-01"
	AnthropicBetaMidConversationClearAt     = "mid-conversation-system-clear-at-2026-08-21"
	AnthropicBetaMidConversationOutputCfg   = "mid-conversation-output-config-2026-07-01"
)

// anthropicMaxCacheBreakpoints is the maximum number of cache breakpoints
// Anthropic accepts per request.
const anthropicMaxCacheBreakpoints = 4

// anthropicProviderToolNames maps Anthropic provider tool IDs to the tool
// names used on the wire. Mirrors the providerToolNames table passed to
// createToolNameMapping in anthropic-language-model.ts.
var anthropicProviderToolNames = map[string]string{
	"anthropic.code_execution_20250522":    "code_execution",
	"anthropic.code_execution_20250825":    "code_execution",
	"anthropic.code_execution_20260120":    "code_execution",
	"anthropic.computer_20241022":          "computer",
	"anthropic.computer_20250124":          "computer",
	"anthropic.computer_20251124":          "computer",
	"anthropic.computer_toolset_20260801":  "computer",
	"anthropic.text_editor_20241022":       "str_replace_editor",
	"anthropic.text_editor_20250124":       "str_replace_editor",
	"anthropic.text_editor_20250429":       "str_replace_based_edit_tool",
	"anthropic.text_editor_20250728":       "str_replace_based_edit_tool",
	"anthropic.bash_20241022":              "bash",
	"anthropic.bash_20250124":              "bash",
	"anthropic.memory_20250818":            "memory",
	"anthropic.web_search_20250305":        "web_search",
	"anthropic.web_search_20260209":        "web_search",
	"anthropic.web_search_20260318":        "web_search",
	"anthropic.web_fetch_20250910":         "web_fetch",
	"anthropic.web_fetch_20260209":         "web_fetch",
	"anthropic.web_fetch_20260318":         "web_fetch",
	"anthropic.tool_search_regex_20251119": "tool_search_tool_regex",
	"anthropic.tool_search_bm25_20251119":  "tool_search_tool_bm25",
	"anthropic.advisor_20260301":           "advisor",
}

// AnthropicProviderToolName maps a Go SDK tool name to the Anthropic wire tool
// name. In the Go SDK provider tools are named by their provider ID (for
// example "anthropic.web_search_20250305"), so the ID doubles as the custom
// tool name. Unknown names are returned unchanged, like TS
// ToolNameMapping.toProviderToolName.
func AnthropicProviderToolName(toolName string) string {
	if providerName, ok := anthropicProviderToolNames[toolName]; ok {
		return providerName
	}
	return toolName
}

// AnthropicToolsetNames returns the toolset_name for toolset tools passed to
// the request. Mirrors getAnthropicToolsetNames in anthropic-language-model.ts.
func AnthropicToolsetNames(tools []types.Tool) map[string]string {
	out := map[string]string{}
	for _, t := range tools {
		if t.Name == "anthropic.computer_toolset_20260801" {
			out[t.Name] = "computer"
		}
	}
	return out
}

// AnthropicCacheControlValidator mirrors CacheControlValidator from
// get-cache-control.ts: it extracts cache_control from provider options,
// rejects it on non-cacheable blocks and enforces the 4-breakpoint limit.
type AnthropicCacheControlValidator struct {
	breakpointCount int
	warnings        []types.Warning
}

// NewAnthropicCacheControlValidator creates a validator for one request.
func NewAnthropicCacheControlValidator() *AnthropicCacheControlValidator {
	return &AnthropicCacheControlValidator{}
}

// GetCacheControl returns the cache_control value from the anthropic provider
// options (accepting both cacheControl and cache_control), or nil.
func (v *AnthropicCacheControlValidator) GetCacheControl(providerOptions map[string]interface{}, contextType string, canCache bool) interface{} {
	value := anthropicCacheControlFromOptions(providerOptions)
	if value == nil {
		return nil
	}
	if !canCache {
		v.warnings = append(v.warnings, types.Warning{
			Type:    "unsupported",
			Feature: "cache_control on non-cacheable context",
			Details: fmt.Sprintf("cache_control cannot be set on %s. It will be ignored.", contextType),
		})
		return nil
	}
	v.breakpointCount++
	if v.breakpointCount > anthropicMaxCacheBreakpoints {
		v.warnings = append(v.warnings, types.Warning{
			Type:    "unsupported",
			Feature: "cacheControl breakpoint limit",
			Details: fmt.Sprintf("Maximum %d cache breakpoints exceeded (found %d). This breakpoint will be ignored.", anthropicMaxCacheBreakpoints, v.breakpointCount),
		})
		return nil
	}
	return value
}

// Warnings returns the cache-control warnings collected so far.
func (v *AnthropicCacheControlValidator) Warnings() []types.Warning {
	return v.warnings
}

func anthropicCacheControlFromOptions(options map[string]interface{}) interface{} {
	anthropicOpts := anthropicOptions(options)
	if anthropicOpts == nil {
		return nil
	}
	if cacheControl, ok := anthropicOpts["cacheControl"]; ok && cacheControl != nil {
		return cacheControl
	}
	if cacheControl, ok := anthropicOpts["cache_control"]; ok && cacheControl != nil {
		return cacheControl
	}
	return nil
}

// AnthropicPromptOptions configures ConvertToAnthropicPrompt.
type AnthropicPromptOptions struct {
	// SendReasoning controls whether reasoning parts are sent back as
	// thinking / redacted_thinking blocks. nil means true (TS default).
	SendReasoning *bool

	// ToProviderToolName maps a custom tool name to the Anthropic tool name.
	// nil uses AnthropicProviderToolName.
	ToProviderToolName func(toolName string) string

	// ToolsetNames maps custom tool names of toolset tools to the Anthropic
	// toolset_name (see AnthropicToolsetNames).
	ToolsetNames map[string]string

	// CacheControlValidator may be shared with tool preparation so the
	// breakpoint limit spans the whole request. nil creates a fresh one.
	CacheControlValidator *AnthropicCacheControlValidator
}

// AnthropicPrompt is the converted Anthropic Messages API prompt.
type AnthropicPrompt struct {
	// System holds the top-level system text blocks (nil when absent).
	System []map[string]interface{}

	// Messages holds the converted messages.
	Messages []map[string]interface{}

	// Betas lists the anthropic-beta flags required by the prompt, in first-use order.
	Betas []string

	// Warnings holds conversion warnings, including cache-control warnings
	// from the validator.
	Warnings []types.Warning
}

type anthropicBlock struct {
	role     types.MessageRole // system, assistant, or user (user+tool)
	messages []types.Message
}

// groupAnthropicBlocks mirrors groupIntoBlocks: consecutive system, assistant,
// and user/tool messages are grouped into one block each.
func groupAnthropicBlocks(messages []types.Message) []anthropicBlock {
	var blocks []anthropicBlock
	for _, msg := range messages {
		role := msg.Role
		if role == types.RoleTool {
			role = types.RoleUser
		}
		if len(blocks) == 0 || blocks[len(blocks)-1].role != role {
			blocks = append(blocks, anthropicBlock{role: role})
		}
		blocks[len(blocks)-1].messages = append(blocks[len(blocks)-1].messages, msg)
	}
	return blocks
}

type anthropicConverter struct {
	opts      AnthropicPromptOptions
	validator *AnthropicCacheControlValidator
	betas     []string
	betaSet   map[string]bool
	warnings  []types.Warning
}

func (c *anthropicConverter) addBeta(beta string) {
	if c.betaSet[beta] {
		return
	}
	c.betaSet[beta] = true
	c.betas = append(c.betas, beta)
}

func (c *anthropicConverter) warn(message string) {
	c.warnings = append(c.warnings, types.Warning{Type: "other", Message: message})
}

func (c *anthropicConverter) toProviderToolName(name string) string {
	if c.opts.ToProviderToolName != nil {
		return c.opts.ToProviderToolName(name)
	}
	return AnthropicProviderToolName(name)
}

// ConvertToAnthropicPrompt converts SDK messages to the Anthropic Messages API
// prompt shape. It mirrors convertToAnthropicPrompt from the TypeScript SDK:
//
//   - consecutive user and tool messages are merged into one "user" message
//     whose tool results become tool_result blocks;
//   - consecutive assistant messages are merged; tool calls become tool_use,
//     server_tool_use or mcp_tool_use blocks, provider-executed tool results
//     become their *_tool_result blocks, and reasoning becomes thinking /
//     redacted_thinking;
//   - the first system block becomes the top-level system prompt, later ones
//     become inline system messages (mid-conversation system beta);
//   - cache_control is read from part and message provider options.
func ConvertToAnthropicPrompt(messages []types.Message, opts AnthropicPromptOptions) (*AnthropicPrompt, error) {
	c := &anthropicConverter{
		opts:      opts,
		validator: opts.CacheControlValidator,
		betaSet:   map[string]bool{},
	}
	if c.validator == nil {
		c.validator = NewAnthropicCacheControlValidator()
	}
	sendReasoning := opts.SendReasoning == nil || *opts.SendReasoning

	blocks := groupAnthropicBlocks(MergeConsecutiveToolMessages(messages))
	var system []map[string]interface{}
	out := make([]map[string]interface{}, 0, len(blocks))
	// lastUserMessageIndex tracks the out[] index of the last combined
	// user-block message that contains at least one genuine (non-tool-result)
	// user message with non-empty content -- used below to normalize dangling
	// programmatic caller metadata only in history BEFORE that point (TS
	// commit 8373a22603, #21623).
	lastUserMessageIndex := -1

	for i, block := range blocks {
		isLastBlock := i == len(blocks)-1
		switch block.role {
		case types.RoleSystem:
			c.convertSystemBlock(block, i, &system, &out)

		case types.RoleAssistant:
			content, err := c.convertAssistantBlock(block, isLastBlock, sendReasoning)
			if err != nil {
				return nil, err
			}
			if len(content) > 0 {
				out = append(out, map[string]interface{}{
					"role":    "assistant",
					"content": moveToolUseBlocksToEnd(content),
				})
			}

		default:
			// A tool-result sub-message does not end a turn -- only an
			// actual non-empty user message does -- so lastUserMessageIndex
			// is set from the original per-message roles inside this
			// (already tool+user merged) block, not from the block itself.
			for _, msg := range block.messages {
				if msg.Role != types.RoleTool && len(msg.Content) > 0 {
					lastUserMessageIndex = len(out)
					break
				}
			}
			content, err := c.convertUserBlock(block)
			if err != nil {
				return nil, err
			}
			out = append(out, map[string]interface{}{
				"role":    "user",
				"content": content,
			})
		}
	}

	normalizeOrphanedAnthropicCallers(out, lastUserMessageIndex, c)

	warnings := append(c.warnings, c.validator.Warnings()...)
	if opts.CacheControlValidator != nil {
		// Shared validator: the caller collects its warnings once.
		warnings = c.warnings
	}

	return &AnthropicPrompt{
		System:   system,
		Messages: out,
		Betas:    c.betas,
		Warnings: warnings,
	}, nil
}

// normalizeOrphanedAnthropicCallers omits dangling programmatic `caller`
// metadata (added by convertAssistantBlock via anthropicCaller) from
// converted assistant messages before lastUserMessageIndex, mirroring TS
// commit 8373a22603 (#21623). Pruning a multi-step conversation can remove a
// code execution source call while retaining a dependent tool call that
// still references it via caller.tool_id; Anthropic then rejects the next
// request with "source tool ... not found for tool use block ...". This
// retains the tool calls, results, ordering, and cache controls, and emits
// one warning per affected tool call -- it does not restore deleted
// execution history, so a historical call with a missing source is simply
// replayed without programmatic provenance.
//
// Only history strictly before lastUserMessageIndex is normalized: a
// tool-result message does not end a turn, so callers in an active
// continuation (not yet followed by a genuine user message) must keep their
// source intact so Anthropic can resume it. lastUserMessageIndex == -1
// (no genuine user message in the prompt) means nothing is normalized.
func normalizeOrphanedAnthropicCallers(out []map[string]interface{}, lastUserMessageIndex int, c *anthropicConverter) {
	// Resolve source IDs against actual emitted code execution
	// server_tool_use blocks (including custom tool names already resolved
	// to "code_execution" during conversion).
	codeExecutionToolCallIDs := map[string]bool{}
	for _, m := range out {
		if m["role"] != "assistant" {
			continue
		}
		contentParts, _ := m["content"].([]map[string]interface{})
		for _, part := range contentParts {
			if part["type"] == "server_tool_use" && part["name"] == "code_execution" {
				if id, ok := part["id"].(string); ok {
					codeExecutionToolCallIDs[id] = true
				}
			}
		}
	}

	warnedToolCallIDs := map[string]bool{}
	limit := lastUserMessageIndex
	if limit > len(out) {
		limit = len(out)
	}
	for i := 0; i < limit; i++ {
		m := out[i]
		if m["role"] != "assistant" {
			continue
		}
		contentParts, _ := m["content"].([]map[string]interface{})
		for _, part := range contentParts {
			callerRaw, hasCaller := part["caller"]
			if !hasCaller {
				continue
			}
			caller, ok := callerRaw.(map[string]interface{})
			if !ok || caller == nil {
				continue
			}
			callerType, _ := caller["type"].(string)
			toolID, _ := caller["tool_id"].(string)
			if callerType == "direct" || codeExecutionToolCallIDs[toolID] {
				continue
			}

			toolCallID, _ := part["id"].(string)
			if toolCallID == "" {
				toolCallID, _ = part["tool_use_id"].(string)
			}
			delete(part, "caller")
			if !warnedToolCallIDs[toolCallID] {
				warnedToolCallIDs[toolCallID] = true
				c.warn(fmt.Sprintf("Omitted caller metadata for tool %s because source code execution tool %s is missing from the conversation history.", toolCallID, toolID))
			}
		}
	}
}

// ToAnthropicMessages converts messages to Anthropic wire messages using the
// default options. System messages are converted as in ConvertToAnthropicPrompt
// but only the messages list is returned; use ConvertToAnthropicPrompt to also
// get the system blocks, required betas, warnings and conversion errors.
// Parts that cannot be converted cause the whole conversion to fall back to an
// empty list.
func ToAnthropicMessages(messages []types.Message) []map[string]interface{} {
	converted, err := ConvertToAnthropicPrompt(messages, AnthropicPromptOptions{})
	if err != nil {
		return []map[string]interface{}{}
	}
	return converted.Messages
}

// ── system ───────────────────────────────────────────────────────────────────

type anthropicSystemMessage struct {
	content         []map[string]interface{}
	clearAt         interface{}
	effort          interface{}
	toolChangeCount int
}

func (c *anthropicConverter) convertSystemBlock(block anthropicBlock, index int, system *[]map[string]interface{}, out *[]map[string]interface{}) {
	converted := make([]anthropicSystemMessage, 0, len(block.messages))
	for _, msg := range block.messages {
		text := messageText(msg)
		sysOpts := anthropicOptions(msg.ProviderOptions)
		toolChanges := interfaceSlice(sysOpts["toolChanges"])
		clearAt := sysOpts["clearAt"]
		effort := sysOpts["effort"]

		content := make([]map[string]interface{}, 0, 1+len(toolChanges))
		if text != "" || (len(toolChanges) == 0 && clearAt == nil && effort == nil) {
			textBlock := map[string]interface{}{"type": "text", "text": text}
			setIfNotNil(textBlock, "cache_control", c.validator.GetCacheControl(msg.ProviderOptions, "system message", true))
			content = append(content, textBlock)
		}
		for _, change := range toolChanges {
			m, ok := change.(map[string]interface{})
			if !ok {
				continue
			}
			toolName, _ := m["toolName"].(string)
			content = append(content, map[string]interface{}{
				"type": m["type"],
				"tool": map[string]interface{}{
					"type": "tool_reference",
					"name": c.toProviderToolName(toolName),
				},
			})
		}
		converted = append(converted, anthropicSystemMessage{
			content:         content,
			clearAt:         clearAt,
			effort:          effort,
			toolChangeCount: len(toolChanges),
		})
	}

	toolChangeCount := 0
	hasInlineSystemOptions := false
	for _, m := range converted {
		toolChangeCount += m.toolChangeCount
		if m.clearAt != nil || m.effort != nil {
			hasInlineSystemOptions = true
		}
	}

	if index == 0 || (*system == nil && toolChangeCount == 0 && !hasInlineSystemOptions) {
		if toolChangeCount > 0 {
			c.warn("tool changes on the initial system message are not supported by Anthropic. " +
				"Configure the initial tool set via the tools option instead. " +
				"The tool changes have been ignored.")
		}
		// Initial instruction text goes in the top-level system field.
		// Effort-only messages (empty content, no clearAt) stay in the
		// messages array as inline configuration_update-style entries
		// instead of being silently dropped.
		for _, m := range converted {
			if len(m.content) == 0 && m.clearAt == nil && m.effort != nil {
				*out = append(*out, map[string]interface{}{
					"role":          "system",
					"content":       m.content,
					"output_config": map[string]interface{}{"effort": m.effort},
				})
				c.addBeta(AnthropicBetaMidConversationOutputCfg)
			} else if m.clearAt != nil || m.effort != nil {
				c.warn("clearAt and effort on this initial system message are not supported by Anthropic. " +
					"Use a separate effort-only system message with empty content to set effort. " +
					"These options have been ignored.")
			}
		}
		sys := make([]map[string]interface{}, 0, len(converted))
		for _, m := range converted {
			for _, part := range m.content {
				if part["type"] == "text" {
					sys = append(sys, part)
				}
			}
		}
		*system = sys
		return
	}

	c.addBeta(AnthropicBetaMidConversationSystem)
	for _, m := range converted {
		msg := map[string]interface{}{"role": "system", "content": m.content}
		if m.clearAt != nil {
			msg["clear_at"] = m.clearAt
		}
		if m.effort != nil {
			msg["output_config"] = map[string]interface{}{"effort": m.effort}
		}
		*out = append(*out, msg)
		if m.toolChangeCount > 0 {
			c.addBeta(AnthropicBetaMidConversationToolChanges)
		}
		if m.clearAt != nil {
			c.addBeta(AnthropicBetaMidConversationClearAt)
		}
		if m.effort != nil {
			c.addBeta(AnthropicBetaMidConversationOutputCfg)
		}
	}
}

func messageText(msg types.Message) string {
	var b strings.Builder
	for _, part := range msg.Content {
		switch p := part.(type) {
		case types.TextContent:
			b.WriteString(p.Text)
		case *types.TextContent:
			if p != nil {
				b.WriteString(p.Text)
			}
		}
	}
	return b.String()
}

// ── user / tool ──────────────────────────────────────────────────────────────

func (c *anthropicConverter) convertUserBlock(block anthropicBlock) ([]map[string]interface{}, error) {
	content := make([]map[string]interface{}, 0)
	for _, msg := range block.messages {
		if msg.Role == types.RoleTool {
			for j, part := range msg.Content {
				isLastPart := j == len(msg.Content)-1
				var toolResult types.ToolResultContent
				switch p := part.(type) {
				case types.ToolResultContent:
					toolResult = p
				case *types.ToolResultContent:
					if p == nil {
						continue
					}
					toolResult = *p
				default:
					// tool-approval-response parts (and anything else) are skipped.
					continue
				}
				converted, err := c.convertToolResult(msg, toolResult, isLastPart)
				if err != nil {
					return nil, err
				}
				content = append(content, converted)
			}
			continue
		}

		for j, part := range msg.Content {
			isLastPart := j == len(msg.Content)-1
			cacheControl := func(partOptions map[string]interface{}) interface{} {
				if cc := c.validator.GetCacheControl(partOptions, "user message part", true); cc != nil {
					return cc
				}
				if isLastPart {
					return c.validator.GetCacheControl(msg.ProviderOptions, "user message", true)
				}
				return nil
			}

			switch p := part.(type) {
			case types.TextContent:
				block := map[string]interface{}{"type": "text", "text": p.Text}
				setIfNotNil(block, "cache_control", cacheControl(p.ProviderOptions))
				content = append(content, block)
			case *types.TextContent:
				if p == nil {
					continue
				}
				block := map[string]interface{}{"type": "text", "text": p.Text}
				setIfNotNil(block, "cache_control", cacheControl(p.ProviderOptions))
				content = append(content, block)
			case types.ImageContent:
				block, err := c.convertUserFile(imageAsFile(p), cacheControl(p.ProviderOptions))
				if err != nil {
					return nil, err
				}
				content = append(content, block)
			case types.FileContent:
				block, err := c.convertUserFile(p, cacheControl(p.ProviderOptions))
				if err != nil {
					return nil, err
				}
				content = append(content, block)
			case *types.FileContent:
				if p == nil {
					continue
				}
				block, err := c.convertUserFile(*p, cacheControl(p.ProviderOptions))
				if err != nil {
					return nil, err
				}
				content = append(content, block)
			}
		}
	}
	return content, nil
}

func imageAsFile(p types.ImageContent) types.FileContent {
	mediaType := firstNonEmpty(p.MimeType, "image")
	file := types.FileContent{MediaType: mediaType, ProviderOptions: p.ProviderOptions}
	if p.URL != "" {
		file.URL = p.URL
	} else {
		file.Data = p.Image
	}
	return file
}

// anthropicFile is the resolved tagged form of a file part (TS part.data).
type anthropicFile struct {
	kind      types.FileDataType
	data      []byte
	url       string
	reference types.ProviderReference
	text      string
	mediaType string
	filename  string
}

func resolveAnthropicFile(fileData types.FileData, data []byte, url, reference, text, mediaType, filename string) (anthropicFile, error) {
	f := anthropicFile{
		mediaType: firstNonEmpty(mediaType, fileData.MediaType),
		filename:  filename,
	}
	switch {
	case fileData.Type != "":
		f.kind = fileData.Type
		f.data = fileData.Data
		if len(f.data) == 0 && fileData.DataString != "" {
			decoded, err := types.DecodeFileDataString(fileData.DataString)
			if err != nil {
				return f, err
			}
			f.data = decoded
		}
		f.url = fileData.URL
		f.reference = fileData.Reference
		f.text = fileData.Text
	case len(data) > 0:
		f.kind = types.FileDataTypeData
		f.data = data
	case url != "":
		f.kind = types.FileDataTypeURL
		f.url = url
	case reference != "":
		f.kind = types.FileDataTypeReference
		f.reference = types.ProviderReference{"anthropic": reference}
	case text != "":
		f.kind = types.FileDataTypeText
		f.text = text
	default:
		f.kind = types.FileDataTypeData
	}
	if f.kind == types.FileDataTypeURL && strings.HasPrefix(f.url, "data:") {
		parsed, err := ParseDataURL(f.url)
		if err != nil {
			return f, err
		}
		f.kind = types.FileDataTypeData
		f.data = parsed.Data
		f.url = ""
		if f.mediaType == "" {
			f.mediaType = parsed.MediaType
		}
	}
	return f, nil
}

func topLevelMediaType(mediaType string) string {
	if i := strings.Index(mediaType, "/"); i >= 0 {
		return mediaType[:i]
	}
	return mediaType
}

// isFullAnthropicMediaType mirrors isFullMediaType from provider-utils.
func isFullAnthropicMediaType(mediaType string) bool {
	i := strings.Index(mediaType, "/")
	if i == -1 {
		return false
	}
	subtype := mediaType[i+1:]
	return subtype != "" && subtype != "*"
}

// resolveFullMediaType mirrors resolveFullMediaType from provider-utils.
func (f anthropicFile) resolveFullMediaType() (string, error) {
	if isFullAnthropicMediaType(f.mediaType) {
		return f.mediaType, nil
	}
	if f.kind == types.FileDataTypeData {
		if detected := detectAnthropicMediaType(f.data, topLevelMediaType(f.mediaType)); detected != "" {
			return detected, nil
		}
		return "", &providererrors.UnsupportedFunctionalityError{
			Functionality: fmt.Sprintf("file of media type %q must specify subtype since it could not be auto-detected", f.mediaType),
		}
	}
	return "", &providererrors.UnsupportedFunctionalityError{
		Functionality: fmt.Sprintf("file of media type %q must specify subtype since it is not passed as inline bytes", f.mediaType),
	}
}

func detectAnthropicMediaType(data []byte, topLevel string) string {
	isPDF := len(data) >= 4 && string(data[:4]) == "%PDF"
	switch topLevel {
	case "image":
		if len(data) < 3 {
			return ""
		}
		return media.DetectImageMediaType(data)
	case "application":
		if isPDF {
			return "application/pdf"
		}
	case "":
		if isPDF {
			return "application/pdf"
		}
	}
	return ""
}

func (c *anthropicConverter) documentMetadata(providerOptions map[string]interface{}, filename string, block map[string]interface{}) {
	opts := anthropicOptions(providerOptions)
	title, _ := opts["title"].(string)
	if title == "" {
		title = filename
	}
	if title != "" {
		block["title"] = title
	}
	if context, _ := opts["context"].(string); context != "" {
		block["context"] = context
	}
	if citations, ok := opts["citations"].(map[string]interface{}); ok {
		if enabled, _ := citations["enabled"].(bool); enabled {
			block["citations"] = map[string]interface{}{"enabled": true}
		}
	}
}

func (c *anthropicConverter) convertUserFile(p types.FileContent, cacheControl interface{}) (map[string]interface{}, error) {
	f, err := resolveAnthropicFile(p.FileData, p.Data, p.URL, p.Reference, p.Text, firstNonEmpty(p.MediaType, p.MimeType), p.Filename)
	if err != nil {
		return nil, err
	}
	var block map[string]interface{}

	switch f.kind {
	case types.FileDataTypeReference:
		fileID, err := providerutils.ResolveProviderReference(f.reference, "anthropic")
		if err != nil {
			return nil, err
		}
		c.addBeta(AnthropicBetaFilesAPI)
		if containerUpload, _ := anthropicOptions(p.ProviderOptions)["containerUpload"].(bool); containerUpload {
			return map[string]interface{}{"type": "container_upload", "file_id": fileID}, nil
		}
		blockType := "document"
		if topLevelMediaType(f.mediaType) == "image" {
			blockType = "image"
		}
		block = map[string]interface{}{
			"type":   blockType,
			"source": map[string]interface{}{"type": "file", "file_id": fileID},
		}

	case types.FileDataTypeText:
		block = map[string]interface{}{
			"type": "document",
			"source": map[string]interface{}{
				"type":       "text",
				"media_type": "text/plain",
				"data":       f.text,
			},
		}
		c.documentMetadata(p.ProviderOptions, f.filename, block)

	default: // url, data
		topLevel := topLevelMediaType(f.mediaType)
		isURL := f.kind == types.FileDataTypeURL
		switch {
		case topLevel == "image":
			var source map[string]interface{}
			if isURL {
				source = map[string]interface{}{"type": "url", "url": f.url}
			} else {
				mediaType, err := f.resolveFullMediaType()
				if err != nil {
					return nil, err
				}
				source = map[string]interface{}{
					"type":       "base64",
					"media_type": mediaType,
					"data":       base64.StdEncoding.EncodeToString(f.data),
				}
			}
			block = map[string]interface{}{"type": "image", "source": source}

		case topLevel == "application" && c.isPDF(f):
			c.addBeta(AnthropicBetaPDFs)
			var source map[string]interface{}
			if isURL {
				source = map[string]interface{}{"type": "url", "url": f.url}
			} else {
				source = map[string]interface{}{
					"type":       "base64",
					"media_type": "application/pdf",
					"data":       base64.StdEncoding.EncodeToString(f.data),
				}
			}
			block = map[string]interface{}{"type": "document", "source": source}
			c.documentMetadata(p.ProviderOptions, f.filename, block)

		case f.mediaType == "text/plain":
			var source map[string]interface{}
			if isURL {
				source = map[string]interface{}{"type": "url", "url": f.url}
			} else {
				source = map[string]interface{}{
					"type":       "text",
					"media_type": "text/plain",
					"data":       string(f.data),
				}
			}
			block = map[string]interface{}{"type": "document", "source": source}
			c.documentMetadata(p.ProviderOptions, f.filename, block)

		default:
			return nil, &providererrors.UnsupportedFunctionalityError{
				Functionality: fmt.Sprintf("media type: %s", f.mediaType),
			}
		}
	}

	setIfNotNil(block, "cache_control", cacheControl)
	return block, nil
}

// isPDF mirrors the TS check: URL parts compare the declared media type, data
// parts compare the resolved (detected) media type.
func (c *anthropicConverter) isPDF(f anthropicFile) bool {
	if f.kind == types.FileDataTypeURL {
		return f.mediaType == "application/pdf"
	}
	mediaType, err := f.resolveFullMediaType()
	return err == nil && mediaType == "application/pdf"
}

// anthropicToolOutput is the TS LanguageModelV4ToolResultOutput resolved from
// the Go ToolResultContent (Output, or the legacy Result / Error fields).
type anthropicToolOutput struct {
	kind            string // text, json, error-text, error-json, content, execution-denied
	value           interface{}
	content         []types.ToolResultContentBlock
	reason          string
	providerOptions map[string]interface{}
}

func resolveToolOutput(p types.ToolResultContent) anthropicToolOutput {
	if p.Output == nil {
		if p.Error != "" {
			return anthropicToolOutput{kind: "error-text", value: p.Error}
		}
		switch v := p.Result.(type) {
		case string:
			return anthropicToolOutput{kind: "text", value: v}
		case types.ToolResultOutput:
			return resolveToolOutput(types.ToolResultContent{Output: &v})
		case *types.ToolResultOutput:
			if v != nil {
				return resolveToolOutput(types.ToolResultContent{Output: v})
			}
		}
		return anthropicToolOutput{kind: "json", value: p.Result}
	}
	o := p.Output
	out := anthropicToolOutput{value: o.Value, content: o.Content, reason: o.Reason, providerOptions: o.ProviderOptions}
	switch o.Type {
	case types.ToolResultOutputError:
		if _, ok := o.Value.(string); ok {
			out.kind = "error-text"
		} else {
			out.kind = "error-json"
		}
	case "":
		out.kind = "json"
	default:
		out.kind = string(o.Type)
	}
	return out
}

func (o anthropicToolOutput) isError() bool {
	return o.kind == "error-text" || o.kind == "error-json"
}

func (c *anthropicConverter) convertToolResult(msg types.Message, part types.ToolResultContent, isLastPart bool) (map[string]interface{}, error) {
	output := resolveToolOutput(part)

	outputProviderOptions := output.providerOptions
	if outputProviderOptions == nil && output.kind == "content" {
		for _, block := range output.content {
			if opts := toolResultBlockProviderOptions(block); opts != nil {
				outputProviderOptions = opts
				break
			}
		}
	}

	cacheControl := c.validator.GetCacheControl(part.ProviderOptions, "tool result part", true)
	if cacheControl == nil {
		cacheControl = c.validator.GetCacheControl(outputProviderOptions, "tool result output", true)
	}
	if cacheControl == nil && isLastPart {
		cacheControl = c.validator.GetCacheControl(msg.ProviderOptions, "tool result message", true)
	}

	var contentValue interface{}
	switch output.kind {
	case "content":
		blocks := make([]map[string]interface{}, 0, len(output.content))
		for _, block := range output.content {
			converted, err := c.convertToolResultContentBlock(block)
			if err != nil {
				return nil, err
			}
			if converted != nil {
				blocks = append(blocks, converted)
			}
		}
		contentValue = blocks
	case "text", "error-text":
		contentValue = stringValue(output.value)
	case "execution-denied":
		if output.reason != "" {
			contentValue = output.reason
		} else {
			contentValue = "Tool call execution denied."
		}
	default: // json, error-json
		contentValue = jsonStringify(output.value)
	}

	result := map[string]interface{}{
		"type":        "tool_result",
		"tool_use_id": part.ToolCallID,
	}
	if toolsetName := c.toolsetName(part.ToolName, part.ProviderOptions); toolsetName != "" {
		result["toolset_name"] = toolsetName
	}
	result["content"] = contentValue
	if output.isError() {
		result["is_error"] = true
	}
	setIfNotNil(result, "cache_control", cacheControl)
	return result, nil
}

func toolResultBlockProviderOptions(block types.ToolResultContentBlock) map[string]interface{} {
	switch b := block.(type) {
	case types.TextContentBlock:
		return b.ProviderOptions
	case types.ImageContentBlock:
		return b.ProviderOptions
	case types.FileContentBlock:
		return b.ProviderOptions
	case types.CustomContentBlock:
		return b.ProviderOptions
	case *types.TextContentBlock:
		if b != nil {
			return b.ProviderOptions
		}
	case *types.ImageContentBlock:
		if b != nil {
			return b.ProviderOptions
		}
	case *types.FileContentBlock:
		if b != nil {
			return b.ProviderOptions
		}
	case *types.CustomContentBlock:
		if b != nil {
			return b.ProviderOptions
		}
	}
	return nil
}

func (c *anthropicConverter) convertToolResultContentBlock(block types.ToolResultContentBlock) (map[string]interface{}, error) {
	switch b := block.(type) {
	case *types.TextContentBlock:
		if b == nil {
			return nil, nil
		}
		return c.convertToolResultContentBlock(*b)
	case *types.ImageContentBlock:
		if b == nil {
			return nil, nil
		}
		return c.convertToolResultContentBlock(*b)
	case *types.FileContentBlock:
		if b == nil {
			return nil, nil
		}
		return c.convertToolResultContentBlock(*b)
	case *types.CustomContentBlock:
		if b == nil {
			return nil, nil
		}
		return c.convertToolResultContentBlock(*b)

	case types.TextContentBlock:
		return map[string]interface{}{"type": "text", "text": b.Text}, nil

	case types.ImageContentBlock:
		f, err := resolveAnthropicFile(types.FileData{}, b.Data, "", "", "", firstNonEmpty(b.MediaType, "image"), "")
		if err != nil {
			return nil, err
		}
		return c.convertToolResultFile(f)

	case types.FileContentBlock:
		f, err := resolveAnthropicFile(b.FileData, b.Data, b.URL, b.Reference, b.Text, b.MediaType, b.Filename)
		if err != nil {
			return nil, err
		}
		return c.convertToolResultFile(f)

	case types.CustomContentBlock:
		opts := anthropicOptions(b.ProviderOptions)
		if opts["type"] == "tool-reference" {
			return map[string]interface{}{"type": "tool_reference", "tool_name": opts["toolName"]}, nil
		}
		c.warn("unsupported custom tool content part")
		return nil, nil

	default:
		c.warn(fmt.Sprintf("unsupported tool content part type: %s", block.ToolResultContentType()))
		return nil, nil
	}
}

func (c *anthropicConverter) convertToolResultFile(f anthropicFile) (map[string]interface{}, error) {
	topLevel := topLevelMediaType(f.mediaType)
	switch f.kind {
	case types.FileDataTypeURL:
		if topLevel == "image" {
			return map[string]interface{}{"type": "image", "source": map[string]interface{}{"type": "url", "url": f.url}}, nil
		}
		return map[string]interface{}{"type": "document", "source": map[string]interface{}{"type": "url", "url": f.url}}, nil
	case types.FileDataTypeData:
		if topLevel == "image" {
			mediaType, err := f.resolveFullMediaType()
			if err != nil {
				return nil, err
			}
			return map[string]interface{}{"type": "image", "source": map[string]interface{}{
				"type":       "base64",
				"media_type": mediaType,
				"data":       base64.StdEncoding.EncodeToString(f.data),
			}}, nil
		}
		mediaType, err := f.resolveFullMediaType()
		if err != nil {
			return nil, err
		}
		if mediaType == "application/pdf" {
			c.addBeta(AnthropicBetaPDFs)
			return map[string]interface{}{"type": "document", "source": map[string]interface{}{
				"type":       "base64",
				"media_type": "application/pdf",
				"data":       base64.StdEncoding.EncodeToString(f.data),
			}}, nil
		}
		c.warn(fmt.Sprintf("unsupported tool content part type: file with media type: %s", f.mediaType))
		return nil, nil
	default:
		c.warn(fmt.Sprintf("unsupported tool content part type: file with data type: %s", f.kind))
		return nil, nil
	}
}

// ── assistant ────────────────────────────────────────────────────────────────

// anthropicToolCall is the unified view of a tool-call part, from either a
// ToolCallContent part or the legacy Message.ToolCalls field.
type anthropicToolCall struct {
	id               string
	toolName         string
	input            interface{}
	providerExecuted bool
	providerOptions  map[string]interface{}
}

func toolCallFromContent(p types.ToolCallContent) anthropicToolCall {
	var input interface{}
	if p.Arguments != nil {
		input = p.Arguments
	} else if p.Input != "" {
		if err := json.Unmarshal([]byte(p.Input), &input); err != nil {
			input = p.Input
		}
	}
	opts := p.ProviderOptions
	if opts == nil {
		opts = rawMetadataMap(p.ProviderMetadata)
	}
	return anthropicToolCall{
		id:               p.ToolCallID,
		toolName:         p.ToolName,
		input:            input,
		providerExecuted: p.ProviderExecuted,
		providerOptions:  opts,
	}
}

func toolCallFromLegacy(p types.ToolCall) anthropicToolCall {
	var input interface{}
	if p.Arguments != nil {
		input = p.Arguments
	} else if p.RawArguments != "" {
		if err := json.Unmarshal([]byte(p.RawArguments), &input); err != nil {
			input = p.RawArguments
		}
	}
	return anthropicToolCall{
		id:               p.ID,
		toolName:         p.ToolName,
		input:            input,
		providerExecuted: p.ProviderExecuted,
		providerOptions:  p.ProviderMetadata,
	}
}

// assistantParts returns the message content with legacy Message.ToolCalls
// that are not already present as ToolCallContent parts appended at the end.
func assistantParts(msg types.Message) []types.ContentPart {
	if len(msg.ToolCalls) == 0 {
		return msg.Content
	}
	seen := map[string]bool{}
	for _, part := range msg.Content {
		switch p := part.(type) {
		case types.ToolCallContent:
			seen[p.ToolCallID] = true
		case *types.ToolCallContent:
			if p != nil {
				seen[p.ToolCallID] = true
			}
		}
	}
	parts := make([]types.ContentPart, 0, len(msg.Content)+len(msg.ToolCalls))
	parts = append(parts, msg.Content...)
	for _, tc := range msg.ToolCalls {
		if seen[tc.ID] {
			continue
		}
		parts = append(parts, legacyToolCallPart{tc})
	}
	return parts
}

// legacyToolCallPart adapts a Message.ToolCalls entry to a content part.
type legacyToolCallPart struct{ call types.ToolCall }

func (legacyToolCallPart) ContentType() string { return "tool-call" }

func (c *anthropicConverter) convertAssistantBlock(block anthropicBlock, isLastBlock, sendReasoning bool) ([]map[string]interface{}, error) {
	content := make([]map[string]interface{}, 0)
	mcpToolUseIDs := map[string]bool{}

	for j, msg := range block.messages {
		isLastMessage := j == len(block.messages)-1
		parts := assistantParts(msg)

		for k, part := range parts {
			isLastContentPart := k == len(parts)-1
			cacheControl := func(partOptions map[string]interface{}) interface{} {
				if cc := c.validator.GetCacheControl(partOptions, "assistant message part", true); cc != nil {
					return cc
				}
				if isLastContentPart {
					return c.validator.GetCacheControl(msg.ProviderOptions, "assistant message", true)
				}
				return nil
			}

			switch p := part.(type) {
			case *types.TextContent:
				if p != nil {
					c.convertAssistantText(&content, *p, cacheControl, isLastBlock && isLastMessage && isLastContentPart)
				}
			case types.TextContent:
				c.convertAssistantText(&content, p, cacheControl, isLastBlock && isLastMessage && isLastContentPart)

			case *types.ReasoningContent:
				if p != nil {
					c.convertReasoning(&content, *p, sendReasoning)
				}
			case types.ReasoningContent:
				c.convertReasoning(&content, p, sendReasoning)

			case types.ToolCallContent:
				tc := toolCallFromContent(p)
				c.convertToolCall(&content, tc, cacheControl(tc.providerOptions), mcpToolUseIDs)
			case *types.ToolCallContent:
				if p != nil {
					tc := toolCallFromContent(*p)
					c.convertToolCall(&content, tc, cacheControl(tc.providerOptions), mcpToolUseIDs)
				}
			case legacyToolCallPart:
				tc := toolCallFromLegacy(p.call)
				c.convertToolCall(&content, tc, cacheControl(tc.providerOptions), mcpToolUseIDs)

			case types.ToolResultContent:
				if err := c.convertProviderToolResult(&content, p, cacheControl(p.ProviderOptions), mcpToolUseIDs); err != nil {
					return nil, err
				}
			case *types.ToolResultContent:
				if p != nil {
					if err := c.convertProviderToolResult(&content, *p, cacheControl(p.ProviderOptions), mcpToolUseIDs); err != nil {
						return nil, err
					}
				}

			case types.CustomContent:
				// Go extension: forward anthropic-keyed custom blocks verbatim,
				// reading from ProviderOptions (caller-constructed) or falling
				// back to ProviderMetadata (round-tripped from a previous
				// response), mirroring convertReasoning's same fallback.
				opts := p.ProviderOptions
				if opts == nil {
					opts = rawMetadataMap(p.ProviderMetadata)
				}
				anthropicOpts, ok := opts["anthropic"].(map[string]interface{})
				if !ok {
					break
				}
				if p.Kind == "anthropic.fallback" {
					// Fallback blocks require from.model and to.model to
					// round-trip; anything else is dropped with a warning
					// rather than sent malformed (TS commit a587f554f7,
					// #21736).
					from, _ := anthropicOpts["from"].(map[string]interface{})
					to, _ := anthropicOpts["to"].(map[string]interface{})
					fromModel, _ := from["model"].(string)
					toModel, _ := to["model"].(string)
					if fromModel == "" || toModel == "" {
						c.warn("anthropic fallback metadata must include from.model and to.model")
						break
					}
					content = append(content, map[string]interface{}{
						"type": "fallback",
						"from": map[string]interface{}{"model": fromModel},
						"to":   map[string]interface{}{"model": toModel},
					})
					break
				}
				forwarded := make(map[string]interface{}, len(anthropicOpts))
				for key, value := range anthropicOpts {
					forwarded[key] = value
				}
				content = append(content, forwarded)
			}
			// file and reasoning-file parts are not sent back to Anthropic.
		}
	}
	return content, nil
}

func (c *anthropicConverter) convertAssistantText(content *[]map[string]interface{}, p types.TextContent, cacheControl func(map[string]interface{}) interface{}, trim bool) {
	textMetadata := anthropicOptions(p.ProviderOptions)
	if textMetadata == nil {
		textMetadata = anthropicOptions(rawMetadataMap(p.ProviderMetadata))
	}

	if textMetadata["type"] == "compaction" {
		if p.Text == "" {
			return
		}
		signature, hasSignature := textMetadata["signature"].(string)
		if hasSignature {
			c.addBeta(AnthropicBetaCompact)
		}
		block := map[string]interface{}{"type": "compaction", "content": p.Text}
		if hasSignature {
			block["signature"] = signature
		}
		setIfNotNil(block, "cache_control", cacheControl(p.ProviderOptions))
		*content = append(*content, block)
		return
	}

	text := p.Text
	if trim {
		// Anthropic rejects trailing whitespace in pre-filled assistant responses.
		text = strings.TrimSpace(text)
	}
	block := map[string]interface{}{"type": "text", "text": text}
	if citations, ok := textMetadata["citations"]; ok && citations != nil {
		block["citations"] = citations
	}
	setIfNotNil(block, "cache_control", cacheControl(p.ProviderOptions))
	*content = append(*content, block)
}

func (c *anthropicConverter) convertReasoning(content *[]map[string]interface{}, p types.ReasoningContent, sendReasoning bool) {
	if !sendReasoning {
		c.warn("sending reasoning content is disabled for this model")
		return
	}
	opts := anthropicOptions(p.ProviderOptions)
	if opts == nil {
		opts = anthropicOptions(rawMetadataMap(p.ProviderMetadata))
	}
	signature := p.Signature
	if signature == "" {
		signature, _ = opts["signature"].(string)
	}
	redactedData := p.RedactedData
	if redactedData == "" {
		redactedData, _ = opts["redactedData"].(string)
	}

	switch {
	case signature != "":
		// Thinking blocks cannot carry cache_control; validate for the warning.
		c.validator.GetCacheControl(p.ProviderOptions, "thinking block", false)
		*content = append(*content, map[string]interface{}{
			"type":      "thinking",
			"thinking":  p.Text,
			"signature": signature,
		})
	case redactedData != "":
		c.validator.GetCacheControl(p.ProviderOptions, "redacted thinking block", false)
		*content = append(*content, map[string]interface{}{
			"type": "redacted_thinking",
			"data": redactedData,
		})
	default:
		c.warn("unsupported reasoning metadata")
	}
}

func (c *anthropicConverter) convertToolCall(content *[]map[string]interface{}, part anthropicToolCall, cacheControl interface{}, mcpToolUseIDs map[string]bool) {
	caller := anthropicCaller(part.providerOptions)
	opts := anthropicOptions(part.providerOptions)

	if part.providerExecuted {
		providerToolName := c.toProviderToolName(part.toolName)
		inputMap, _ := part.input.(map[string]interface{})
		inputType, _ := inputMap["type"].(string)

		serverToolUse := func(name string, input interface{}) {
			block := map[string]interface{}{
				"type":  "server_tool_use",
				"id":    part.id,
				"name":  name,
				"input": inputOrEmpty(input),
			}
			if caller != nil {
				block["caller"] = caller
			}
			setIfNotNil(block, "cache_control", cacheControl)
			*content = append(*content, block)
		}

		switch {
		case opts["type"] == "mcp-tool-use":
			mcpToolUseIDs[part.id] = true
			serverName, ok := opts["serverName"].(string)
			if !ok {
				c.warn("mcp tool use server name is required and must be a string")
				return
			}
			block := map[string]interface{}{
				"type":        "mcp_tool_use",
				"id":          part.id,
				"name":        part.toolName,
				"input":       inputOrEmpty(part.input),
				"server_name": serverName,
			}
			setIfNotNil(block, "cache_control", cacheControl)
			*content = append(*content, block)

		case providerToolName == "code_execution" &&
			(inputType == "bash_code_execution" || inputType == "text_editor_code_execution"):
			// code execution sub-tools: map back to the sub-tool name.
			serverToolUse(inputType, withoutKey(inputMap, "type"))

		case providerToolName == "code_execution" && inputType == "programmatic-tool-call":
			// Strip the synthetic 'programmatic-tool-call' discriminator.
			serverToolUse("code_execution", withoutKey(inputMap, "type"))

		case providerToolName == "code_execution" || providerToolName == "web_fetch" || providerToolName == "web_search",
			providerToolName == "tool_search_tool_regex" || providerToolName == "tool_search_tool_bm25":
			serverToolUse(providerToolName, part.input)

		case providerToolName == "advisor":
			// The advisor server_tool_use.input is always {}.
			serverToolUse("advisor", map[string]interface{}{})

		default:
			c.warn(fmt.Sprintf("provider executed tool call for tool %s is not supported", part.toolName))
		}
		return
	}

	if toolsetName := c.toolsetName(part.toolName, part.providerOptions); toolsetName != "" {
		// toolset member call: the `action` is the member tool name. A
		// malformed call (action missing, null, or not a string -- e.g. the
		// raw input isn't even an object) is preserved rather than dropped,
		// using the toolset name itself as a placeholder `name`, so Anthropic
		// still receives a tool_use block matching any already-converted
		// tool-result/error for this call; otherwise Anthropic rejects the
		// request for a dangling result with no matching call (TS commit
		// c35458eac5, #21875).
		rawInput := toAnthropicToolInput(part.input)
		action, hasAction := rawInput["action"].(string)
		if !hasAction {
			c.warn(fmt.Sprintf("toolset tool call for tool %s is missing the action", part.toolName))
		}
		name := toolsetName
		if hasAction {
			name = action
		}
		block := map[string]interface{}{
			"type":         "tool_use",
			"id":           part.id,
			"name":         name,
			"toolset_name": toolsetName,
			"input":        rawInput,
		}
		if caller != nil {
			block["caller"] = caller
		}
		setIfNotNil(block, "cache_control", cacheControl)
		*content = append(*content, block)
		return
	}

	block := map[string]interface{}{
		"type":  "tool_use",
		"id":    part.id,
		"name":  part.toolName,
		"input": toAnthropicToolInput(part.input),
	}
	if caller != nil {
		block["caller"] = caller
	}
	setIfNotNil(block, "cache_control", cacheControl)
	*content = append(*content, block)
}

func (c *anthropicConverter) convertProviderToolResult(content *[]map[string]interface{}, part types.ToolResultContent, cacheControl interface{}, mcpToolUseIDs map[string]bool) error {
	providerToolName := c.toProviderToolName(part.ToolName)
	caller := anthropicCaller(part.ProviderOptions)
	output := resolveToolOutput(part)
	unsupportedOutput := func() {
		c.warn(fmt.Sprintf("provider executed tool result output type %s for tool %s is not supported", output.kind, part.ToolName))
	}
	push := func(block map[string]interface{}, withCaller bool) {
		if withCaller && caller != nil {
			block["caller"] = caller
		}
		setIfNotNil(block, "cache_control", cacheControl)
		*content = append(*content, block)
	}

	// Mirrors the TS control flow: the mcp branch does not break, so after an
	// mcp_tool_result the tool name is still matched against the provider
	// tools below (and usually ends in the "not supported" warning).
	if mcpToolUseIDs[part.ToolCallID] {
		if output.kind != "json" && output.kind != "error-json" {
			unsupportedOutput()
			return nil
		}
		push(map[string]interface{}{
			"type":        "mcp_tool_result",
			"tool_use_id": part.ToolCallID,
			"is_error":    output.kind == "error-json",
			"content":     output.value,
		}, false)
	} else if providerToolName == "code_execution" {
		return c.convertCodeExecutionResult(part, output, push, unsupportedOutput)
	}

	switch providerToolName {
	case "code_execution":
		// Only reachable after an mcp result (TS falls through to the final warning).
		c.warn(fmt.Sprintf("provider executed tool result for tool %s is not supported", part.ToolName))

	case "web_fetch":
		if output.kind == "error-json" {
			push(map[string]interface{}{
				"type":        "web_fetch_tool_result",
				"tool_use_id": part.ToolCallID,
				"content": map[string]interface{}{
					"type":       "web_fetch_tool_result_error",
					"error_code": extractErrorCode(output.value),
				},
			}, true)
			return nil
		}
		if output.kind != "json" {
			unsupportedOutput()
			return nil
		}
		push(map[string]interface{}{
			"type":        "web_fetch_tool_result",
			"tool_use_id": part.ToolCallID,
			"content":     anthropicWebFetchResultContent(output.value),
		}, true)

	case "web_search":
		if output.kind == "error-json" {
			push(map[string]interface{}{
				"type":        "web_search_tool_result",
				"tool_use_id": part.ToolCallID,
				"content": map[string]interface{}{
					"type":       "web_search_tool_result_error",
					"error_code": extractErrorCode(output.value),
				},
			}, true)
			return nil
		}
		if output.kind != "json" {
			unsupportedOutput()
			return nil
		}
		push(map[string]interface{}{
			"type":        "web_search_tool_result",
			"tool_use_id": part.ToolCallID,
			"content":     anthropicWebSearchResultContent(output.value),
		}, true)

	case "tool_search_tool_regex", "tool_search_tool_bm25":
		if output.kind != "json" {
			unsupportedOutput()
			return nil
		}
		refs := make([]map[string]interface{}, 0)
		for _, item := range interfaceSlice(output.value) {
			if m, ok := item.(map[string]interface{}); ok {
				refs = append(refs, map[string]interface{}{"type": "tool_reference", "tool_name": m["toolName"]})
			}
		}
		push(map[string]interface{}{
			"type":        "tool_search_tool_result",
			"tool_use_id": part.ToolCallID,
			"content": map[string]interface{}{
				"type":            "tool_search_tool_search_result",
				"tool_references": refs,
			},
		}, false)

	case "advisor":
		if output.kind != "json" && output.kind != "error-json" {
			unsupportedOutput()
			return nil
		}
		value, _ := output.value.(map[string]interface{})
		var resultContent map[string]interface{}
		switch value["type"] {
		case "advisor_result":
			resultContent = map[string]interface{}{"type": "advisor_result", "text": value["text"]}
		case "advisor_redacted_result":
			resultContent = map[string]interface{}{"type": "advisor_redacted_result", "encrypted_content": value["encryptedContent"]}
		default:
			resultContent = map[string]interface{}{"type": "advisor_tool_result_error", "error_code": value["errorCode"]}
		}
		if value["type"] == "advisor_result" || value["type"] == "advisor_redacted_result" {
			if stopReason, ok := value["stopReason"]; ok {
				resultContent["stop_reason"] = stopReason
			}
		}
		push(map[string]interface{}{
			"type":        "advisor_tool_result",
			"tool_use_id": part.ToolCallID,
			"content":     resultContent,
		}, false)

	default:
		c.warn(fmt.Sprintf("provider executed tool result for tool %s is not supported", part.ToolName))
	}
	return nil
}

func (c *anthropicConverter) convertCodeExecutionResult(part types.ToolResultContent, output anthropicToolOutput, push func(map[string]interface{}, bool), unsupportedOutput func()) error {

	if output.isError() {
		errorInfo := parseErrorValue(output.value)
		errorCode, _ := errorInfo["errorCode"].(string)
		if errorCode == "" {
			errorCode = "unknown"
		}
		if errorInfo["type"] == "code_execution_tool_result_error" {
			push(map[string]interface{}{
				"type":        "code_execution_tool_result",
				"tool_use_id": part.ToolCallID,
				"content": map[string]interface{}{
					"type":       "code_execution_tool_result_error",
					"error_code": errorCode,
				},
			}, false)
		} else {
			push(map[string]interface{}{
				"type":        "bash_code_execution_tool_result",
				"tool_use_id": part.ToolCallID,
				"content": map[string]interface{}{
					"type":       "bash_code_execution_tool_result_error",
					"error_code": errorCode,
				},
			}, false)
		}
		return nil
	}
	if output.kind != "json" {
		unsupportedOutput()
		return nil
	}
	value, ok := output.value.(map[string]interface{})
	valueType, typeOK := value["type"].(string)
	if !ok || !typeOK {
		c.warn(fmt.Sprintf("provider executed tool result output value is not a valid code execution result for tool %s", part.ToolName))
		return nil
	}
	switch valueType {
	case "code_execution_result":
		push(map[string]interface{}{
			"type":        "code_execution_tool_result",
			"tool_use_id": part.ToolCallID,
			"content": map[string]interface{}{
				"type":        valueType,
				"stdout":      value["stdout"],
				"stderr":      value["stderr"],
				"return_code": value["return_code"],
				"content":     sliceOrEmpty(value["content"]),
			},
		}, false)
	case "encrypted_code_execution_result":
		push(map[string]interface{}{
			"type":        "code_execution_tool_result",
			"tool_use_id": part.ToolCallID,
			"content": map[string]interface{}{
				"type":             valueType,
				"encrypted_stdout": value["encrypted_stdout"],
				"stderr":           value["stderr"],
				"return_code":      value["return_code"],
				"content":          sliceOrEmpty(value["content"]),
			},
		}, false)
	case "bash_code_execution_result":
		push(map[string]interface{}{
			"type":        "bash_code_execution_tool_result",
			"tool_use_id": part.ToolCallID,
			"content": map[string]interface{}{
				"type":        valueType,
				"stdout":      value["stdout"],
				"stderr":      value["stderr"],
				"return_code": value["return_code"],
				"content":     value["content"],
			},
		}, false)
	case "bash_code_execution_tool_result_error":
		push(map[string]interface{}{
			"type":        "bash_code_execution_tool_result",
			"tool_use_id": part.ToolCallID,
			"content":     value,
		}, false)
	default:
		push(map[string]interface{}{
			"type":        "text_editor_code_execution_tool_result",
			"tool_use_id": part.ToolCallID,
			"content":     value,
		}, false)
	}
	return nil
}

// moveToolUseBlocksToEnd moves client tool_use blocks after the other blocks
// of their segment; thinking and redacted_thinking blocks delimit segments.
func moveToolUseBlocksToEnd(content []map[string]interface{}) []map[string]interface{} {
	result := make([]map[string]interface{}, 0, len(content))
	var segment []map[string]interface{}
	flush := func() {
		for _, part := range segment {
			if part["type"] != "tool_use" {
				result = append(result, part)
			}
		}
		for _, part := range segment {
			if part["type"] == "tool_use" {
				result = append(result, part)
			}
		}
		segment = nil
	}
	for _, part := range content {
		if part["type"] == "thinking" || part["type"] == "redacted_thinking" || part["type"] == "fallback" {
			// fallback blocks are a boundary between thinking blocks from
			// different models, same as thinking/redacted_thinking itself
			// (TS commit a587f554f7, #21736): they must stay in their
			// original position rather than being swept to the end with
			// tool_use blocks.
			flush()
			result = append(result, part)
		} else {
			segment = append(segment, part)
		}
	}
	flush()
	return result
}

func (c *anthropicConverter) toolsetName(toolName string, providerOptions map[string]interface{}) string {
	if name, ok := c.opts.ToolsetNames[toolName]; ok && name != "" {
		return name
	}
	name, _ := anthropicOptions(providerOptions)["toolsetName"].(string)
	return name
}

func anthropicCaller(providerOptions map[string]interface{}) map[string]interface{} {
	caller, ok := anthropicOptions(providerOptions)["caller"].(map[string]interface{})
	if !ok {
		return nil
	}
	callerType, _ := caller["type"].(string)
	toolID, _ := caller["toolId"].(string)
	if (callerType == "code_execution_20250825" || callerType == "code_execution_20260120") && toolID != "" {
		return map[string]interface{}{"type": callerType, "tool_id": toolID}
	}
	if callerType == "direct" {
		return map[string]interface{}{"type": "direct"}
	}
	return nil
}

// toAnthropicToolInput wraps invalid (non-object) tool call input because
// Anthropic requires tool_use.input to be an object.
func toAnthropicToolInput(input interface{}) map[string]interface{} {
	switch v := input.(type) {
	case map[string]interface{}:
		return v
	case nil:
		return map[string]interface{}{}
	default:
		return map[string]interface{}{"rawInvalidInput": v}
	}
}

func inputOrEmpty(input interface{}) interface{} {
	if input == nil {
		return map[string]interface{}{}
	}
	return input
}

// ── helpers ──────────────────────────────────────────────────────────────────

func anthropicOptions(options map[string]interface{}) map[string]interface{} {
	if options == nil {
		return nil
	}
	anthropicOpts, _ := options["anthropic"].(map[string]interface{})
	return anthropicOpts
}

func rawMetadataMap(raw json.RawMessage) map[string]interface{} {
	if len(raw) == 0 {
		return nil
	}
	var out map[string]interface{}
	if err := json.Unmarshal(raw, &out); err != nil {
		return nil
	}
	return out
}

func setIfNotNil(m map[string]interface{}, key string, value interface{}) {
	if value != nil {
		m[key] = value
	}
}

func withoutKey(m map[string]interface{}, key string) map[string]interface{} {
	out := make(map[string]interface{}, len(m))
	for k, v := range m {
		if k != key {
			out[k] = v
		}
	}
	return out
}

func sliceOrEmpty(v interface{}) interface{} {
	if v == nil {
		return []interface{}{}
	}
	return v
}

func stringValue(v interface{}) string {
	if s, ok := v.(string); ok {
		return s
	}
	return jsonStringify(v)
}

// jsonStringify mirrors JSON.stringify for tool output values.
func jsonStringify(v interface{}) string {
	data, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprintf("%v", v)
	}
	return string(data)
}

// parseErrorValue mirrors the error-info parsing for code execution errors:
// JSON strings are parsed, objects are used as-is.
func parseErrorValue(value interface{}) map[string]interface{} {
	switch v := value.(type) {
	case string:
		var parsed map[string]interface{}
		if err := json.Unmarshal([]byte(v), &parsed); err == nil {
			return parsed
		}
	case map[string]interface{}:
		return v
	}
	return map[string]interface{}{}
}

// extractErrorCode mirrors extractErrorValue(...).errorCode ?? 'unavailable'.
func extractErrorCode(value interface{}) string {
	if code, _ := parseErrorValue(value)["errorCode"].(string); code != "" {
		return code
	}
	return "unavailable"
}

func interfaceSlice(value interface{}) []interface{} {
	switch v := value.(type) {
	case []interface{}:
		return v
	case []map[string]interface{}:
		out := make([]interface{}, len(v))
		for i := range v {
			out[i] = v[i]
		}
		return out
	default:
		return nil
	}
}

func copyIfPresent(dst map[string]interface{}, dstKey string, src map[string]interface{}, srcKey string) {
	if v, ok := src[srcKey]; ok {
		dst[dstKey] = v
	}
}

func anthropicWebSearchResultContent(value interface{}) []map[string]interface{} {
	items := interfaceSlice(value)
	out := make([]map[string]interface{}, 0, len(items))
	for _, item := range items {
		m, ok := item.(map[string]interface{})
		if !ok {
			continue
		}
		result := map[string]interface{}{}
		copyIfPresent(result, "url", m, "url")
		copyIfPresent(result, "title", m, "title")
		copyIfPresent(result, "page_age", m, "pageAge")
		copyIfPresent(result, "encrypted_content", m, "encryptedContent")
		copyIfPresent(result, "type", m, "type")
		out = append(out, result)
	}
	return out
}

func anthropicWebFetchResultContent(value interface{}) map[string]interface{} {
	m, _ := value.(map[string]interface{})
	result := map[string]interface{}{"type": "web_fetch_result"}
	copyIfPresent(result, "url", m, "url")
	copyIfPresent(result, "retrieved_at", m, "retrievedAt")
	content, _ := m["content"].(map[string]interface{})
	doc := map[string]interface{}{"type": "document"}
	copyIfPresent(doc, "title", content, "title")
	copyIfPresent(doc, "citations", content, "citations")
	source, _ := content["source"].(map[string]interface{})
	src := map[string]interface{}{}
	copyIfPresent(src, "type", source, "type")
	copyIfPresent(src, "media_type", source, "mediaType")
	copyIfPresent(src, "data", source, "data")
	doc["source"] = src
	result["content"] = doc
	return result
}
