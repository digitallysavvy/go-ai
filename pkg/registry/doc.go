// Package registry resolves models by name. Register providers once, then ask
// for a model with a single "provider:model" string, for example
// "openai:gpt-6-astra". This lets configuration files and environment
// variables choose the model without code changes.
//
// Use NewRegistry for an isolated registry, or the package-level functions
// (RegisterProvider, ResolveLanguageModel and so on) for the global one.
//
//	reg := registry.NewRegistry()
//	reg.RegisterProvider("openai", openai.New(openai.Config{APIKey: os.Getenv("OPENAI_API_KEY")}))
//
//	model, err := reg.ResolveLanguageModel("openai:gpt-6-astra")
//
// RegisterAlias maps a short name to a full ID, and WithSeparator changes the
// ":" separator. NewCustomProvider builds a provider from fixed maps of models,
// which is useful for tests and for routing several providers behind one name.
//
// Reference: https://goaisdk.com/docs/reference/registry/provider-registry.
//
// This package is the Go counterpart of createProviderRegistry and
// customProvider in the Vercel AI SDK for TypeScript.
package registry
