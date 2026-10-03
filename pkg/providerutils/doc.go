// Package providerutils holds helpers shared by provider implementations:
// request option parsing, OpenAI-compatible request and response mapping,
// response message conversion, header handling, URL and path encoding, model
// serialization, and warnings. Application code rarely imports it. Provider
// authors use it so that providers behave the same way.
//
// Subpackages cover prompt conversion (prompt), streaming and server-sent
// events (streaming), and tool conversion (tool).
package providerutils
