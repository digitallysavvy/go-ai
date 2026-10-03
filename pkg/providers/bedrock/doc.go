// Package bedrock is the provider for Amazon Bedrock. It reaches models from
// Anthropic, Amazon, Meta, Cohere and others through one API, using the
// Converse API for chat. It also provides embeddings, image generation and
// reranking.
//
// Create the provider, then ask it for a model and pass the model to
// ai.GenerateText or ai.StreamText:
//
//	p := bedrock.New(bedrock.Config{
//		Region: "us-east-1",
//	})
//	model, err := p.LanguageModel("anthropic.claude-sonnet-4-5-20250929-v1:0")
//	if err != nil {
//		log.Fatal(err)
//	}
//
// Credentials come from Config (an API key, or access keys), a
// CredentialProvider, or the standard AWS environment variables and shared
// credentials file. Subpackages bedrock/anthropic and bedrock/mantle cover the
// native Anthropic Messages API and the Bedrock Mantle endpoint.
//
// Guide: https://goaisdk.com/docs/providers/bedrock.
package bedrock
