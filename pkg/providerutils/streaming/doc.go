// Package streaming provides the building blocks that provider streams share:
// a server-sent events parser and writer, OpenAICompatStream (the stream base
// for OpenAI-compatible providers, which buffers tool calls and emits them when
// the model finishes), tool call trackers, and warnings streams. Provider
// authors use it. Application code rarely needs it.
//
// New OpenAI-compatible providers should embed OpenAICompatStream instead of
// emitting a tool call per delta.
package streaming
