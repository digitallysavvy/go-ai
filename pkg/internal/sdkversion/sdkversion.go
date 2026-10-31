// Package sdkversion holds the Go AI SDK release version in a leaf package, so
// low-level packages such as pkg/telemetry can report it without importing
// pkg/version (which depends on them). pkg/version.Version re-exports it.
package sdkversion

// Version is the Go AI SDK release version.
const Version = "0.6.0"
