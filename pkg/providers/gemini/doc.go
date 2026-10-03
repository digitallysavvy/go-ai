// Package gemini holds the Gemini request, response and streaming code that
// package google (Google AI Studio) and package googlevertex (Vertex AI) share.
// Most applications use one of those two packages and never import this one.
//
// LanguageModel implements provider.LanguageModel for the Gemini wire format.
// A Config injects what differs between the two services: the request paths,
// authentication, metadata keys and capability checks. The package also
// contains the shared tool preparation, reasoning (thinking) budget mapping,
// schema conversion and JSON accumulation for streamed function calls.
//
// To use Gemini models, see https://goaisdk.com/docs/providers/google and
// https://goaisdk.com/docs/providers/google-vertex.
package gemini
