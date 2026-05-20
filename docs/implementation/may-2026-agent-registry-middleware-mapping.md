# May 2026 Agent Registry Middleware Mapping

| TypeScript source | Go package | Mapping notes |
| --- | --- | --- |
| `ai/packages/ai/src/agent/tool-loop-agent.ts` | `pkg/agent` | `RuntimeContext`, `ToolsContext`, `Prompt`, `FilterActiveTools`, `CallOptionsSchema`, approval statuses, invalid tool-call preservation, and `MaxSteps` to `StopWhen` compatibility are implemented. |
| `ai/packages/workflow/src/workflow-agent.ts` | `pkg/agent` | Agent ID, prompt, step finish enrichment, provider-executed tool handling, and invalid tool-call error preservation are mapped onto `ToolLoopAgent` APIs. |
| `ai/packages/ai/src/registry/provider-registry.ts` | `pkg/registry` | Language, embedding, image, speech, transcription, reranking, video, files, and skills resolution are exposed. Unsupported files/skills use `NoSuchProviderError`-equivalent errors. |
| `ai/packages/ai/src/registry/custom-provider.ts` | `pkg/registry` | Custom providers can expose partial model families and optional upload APIs without nil panics. |
| `ai/packages/ai/src/middleware/wrap-language-model.ts` | `pkg/middleware` | Wrapped language models preserve the underlying specification version and forward transformed params/results. |
| `ai/packages/ai/src/middleware/wrap-provider.ts` | `pkg/middleware` | Provider wrappers preserve upload API access and pass non-language model families through. |
| `ai/packages/provider-utils/src/validate-types.ts` | `pkg/schema` | Runtime schema validation is used for agent call options and tool context with schema path details in errors. |
