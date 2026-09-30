// Package harnessutil ports the adapter helpers of `@ai-sdk/harness/utils`
// (ai@7.0.113) that are not part of the bridge transport: authentication
// environment resolution, AI Gateway auth, OAuth refresh, credential
// forwarding and brokering, skills and instructions materialization, shell
// quoting and client-app attribution.
//
// Native subscription credential readers (OS keychains) live in the
// subscription subpackage. Bridge transport helpers (SandboxChannel,
// waitForBridgeReady, bridge tokens, diagnostics) are part of the later bridge
// transport work.
package harnessutil
