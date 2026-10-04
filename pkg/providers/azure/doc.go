// Package azure is the provider for Azure OpenAI Service. The model ID you
// pass is the name of your Azure deployment. It supports chat, the Responses
// API, embeddings, image generation, speech and transcription.
//
// Create the provider, then ask it for a model and pass the model to
// ai.GenerateText or ai.StreamText:
//
//	p, err := azure.New(azure.Config{
//		ResourceName: os.Getenv("AZURE_RESOURCE_NAME"),
//		APIKey:       os.Getenv("AZURE_API_KEY"),
//	})
//	if err != nil {
//		log.Fatal(err)
//	}
//	model, err := p.LanguageModel("my-deployment")
//	if err != nil {
//		log.Fatal(err)
//	}
//
// Set ADTokenProvider to authenticate with Microsoft Entra ID instead of an
// API key.
//
// Guide: https://goaisdk.com/docs/providers/azure.
package azure
