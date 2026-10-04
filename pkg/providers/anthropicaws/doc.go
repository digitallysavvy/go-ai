// Package anthropicaws is the provider for Anthropic Claude models served
// through Anthropic on AWS. It uses the Messages API and authenticates with an
// Anthropic workspace API key or with AWS credentials.
//
// Create the provider, then ask it for a model and pass the model to
// ai.GenerateText or ai.StreamText:
//
//	p, err := anthropicaws.New(anthropicaws.Config{
//		Region:      "us-east-1",
//		WorkspaceID: os.Getenv("ANTHROPIC_AWS_WORKSPACE_ID"),
//		APIKey:      os.Getenv("ANTHROPIC_AWS_API_KEY"),
//	})
//	if err != nil {
//		log.Fatal(err)
//	}
//	model, err := p.LanguageModel("claude-sonnet-5-5")
//	if err != nil {
//		log.Fatal(err)
//	}
//
// Without APIKey, the provider signs requests with AccessKeyID and
// SecretAccessKey, or with the AWS credentials in the environment. Models and
// options match package anthropic.
//
// Guide: https://goaisdk.com/docs/providers/anthropic.
package anthropicaws
