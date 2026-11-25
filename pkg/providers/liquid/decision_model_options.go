package liquid

// DecisionModelOptions are the Liquid decision provider options
// (providerOptions["liquid"]). Liquid decision models do not support
// provider-specific options, so the struct has no fields; every key a caller
// passes is reported as an unsupported-option warning. Mirrors TS
// LiquidDecisionModelOptions (Record<string, never>).
type DecisionModelOptions struct{}
