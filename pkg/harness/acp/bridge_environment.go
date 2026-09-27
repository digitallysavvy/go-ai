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
	Authentication                *Authentication      `json:"authentication,omitempty"`
	ProviderAuthentication        *bridgeProviderAuth  `json:"providerAuthentication,omitempty"`
	ProviderEnvironment           map[string]string    `json:"providerEnvironment,omitempty"`
	SessionMeta                   map[string]any       `json:"sessionMeta,omitempty"`
	ClientCapabilities            map[string]any       `json:"clientCapabilities,omitempty"`
	AskUserQuestionsRequestMethod string               `json:"askUserQuestionsRequestMethod,omitempty"`
	HostToolMCPTransport          HostToolMCPTransport `json:"hostToolMcpTransport,omitempty"`
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
