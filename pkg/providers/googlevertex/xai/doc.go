// Package xai is the provider for xAI Grok models served through Google Vertex
// AI's Model-as-a-Service (MaaS) endpoint. It uses the OpenAI-compatible
// chat endpoint that Vertex exposes for Grok, with Google Cloud authentication.
//
//	p, err := xai.New(xai.Config{
//		Project:  os.Getenv("GOOGLE_VERTEX_PROJECT"),
//		Location: "global",
//	})
//	if err != nil {
//		log.Fatal(err)
//	}
//	model, err := p.LanguageModel(xai.ModelGrok420Reasoning)
//
// Project is required. Location defaults to "global". Authentication uses
// AccessToken, AuthToken or TokenSource when you set one, and falls back to
// Application Default Credentials otherwise.
//
// This package is separate from package xai (xAI's own API) and from
// package googlevertex. For the xAI API directly, use package xai.
//
// Guide: https://goaisdk.com/docs/providers/google-vertex.
//
// Provenance: this package is the Go counterpart of
// @ai-sdk/google-vertex/xai from the Vercel AI SDK for TypeScript. Like the
// TypeScript package, it builds on the shared OpenAI-compatible code rather
// than on the xAI provider, because the Vertex endpoint does not speak the
// Responses API.
package xai
