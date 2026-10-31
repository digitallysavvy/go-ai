package acp

import "encoding/json"

// BridgeConfigurationEnv is the environment variable the host serializes
// ACPBridgeConfiguration into for the embedded bridge to read. Mirrors TS
// `ACP_BRIDGE_CONFIGURATION_ENV`.
const BridgeConfigurationEnv = "AI_SDK_ACP_BRIDGE_CONFIGURATION"

// bridgeConfiguration is the non-secret-shaped (secrets travel as plain env
// vars, not in this JSON blob) configuration the host hands the bridge.
// Mirrors TS `ACPBridgeConfiguration`.
type bridgeConfiguration struct {
	Agent                         agentLaunch          `json:"agent"`
	Authentication                *Authentication      `json:"authentication,omitempty"`
	ProviderAuthentication        *bridgeProviderAuth  `json:"providerAuthentication,omitempty"`
	ProviderEnvironment           *map[string]string   `json:"providerEnvironment,omitempty"` // pointer: an empty map must still serialize as {}
	SessionMeta                   map[string]any       `json:"sessionMeta,omitempty"`
	ClientCapabilities            map[string]any       `json:"clientCapabilities,omitempty"`
	AskUserQuestionsRequestMethod string               `json:"askUserQuestionsRequestMethod,omitempty"`
	HostToolMCPTransport          HostToolMCPTransport `json:"hostToolMcpTransport,omitempty"`
	NonTextContentTypes           []NonTextContentType `json:"nonTextContentTypes,omitempty"`
}

// MarshalJSON emits sessionMeta, clientCapabilities and nonTextContentTypes
// whenever they are non-nil, even when empty: TS spreads each one under a
// `== null` check, so an empty record or array still reaches the bridge (an
// empty sessionMeta becomes `_meta: {}` on session/new).
func (c bridgeConfiguration) MarshalJSON() ([]byte, error) {
	type wire struct {
		Agent                         agentLaunch           `json:"agent"`
		Authentication                *Authentication       `json:"authentication,omitempty"`
		ProviderAuthentication        *bridgeProviderAuth   `json:"providerAuthentication,omitempty"`
		ProviderEnvironment           *map[string]string    `json:"providerEnvironment,omitempty"`
		SessionMeta                   *map[string]any       `json:"sessionMeta,omitempty"`
		ClientCapabilities            *map[string]any       `json:"clientCapabilities,omitempty"`
		AskUserQuestionsRequestMethod string                `json:"askUserQuestionsRequestMethod,omitempty"`
		HostToolMCPTransport          HostToolMCPTransport  `json:"hostToolMcpTransport,omitempty"`
		NonTextContentTypes           *[]NonTextContentType `json:"nonTextContentTypes,omitempty"`
	}
	w := wire{
		Agent: c.Agent.withArgs(), Authentication: c.Authentication, ProviderAuthentication: c.ProviderAuthentication,
		ProviderEnvironment: c.ProviderEnvironment, AskUserQuestionsRequestMethod: c.AskUserQuestionsRequestMethod,
		HostToolMCPTransport: c.HostToolMCPTransport,
	}
	if c.SessionMeta != nil {
		w.SessionMeta = &c.SessionMeta
	}
	if c.ClientCapabilities != nil {
		w.ClientCapabilities = &c.ClientCapabilities
	}
	if c.NonTextContentTypes != nil {
		w.NonTextContentTypes = &c.NonTextContentTypes
	}
	return json.Marshal(w)
}

// bridgeProviderAuth mirrors TS `ACPResolvedProviderAuthentication`
// (discriminated union collapsed into one struct for the Go wire encoding).
type bridgeProviderAuth struct {
	Type string         `json:"type"`
	Env  map[string]any `json:"env,omitempty"`
}

// createBridgeEnvironment mirrors TS `createACPBridgeEnvironment`.
func createBridgeEnvironment(cfg bridgeConfiguration) map[string]string {
	data, err := json.Marshal(cfg)
	if err != nil {
		panic(err)
	}
	return map[string]string{BridgeConfigurationEnv: string(data)}
}

// withArgs returns the launch with a non-nil Args, so the wire carries `[]`
// like TS (`args: z.array(z.string())` is required).
func (a agentLaunch) withArgs() agentLaunch {
	if a.Args == nil {
		a.Args = []string{}
	}
	return a
}
