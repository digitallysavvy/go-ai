// Package middleware provides middleware functionality for wrapping AI models
// with additional behavior like default settings, parameter transformation,
// and operation wrapping.
//
// Middleware can be applied to individual models or to entire providers:
//
//	// Apply middleware to a single model
//	model := openai.New(openai.Config{APIKey: "..."}).LanguageModel("gpt-4")
//	wrapped := middleware.WrapLanguageModel(model, []*middleware.LanguageModelMiddleware{
//		middleware.DefaultSettingsMiddleware(&provider.GenerateOptions{
//			Temperature: floatPtr(0.7),
//		}),
//	}, nil, nil)
//
//	// Apply middleware to all models from a provider
//	provider := openai.New(openai.Config{APIKey: "..."})
//	wrapped := middleware.WrapProvider(provider,
//		[]*middleware.LanguageModelMiddleware{
//			middleware.DefaultSettingsMiddleware(&provider.GenerateOptions{
//				Temperature: floatPtr(0.7),
//			}),
//		},
//		[]*middleware.EmbeddingModelMiddleware{
//			middleware.DefaultEmbeddingSettingsMiddleware(&provider.EmbedOptions{
//				Headers: map[string]string{"Custom-Header": "value"},
//			}),
//		},
//	)
package middleware
