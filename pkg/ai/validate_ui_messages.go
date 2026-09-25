package ai

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/schema"
)

// ValidateUIMessagesOptions configures ValidateUIMessages and
// SafeValidateUIMessages. Mirrors TS ValidateUIMessagesOptions.
type ValidateUIMessagesOptions struct {
	// Messages are the messages to validate. Accepts []UIMessage,
	// []UIMessageChunk, decoded JSON ([]interface{}), json.RawMessage,
	// []byte or string JSON, or any value that marshals to a UI message
	// array. nil means "not provided".
	Messages interface{}

	// MetadataSchema validates each message's metadata. The validated value
	// (with schema defaults applied) replaces the metadata.
	MetadataSchema schema.Schema

	// DataSchemas validate data parts by name (without the `data-` prefix).
	// When set, a data part without a schema is an error. Validated values
	// (with schema defaults applied) replace the part data.
	DataSchemas map[string]schema.Schema

	// Tools validate static tool parts against the current tool input
	// (Parameters) and output (OutputSchema) schemas. A nil slice means no
	// tools were provided (tool parts are not validated); a non-nil empty
	// slice validates against an empty tool set.
	Tools []types.Tool

	// ExperimentalRefineToolInput reconstructs the approved input from an
	// approval's inputSchemaInput before comparing it with the part input.
	ExperimentalRefineToolInput map[string]ToolInputRefiner
}

// SafeValidateUIMessagesResult is the result of SafeValidateUIMessages.
type SafeValidateUIMessagesResult struct {
	Success bool
	Data    []UIMessage
	Error   error
}

// ValidateUIMessages validates a list of UI messages. Metadata, data parts
// and tool parts are only validated when the corresponding schemas / tools
// are provided. Mirrors TS validateUIMessages.
func ValidateUIMessages(ctx context.Context, opts ValidateUIMessagesOptions) ([]UIMessage, error) {
	result := SafeValidateUIMessages(ctx, opts)
	if !result.Success {
		return nil, result.Error
	}
	return result.Data, nil
}

// SafeValidateUIMessages validates like ValidateUIMessages but returns a
// result instead of an error. Mirrors TS safeValidateUIMessages.
func SafeValidateUIMessages(ctx context.Context, opts ValidateUIMessagesOptions) SafeValidateUIMessagesResult {
	return safeValidateUIMessagesInternal(ctx, opts, false)
}

// ValidateUIMessagesForAgent validates like ValidateUIMessages, but terminal
// tool parts whose tools are not registered (for example tools from a
// disconnected MCP server) are converted to dynamic tool parts even when no
// tools are provided. Mirrors TS validateUIMessagesForAgent.
func ValidateUIMessagesForAgent(ctx context.Context, opts ValidateUIMessagesOptions) ([]UIMessage, error) {
	result := safeValidateUIMessagesInternal(ctx, opts, true)
	if !result.Success {
		return nil, result.Error
	}
	return result.Data, nil
}

func safeValidateUIMessagesInternal(ctx context.Context, opts ValidateUIMessagesOptions, convertMissingTerminalToolsToDynamic bool) SafeValidateUIMessagesResult {
	fail := func(err error) SafeValidateUIMessagesResult {
		return SafeValidateUIMessagesResult{Success: false, Error: err}
	}
	if ctx == nil {
		ctx = context.Background()
	}

	raw, err := uiMessagesToJSONValue(opts.Messages)
	if err != nil {
		return fail(err)
	}
	if raw == nil {
		return fail(&providererrors.InvalidArgumentError{
			Field:   "messages",
			Message: "messages parameter must be provided",
		})
	}
	if err := validateUIMessagesStructure(raw); err != nil {
		return fail(err)
	}
	messages, err := decodeValidatedUIMessages(raw)
	if err != nil {
		return fail(err)
	}

	if opts.MetadataSchema != nil {
		for i := range messages {
			message := &messages[i]
			if err := opts.MetadataSchema.Validator().Validate(message.Metadata); err != nil {
				return fail(uiValidationError(message.Metadata, err, fmt.Sprintf("messages[%d].metadata", i), "", message.ID))
			}
			message.Metadata = schema.ApplyDefaults(message.Metadata, opts.MetadataSchema)
		}
	}

	shouldValidateToolParts := opts.Tools != nil || convertMissingTerminalToolsToDynamic
	if opts.DataSchemas == nil && !shouldValidateToolParts {
		return SafeValidateUIMessagesResult{Success: true, Data: messages}
	}

	for msgIdx := range messages {
		message := &messages[msgIdx]
		for partIdx, part := range message.Parts {
			if dataPart, ok := asDataUIPart(part); ok && opts.DataSchemas != nil {
				dataName := dataPart.DataName()
				field := fmt.Sprintf("messages[%d].parts[%d].data", msgIdx, partIdx)
				dataSchema := opts.DataSchemas[dataName]
				if dataSchema == nil {
					return fail(uiValidationError(dataPart.Data, fmt.Errorf("No data schema found for data part %s", dataName), field, dataName, dataPart.ID))
				}
				if err := dataSchema.Validator().Validate(dataPart.Data); err != nil {
					return fail(uiValidationError(dataPart.Data, err, field, dataName, dataPart.ID))
				}
				dataPart.Data = schema.ApplyDefaults(dataPart.Data, dataSchema)
			}

			if !shouldValidateToolParts || !IsStaticToolUIPart(part) {
				continue
			}
			toolPart, _ := AsToolUIPart(part)
			toolName := GetStaticToolName(toolPart)
			tool := findUITool(opts.Tools, toolName)
			isTerminal := toolPart.State == ToolStateOutputAvailable ||
				toolPart.State == ToolStateOutputError ||
				toolPart.State == ToolStateOutputDenied

			if tool == nil && isTerminal {
				// Persisted terminal history can reference tools that are no
				// longer registered. Normalize those parts so callers do not
				// receive unvalidated values under current static tool types.
				message.Parts[partIdx] = asDynamicToolPart(toolPart)
				continue
			}

			inputField := fmt.Sprintf("messages[%d].parts[%d].input", msgIdx, partIdx)
			if tool == nil {
				return fail(uiValidationError(toolPart.Input, fmt.Errorf("No tool schema found for tool part %s", toolName), inputField, toolName, toolPart.ToolCallID))
			}

			hasInputSchemaInput := toolPart.Approval != nil && toolPart.Approval.InputSchemaInput != nil
			inputToValidate := toolPart.Input
			if hasInputSchemaInput {
				inputToValidate = toolPart.Approval.InputSchemaInput
			}
			convertToDynamic := false

			if toolPart.State != ToolStateInputStreaming &&
				(toolPart.State != ToolStateOutputError || hasInputSchemaInput || toolPart.Input != nil) {
				inputErr := validateUIToolInput(ctx, tool, toolPart, inputToValidate, hasInputSchemaInput, opts.ExperimentalRefineToolInput[toolName], inputField)
				if inputErr != nil {
					// Failed calls and empty terminal inputs must remain
					// loadable, but must not claim to match the current
					// static input type.
					if toolPart.State == ToolStateOutputError ||
						(toolPart.State == ToolStateOutputAvailable && isEmptyJSONObject(toolPart.Input)) {
						convertToDynamic = true
					} else {
						return fail(inputErr)
					}
				}
			}

			if toolPart.State == ToolStateOutputAvailable {
				if validator := schemaValidatorFor(tool.OutputSchema); validator != nil {
					if err := validator.Validate(toolPart.Output); err != nil {
						return fail(uiValidationError(toolPart.Output, err, fmt.Sprintf("messages[%d].parts[%d].output", msgIdx, partIdx), toolName, toolPart.ToolCallID))
					}
				}
			}

			if convertToDynamic {
				message.Parts[partIdx] = asDynamicToolPart(toolPart)
			}
		}
	}

	return SafeValidateUIMessagesResult{Success: true, Data: messages}
}

func validateUIToolInput(ctx context.Context, tool *types.Tool, toolPart *ToolUIPart, value interface{}, hasInputSchemaInput bool, refine ToolInputRefiner, field string) error {
	toolName := GetStaticToolName(toolPart)
	parsed := value
	if validator := toolInputValidator(tool); validator != nil {
		if err := validator.Validate(value); err != nil {
			return uiValidationError(value, err, field, toolName, toolPart.ToolCallID)
		}
		switch s := tool.Parameters.(type) {
		case schema.Schema:
			parsed = schema.ApplyDefaults(value, s)
		case map[string]interface{}:
			parsed = schema.ApplyDefaults(value, schema.NewSimpleJSONSchema(s))
		}
	}
	if !hasInputSchemaInput {
		return nil
	}

	reconstructed := parsed
	if refine != nil {
		args, err := toArgumentsMap(parsed)
		if err != nil {
			return uiValidationError(value, err, field, toolName, toolPart.ToolCallID)
		}
		refined, err := refine(ctx, ToolInputRefinementOptions{
			ToolCall: types.ToolCall{ID: toolPart.ToolCallID, ToolName: toolName, Arguments: args},
			Tool:     tool,
		})
		if err != nil {
			return uiValidationError(value, err, field, toolName, toolPart.ToolCallID)
		}
		if refined != nil {
			reconstructed = refined
		}
	}
	if !isDeepEqualJSONData(reconstructed, toolPart.Input) {
		return uiValidationError(toolPart.Input, fmt.Errorf("Tool input does not match the output reconstructed from inputSchemaInput."), field, toolName, toolPart.ToolCallID)
	}
	return nil
}

func schemaValidatorFor(s interface{}) schema.Validator {
	switch v := s.(type) {
	case schema.Schema:
		if v == nil {
			return nil
		}
		return v.Validator()
	case map[string]interface{}:
		if v == nil {
			return nil
		}
		return schema.NewSimpleJSONSchema(v).Validator()
	}
	return nil
}

func uiValidationError(value interface{}, cause error, field, entityName, entityID string) error {
	return providererrors.NewValidationErrorWithContext(value, cause.Error(), cause, &providererrors.ValidationContext{
		Field:      field,
		EntityName: entityName,
		EntityID:   entityID,
	})
}

func isEmptyJSONObject(value interface{}) bool {
	m, ok := value.(map[string]interface{})
	return ok && len(m) == 0
}

func asDynamicToolPart(part *ToolUIPart) *ToolUIPart {
	dynamic := *part
	dynamic.ToolName = GetStaticToolName(part)
	dynamic.Type = "dynamic-tool"
	return &dynamic
}

// uiMessagesToJSONValue converts the caller-supplied messages into decoded
// JSON data. It returns nil when no messages were provided.
func uiMessagesToJSONValue(messages interface{}) (interface{}, error) {
	var data []byte
	switch v := messages.(type) {
	case nil:
		return nil, nil
	case json.RawMessage:
		data = v
	case []byte:
		data = v
	case string:
		data = []byte(v)
	default:
		encoded, err := json.Marshal(v)
		if err != nil {
			return nil, &providererrors.InvalidArgumentError{Field: "messages", Message: err.Error(), Cause: err}
		}
		data = encoded
	}
	var out interface{}
	if err := json.Unmarshal(data, &out); err != nil {
		return nil, &providererrors.InvalidArgumentError{Field: "messages", Message: err.Error(), Cause: err}
	}
	return out, nil
}

// decodeValidatedUIMessages decodes structurally valid messages and drops
// fields the TS schema does not declare for a part's state (zod strips them).
func decodeValidatedUIMessages(raw interface{}) ([]UIMessage, error) {
	data, err := json.Marshal(raw)
	if err != nil {
		return nil, err
	}
	var messages []UIMessage
	if err := json.Unmarshal(data, &messages); err != nil {
		return nil, err
	}
	for i := range messages {
		for _, part := range messages[i].Parts {
			tool, ok := AsToolUIPart(part)
			if !ok {
				continue
			}
			if tool.State != ToolStateOutputError {
				tool.RawInput = nil
			}
			if tool.State != ToolStateOutputAvailable && tool.State != ToolStateOutputError {
				tool.ResultProviderMetadata = nil
			}
			if tool.State != ToolStateOutputAvailable {
				tool.Preliminary = nil
			}
		}
	}
	return messages, nil
}

// ---------------------------------------------------------------------------
// Structural validation (TS uiMessagesSchema)
// ---------------------------------------------------------------------------

type uiSchemaIssue struct {
	path    string
	message string
}

func (i uiSchemaIssue) String() string {
	if i.path == "" {
		return i.message
	}
	return fmt.Sprintf("%s: %s", i.path, i.message)
}

func validateUIMessagesStructure(raw interface{}) error {
	issue := uiMessagesStructureIssue(raw)
	if issue == nil {
		return nil
	}
	return providererrors.NewValidationErrorWithContext(raw, issue.String(), fmt.Errorf("%s", issue.message), &providererrors.ValidationContext{})
}

func uiMessagesStructureIssue(raw interface{}) *uiSchemaIssue {
	messages, ok := raw.([]interface{})
	if !ok {
		return &uiSchemaIssue{message: "Invalid input: expected array"}
	}
	if len(messages) == 0 {
		return &uiSchemaIssue{message: "Messages array must not be empty"}
	}
	for i, rawMessage := range messages {
		path := fmt.Sprintf("[%d]", i)
		message, ok := rawMessage.(map[string]interface{})
		if !ok {
			return &uiSchemaIssue{path: path, message: "Invalid input: expected object"}
		}
		if msg := requireString(message, "id"); msg != "" {
			return &uiSchemaIssue{path: path + ".id", message: msg}
		}
		role, _ := message["role"].(string)
		if role != "system" && role != "user" && role != "assistant" {
			return &uiSchemaIssue{path: path + ".role", message: `Invalid option: expected one of "system"|"user"|"assistant"`}
		}
		parts, ok := message["parts"].([]interface{})
		if !ok {
			return &uiSchemaIssue{path: path + ".parts", message: "Invalid input: expected array"}
		}
		for j, rawPart := range parts {
			if msg := uiPartIssue(rawPart); msg != "" {
				return &uiSchemaIssue{path: fmt.Sprintf("%s.parts[%d]", path, j), message: msg}
			}
		}
		if role != "assistant" && len(parts) == 0 {
			return &uiSchemaIssue{path: path + ".parts", message: "Message must contain at least one part"}
		}
	}
	return nil
}

// uiPartIssue returns an empty string when the part matches one of the TS
// UI message part schemas.
func uiPartIssue(raw interface{}) string {
	part, ok := raw.(map[string]interface{})
	if !ok {
		return "Invalid input: expected object"
	}
	partType, ok := part["type"].(string)
	if !ok {
		return "Invalid input: expected string for type"
	}
	checks := func(fns ...func() string) string {
		for _, fn := range fns {
			if msg := fn(); msg != "" {
				return msg
			}
		}
		return ""
	}
	str := func(key string) func() string { return func() string { return requireString(part, key) } }
	optStr := func(key string) func() string { return func() string { return optionalString(part, key) } }
	optPM := func(key string) func() string { return func() string { return optionalProviderMetadata(part, key) } }
	optState := func() string {
		if v, exists := part["state"]; exists && v != "streaming" && v != "done" {
			return `Invalid option: expected one of "streaming"|"done" for state`
		}
		return ""
	}

	switch {
	case partType == "text":
		return checks(str("text"), optState, optPM("providerMetadata"))
	case partType == "reasoning":
		return checks(optStr("id"), str("text"), optState, optPM("providerMetadata"))
	case partType == "custom":
		return checks(str("kind"), optPM("providerMetadata"))
	case partType == "source-url":
		return checks(str("sourceId"), str("url"), optStr("title"), optPM("providerMetadata"))
	case partType == "source-document":
		return checks(str("sourceId"), str("mediaType"), str("title"), optStr("filename"), optPM("providerMetadata"))
	case partType == "file":
		return checks(str("mediaType"), optStr("filename"), str("url"), func() string {
			v, exists := part["providerReference"]
			if !exists {
				return ""
			}
			m, ok := v.(map[string]interface{})
			if !ok {
				return "Invalid input: expected record for providerReference"
			}
			for key, value := range m {
				if _, ok := value.(string); !ok {
					return fmt.Sprintf("Invalid input: expected string for providerReference.%s", key)
				}
			}
			return ""
		}, optPM("providerMetadata"))
	case partType == "reasoning-file":
		return checks(str("mediaType"), str("url"), optPM("providerMetadata"))
	case partType == "step-start":
		return ""
	case strings.HasPrefix(partType, "data-"):
		return optionalString(part, "id")
	case partType == "dynamic-tool":
		return checks(str("toolName"), func() string { return uiToolPartIssue(part) })
	case strings.HasPrefix(partType, "tool-"):
		return uiToolPartIssue(part)
	}
	return fmt.Sprintf("Invalid input: unsupported part type %q", partType)
}

func uiToolPartIssue(part map[string]interface{}) string {
	if msg := requireString(part, "toolCallId"); msg != "" {
		return msg
	}
	for _, check := range []string{
		optionalString(part, "title"),
		optionalJSONRecord(part, "toolMetadata"),
		optionalBool(part, "providerExecuted"),
		optionalProviderMetadata(part, "callProviderMetadata"),
	} {
		if check != "" {
			return check
		}
	}
	state, _ := part["state"].(string)
	never := func(keys ...string) string {
		for _, key := range keys {
			if _, exists := part[key]; exists {
				return fmt.Sprintf("Invalid input: %s must be absent in state %s", key, state)
			}
		}
		return ""
	}
	switch ToolUIPartState(state) {
	case ToolStateInputStreaming, ToolStateInputAvailable:
		return never("output", "errorText", "approval")
	case ToolStateApprovalRequested:
		if msg := never("output", "errorText"); msg != "" {
			return msg
		}
		return approvalIssue(part, "requested", false)
	case ToolStateApprovalResponded:
		if msg := never("output", "errorText"); msg != "" {
			return msg
		}
		return approvalIssue(part, "responded", false)
	case ToolStateOutputAvailable:
		if msg := never("errorText"); msg != "" {
			return msg
		}
		if msg := optionalProviderMetadata(part, "resultProviderMetadata"); msg != "" {
			return msg
		}
		if msg := optionalBool(part, "preliminary"); msg != "" {
			return msg
		}
		return approvalIssue(part, "granted", true)
	case ToolStateOutputError:
		if msg := never("output"); msg != "" {
			return msg
		}
		if msg := requireString(part, "errorText"); msg != "" {
			return msg
		}
		if msg := optionalProviderMetadata(part, "resultProviderMetadata"); msg != "" {
			return msg
		}
		return approvalIssue(part, "granted", true)
	case ToolStateOutputDenied:
		if msg := never("output", "errorText"); msg != "" {
			return msg
		}
		return approvalIssue(part, "denied", false)
	}
	return fmt.Sprintf("Invalid input: unsupported tool state %q", state)
}

// approvalIssue validates the approval object against the TS approval
// schemas: requested, responded, granted (approved: true) and denied
// (approved: false).
func approvalIssue(part map[string]interface{}, kind string, optional bool) string {
	raw, exists := part["approval"]
	if !exists {
		if optional {
			return ""
		}
		return "Invalid input: approval is required"
	}
	approval, ok := raw.(map[string]interface{})
	if !ok {
		return "Invalid input: expected object for approval"
	}
	for _, check := range []string{
		requireString(approval, "id"),
		optionalString(approval, "requestReason"),
		optionalBool(approval, "isAutomatic"),
		optionalString(approval, "signature"),
	} {
		if check != "" {
			return "approval." + check
		}
	}
	approved, hasApproved := approval["approved"]
	switch kind {
	case "requested":
		if hasApproved {
			return "Invalid input: approval.approved must be absent"
		}
		if _, exists := approval["reason"]; exists {
			return "Invalid input: approval.reason must be absent"
		}
		return ""
	}
	b, ok := approved.(bool)
	if !hasApproved || !ok {
		return "Invalid input: expected boolean for approval.approved"
	}
	if msg := optionalString(approval, "reason"); msg != "" {
		return "approval." + msg
	}
	if kind == "granted" && !b {
		return "Invalid input: expected approval.approved to be true"
	}
	if kind == "denied" && b {
		return "Invalid input: expected approval.approved to be false"
	}
	return ""
}

func requireString(obj map[string]interface{}, key string) string {
	if _, ok := obj[key].(string); !ok {
		return fmt.Sprintf("Invalid input: expected string for %s", key)
	}
	return ""
}

func optionalString(obj map[string]interface{}, key string) string {
	if v, exists := obj[key]; exists {
		if _, ok := v.(string); !ok {
			return fmt.Sprintf("Invalid input: expected string for %s", key)
		}
	}
	return ""
}

func optionalBool(obj map[string]interface{}, key string) string {
	if v, exists := obj[key]; exists {
		if _, ok := v.(bool); !ok {
			return fmt.Sprintf("Invalid input: expected boolean for %s", key)
		}
	}
	return ""
}

func optionalJSONRecord(obj map[string]interface{}, key string) string {
	if v, exists := obj[key]; exists {
		if _, ok := v.(map[string]interface{}); !ok {
			return fmt.Sprintf("Invalid input: expected record for %s", key)
		}
	}
	return ""
}

// optionalProviderMetadata checks record<string, record<string, JSON>>.
func optionalProviderMetadata(obj map[string]interface{}, key string) string {
	v, exists := obj[key]
	if !exists {
		return ""
	}
	m, ok := v.(map[string]interface{})
	if !ok {
		return fmt.Sprintf("Invalid input: expected record for %s", key)
	}
	for provider, value := range m {
		if _, ok := value.(map[string]interface{}); !ok {
			return fmt.Sprintf("Invalid input: expected record for %s.%s", key, provider)
		}
	}
	return ""
}
