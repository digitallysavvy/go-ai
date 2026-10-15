package mcp

import "encoding/json"

type looseMCPResourceAlias MCPResource

// UnmarshalJSON decodes MCPResource and keeps the keys it does not declare in Extra.
func (v *MCPResource) UnmarshalJSON(data []byte) error {
	var a looseMCPResourceAlias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*v = MCPResource(a)
	v.Extra = decodeLooseExtra(data, a)
	return nil
}

// MarshalJSON encodes MCPResource followed by its Extra keys.
func (v MCPResource) MarshalJSON() ([]byte, error) {
	return marshalLoose(looseMCPResourceAlias(v), v.Extra)
}

type looseMCPResourceTemplateAlias MCPResourceTemplate

// UnmarshalJSON decodes MCPResourceTemplate and keeps the keys it does not declare in Extra.
func (v *MCPResourceTemplate) UnmarshalJSON(data []byte) error {
	var a looseMCPResourceTemplateAlias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*v = MCPResourceTemplate(a)
	v.Extra = decodeLooseExtra(data, a)
	return nil
}

// MarshalJSON encodes MCPResourceTemplate followed by its Extra keys.
func (v MCPResourceTemplate) MarshalJSON() ([]byte, error) {
	return marshalLoose(looseMCPResourceTemplateAlias(v), v.Extra)
}

type looseMCPPromptAlias MCPPrompt

// UnmarshalJSON decodes MCPPrompt and keeps the keys it does not declare in Extra.
func (v *MCPPrompt) UnmarshalJSON(data []byte) error {
	var a looseMCPPromptAlias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*v = MCPPrompt(a)
	v.Extra = decodeLooseExtra(data, a)
	return nil
}

// MarshalJSON encodes MCPPrompt followed by its Extra keys.
func (v MCPPrompt) MarshalJSON() ([]byte, error) {
	return marshalLoose(looseMCPPromptAlias(v), v.Extra)
}

type looseMCPPromptArgumentAlias MCPPromptArgument

// UnmarshalJSON decodes MCPPromptArgument and keeps the keys it does not declare in Extra.
func (v *MCPPromptArgument) UnmarshalJSON(data []byte) error {
	var a looseMCPPromptArgumentAlias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*v = MCPPromptArgument(a)
	v.extraJSON = encodeLooseExtra(decodeLooseExtra(data, a))
	return nil
}

// MarshalJSON encodes MCPPromptArgument followed by its Extra keys.
func (v MCPPromptArgument) MarshalJSON() ([]byte, error) {
	return marshalLoose(looseMCPPromptArgumentAlias(v), v.Extra())
}

type looseServerInfoAlias ServerInfo

// UnmarshalJSON decodes ServerInfo and keeps the keys it does not declare in Extra.
func (v *ServerInfo) UnmarshalJSON(data []byte) error {
	var a looseServerInfoAlias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*v = ServerInfo(a)
	v.extraJSON = encodeLooseExtra(decodeLooseExtra(data, a))
	return nil
}

// MarshalJSON encodes ServerInfo followed by its Extra keys.
func (v ServerInfo) MarshalJSON() ([]byte, error) {
	return marshalLoose(looseServerInfoAlias(v), v.Extra())
}

type looseClientInfoAlias ClientInfo

// UnmarshalJSON decodes ClientInfo and keeps the keys it does not declare in Extra.
func (v *ClientInfo) UnmarshalJSON(data []byte) error {
	var a looseClientInfoAlias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*v = ClientInfo(a)
	v.extraJSON = encodeLooseExtra(decodeLooseExtra(data, a))
	return nil
}

// MarshalJSON encodes ClientInfo followed by its Extra keys.
func (v ClientInfo) MarshalJSON() ([]byte, error) {
	return marshalLoose(looseClientInfoAlias(v), v.Extra())
}

type looseServerCapabilitiesAlias ServerCapabilities

// UnmarshalJSON decodes ServerCapabilities and keeps the keys it does not declare in Extra.
func (v *ServerCapabilities) UnmarshalJSON(data []byte) error {
	var a looseServerCapabilitiesAlias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*v = ServerCapabilities(a)
	v.Extra = decodeLooseExtra(data, a)
	return nil
}

// MarshalJSON encodes ServerCapabilities followed by its Extra keys.
func (v ServerCapabilities) MarshalJSON() ([]byte, error) {
	return marshalLoose(looseServerCapabilitiesAlias(v), v.Extra)
}

type looseClientCapabilitiesAlias ClientCapabilities

// UnmarshalJSON decodes ClientCapabilities and keeps the keys it does not declare in Extra.
func (v *ClientCapabilities) UnmarshalJSON(data []byte) error {
	var a looseClientCapabilitiesAlias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*v = ClientCapabilities(a)
	v.Extra = decodeLooseExtra(data, a)
	return nil
}

// MarshalJSON encodes ClientCapabilities followed by its Extra keys.
func (v ClientCapabilities) MarshalJSON() ([]byte, error) {
	return marshalLoose(looseClientCapabilitiesAlias(v), v.Extra)
}

type looseEventsCapabilityAlias EventsCapability

// UnmarshalJSON decodes EventsCapability and keeps the keys it does not declare in Extra.
func (v *EventsCapability) UnmarshalJSON(data []byte) error {
	var a looseEventsCapabilityAlias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*v = EventsCapability(a)
	v.extraJSON = encodeLooseExtra(decodeLooseExtra(data, a))
	return nil
}

// MarshalJSON encodes EventsCapability followed by its Extra keys.
func (v EventsCapability) MarshalJSON() ([]byte, error) {
	return marshalLoose(looseEventsCapabilityAlias(v), v.Extra())
}

type looseCompletionsCapabilityAlias CompletionsCapability

// UnmarshalJSON decodes CompletionsCapability and keeps the keys it does not declare in Extra.
func (v *CompletionsCapability) UnmarshalJSON(data []byte) error {
	var a looseCompletionsCapabilityAlias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*v = CompletionsCapability(a)
	v.extraJSON = encodeLooseExtra(decodeLooseExtra(data, a))
	return nil
}

// MarshalJSON encodes CompletionsCapability followed by its Extra keys.
func (v CompletionsCapability) MarshalJSON() ([]byte, error) {
	return marshalLoose(looseCompletionsCapabilityAlias(v), v.Extra())
}

type looseSamplingCapabilityAlias SamplingCapability

// UnmarshalJSON decodes SamplingCapability and keeps the keys it does not declare in Extra.
func (v *SamplingCapability) UnmarshalJSON(data []byte) error {
	var a looseSamplingCapabilityAlias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*v = SamplingCapability(a)
	v.extraJSON = encodeLooseExtra(decodeLooseExtra(data, a))
	return nil
}

// MarshalJSON encodes SamplingCapability followed by its Extra keys.
func (v SamplingCapability) MarshalJSON() ([]byte, error) {
	return marshalLoose(looseSamplingCapabilityAlias(v), v.Extra())
}

type looseLoggingCapabilityAlias LoggingCapability

// UnmarshalJSON decodes LoggingCapability and keeps the keys it does not declare in Extra.
func (v *LoggingCapability) UnmarshalJSON(data []byte) error {
	var a looseLoggingCapabilityAlias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*v = LoggingCapability(a)
	v.extraJSON = encodeLooseExtra(decodeLooseExtra(data, a))
	return nil
}

// MarshalJSON encodes LoggingCapability followed by its Extra keys.
func (v LoggingCapability) MarshalJSON() ([]byte, error) {
	return marshalLoose(looseLoggingCapabilityAlias(v), v.Extra())
}

type loosePromptsCapabilityAlias PromptsCapability

// UnmarshalJSON decodes PromptsCapability and keeps the keys it does not declare in Extra.
func (v *PromptsCapability) UnmarshalJSON(data []byte) error {
	var a loosePromptsCapabilityAlias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*v = PromptsCapability(a)
	v.extraJSON = encodeLooseExtra(decodeLooseExtra(data, a))
	return nil
}

// MarshalJSON encodes PromptsCapability followed by its Extra keys.
func (v PromptsCapability) MarshalJSON() ([]byte, error) {
	return marshalLoose(loosePromptsCapabilityAlias(v), v.Extra())
}

type looseResourcesCapabilityAlias ResourcesCapability

// UnmarshalJSON decodes ResourcesCapability and keeps the keys it does not declare in Extra.
func (v *ResourcesCapability) UnmarshalJSON(data []byte) error {
	var a looseResourcesCapabilityAlias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*v = ResourcesCapability(a)
	v.extraJSON = encodeLooseExtra(decodeLooseExtra(data, a))
	return nil
}

// MarshalJSON encodes ResourcesCapability followed by its Extra keys.
func (v ResourcesCapability) MarshalJSON() ([]byte, error) {
	return marshalLoose(looseResourcesCapabilityAlias(v), v.Extra())
}

type looseToolsCapabilityAlias ToolsCapability

// UnmarshalJSON decodes ToolsCapability and keeps the keys it does not declare in Extra.
func (v *ToolsCapability) UnmarshalJSON(data []byte) error {
	var a looseToolsCapabilityAlias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*v = ToolsCapability(a)
	v.extraJSON = encodeLooseExtra(decodeLooseExtra(data, a))
	return nil
}

// MarshalJSON encodes ToolsCapability followed by its Extra keys.
func (v ToolsCapability) MarshalJSON() ([]byte, error) {
	return marshalLoose(looseToolsCapabilityAlias(v), v.Extra())
}

type looseRootsCapabilityAlias RootsCapability

// UnmarshalJSON decodes RootsCapability and keeps the keys it does not declare in Extra.
func (v *RootsCapability) UnmarshalJSON(data []byte) error {
	var a looseRootsCapabilityAlias
	if err := json.Unmarshal(data, &a); err != nil {
		return err
	}
	*v = RootsCapability(a)
	v.extraJSON = encodeLooseExtra(decodeLooseExtra(data, a))
	return nil
}

// MarshalJSON encodes RootsCapability followed by its Extra keys.
func (v RootsCapability) MarshalJSON() ([]byte, error) {
	return marshalLoose(looseRootsCapabilityAlias(v), v.Extra())
}

// Extra returns the keys MCPPromptArgument does not declare (TS looseObject passthrough).
func (v MCPPromptArgument) Extra() map[string]interface{} { return decodeLooseExtraJSON(v.extraJSON) }

// Extra returns the keys ServerInfo does not declare (TS looseObject passthrough).
func (v ServerInfo) Extra() map[string]interface{} { return decodeLooseExtraJSON(v.extraJSON) }

// Extra returns the keys ClientInfo does not declare (TS looseObject passthrough).
func (v ClientInfo) Extra() map[string]interface{} { return decodeLooseExtraJSON(v.extraJSON) }

// Extra returns the keys EventsCapability does not declare (TS looseObject passthrough).
func (v EventsCapability) Extra() map[string]interface{} { return decodeLooseExtraJSON(v.extraJSON) }

// Extra returns the keys CompletionsCapability does not declare (TS looseObject passthrough).
func (v CompletionsCapability) Extra() map[string]interface{} {
	return decodeLooseExtraJSON(v.extraJSON)
}

// Extra returns the keys SamplingCapability does not declare (TS looseObject passthrough).
func (v SamplingCapability) Extra() map[string]interface{} { return decodeLooseExtraJSON(v.extraJSON) }

// Extra returns the keys LoggingCapability does not declare (TS looseObject passthrough).
func (v LoggingCapability) Extra() map[string]interface{} { return decodeLooseExtraJSON(v.extraJSON) }

// Extra returns the keys PromptsCapability does not declare (TS looseObject passthrough).
func (v PromptsCapability) Extra() map[string]interface{} { return decodeLooseExtraJSON(v.extraJSON) }

// Extra returns the keys ResourcesCapability does not declare (TS looseObject passthrough).
func (v ResourcesCapability) Extra() map[string]interface{} { return decodeLooseExtraJSON(v.extraJSON) }

// Extra returns the keys ToolsCapability does not declare (TS looseObject passthrough).
func (v ToolsCapability) Extra() map[string]interface{} { return decodeLooseExtraJSON(v.extraJSON) }

// Extra returns the keys RootsCapability does not declare (TS looseObject passthrough).
func (v RootsCapability) Extra() map[string]interface{} { return decodeLooseExtraJSON(v.extraJSON) }
