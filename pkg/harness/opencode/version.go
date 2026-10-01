package opencode

// Version is reported in the `AI_SDK_HARNESS_CLIENT_APP` / User-Agent value
// this adapter sends to the sandbox bridge (TS `VERSION` from
// harness-opencode/src/version.ts). Pinned to the embedded bridge's source
// package version (see pkg/harness/bridges.VERSIONS.json,
// adapters.opencode.sourceVersion).
const Version = "1.0.125"
