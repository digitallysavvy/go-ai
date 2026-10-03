// Package anthropic is the provider for Anthropic Claude models served through
// Google Vertex AI. It uses the Anthropic Messages API on Vertex and
// authenticates with Google Cloud credentials.
//
//	p := anthropic.New(anthropic.Options{
//		Project:  os.Getenv("GOOGLE_VERTEX_PROJECT"),
//		Location: "us-east5",
//	})
//	model, err := p.LanguageModel(string(anthropic.ClaudeSonnet5_5))
//	if err != nil {
//		log.Fatal(err)
//	}
//
// Pass model to ai.GenerateText or ai.StreamText. Project and Location default
// to the GOOGLE_VERTEX_PROJECT and GOOGLE_VERTEX_LOCATION environment
// variables. Credentials come from CredentialsFile, CredentialsJSON,
// TokenSource, AuthToken, or Application Default Credentials, in that order.
//
// Guide: https://goaisdk.com/docs/providers/google-vertex.
//
// Provenance: this package is the Go counterpart of
// @ai-sdk/google-vertex/anthropic from the Vercel AI SDK for TypeScript.
package anthropic
