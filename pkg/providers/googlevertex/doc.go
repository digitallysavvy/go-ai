// Package googlevertex is the provider for Google Vertex AI. It serves Gemini
// models, partner models (Anthropic Claude through googlevertex/anthropic and
// xAI Grok through googlevertex/xai), embeddings, image generation, video
// generation, speech and transcription on Google Cloud, with Google Cloud
// authentication and regional endpoints.
//
// Create the provider, then ask it for a model and pass the model to
// ai.GenerateText or ai.StreamText:
//
//	p, err := googlevertex.New(googlevertex.Config{
//		Project:     os.Getenv("GOOGLE_VERTEX_PROJECT"),
//		Location:    os.Getenv("GOOGLE_VERTEX_LOCATION"),
//		AccessToken: os.Getenv("GOOGLE_VERTEX_ACCESS_TOKEN"),
//	})
//	if err != nil {
//		log.Fatal(err)
//	}
//	model, err := p.LanguageModel("gemini-3.1-flash-lite-preview")
//	if err != nil {
//		log.Fatal(err)
//	}
//
// Project and Location are required. Authenticate with one of AccessToken,
// AuthToken (a function that returns a fresh token) or TokenSource, or set
// APIKey for Vertex express mode. The Gemini request and streaming code that
// this package shares with package google lives in package gemini.
//
// Guide: https://goaisdk.com/docs/providers/google-vertex.
//
// Provenance: this package is the Go counterpart of @ai-sdk/google-vertex from
// the Vercel AI SDK for TypeScript.
package googlevertex
