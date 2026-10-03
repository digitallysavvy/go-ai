// Package mantle is the provider for the Amazon Bedrock Mantle endpoint, which
// serves open-weight models through OpenAI-compatible APIs. It authenticates
// with a Bedrock API key or signs requests with AWS credentials.
//
// Create the provider, then ask it for a model and pass the model to
// ai.GenerateText or ai.StreamText:
//
//	p := mantle.New(mantle.ProviderSettings{Region: "us-east-1"})
//	model, err := p.LanguageModel("openai.gpt-oss-120b")
//	if err != nil {
//		log.Fatal(err)
//	}
//
// Guide: https://goaisdk.com/docs/providers/bedrock.
package mantle
