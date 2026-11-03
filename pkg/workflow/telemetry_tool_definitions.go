package workflow

import (
	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/internal/jsonorder"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// withTelemetryToolDefinitions returns a copy of internal whose language-model
// call start events carry TS WorkflowAgent's serialized tool definitions
// (model-call-iterator.ts 367: `Object.values(serializeToolSet(effectiveTools))`),
// built from the step's prepared tools. originals are the run's tools before
// schema resolution, which tell whether a tool has its own validator.
func withTelemetryToolDefinitions(internal *ai.InternalOptions, originals []types.Tool) *ai.InternalOptions {
	out := ai.InternalOptions{}
	if internal != nil {
		out = *internal
	}
	byName := make(map[string]types.Tool, len(originals))
	for _, t := range originals {
		byName[t.Name] = t
	}
	out.TelemetryToolDefinitions = func(tools []types.Tool) interface{} {
		return telemetryToolDefinitions(tools, byName)
	}
	return &out
}

// telemetryToolDefinitions is TS serializeToolSet's definitions as
// JSON.stringify writes them: in TS key order, absent values left out, and
// no tool name. A step without tools gives an empty list ("[]").
func telemetryToolDefinitions(tools []types.Tool, originals map[string]types.Tool) []*jsonorder.Object {
	defs := make([]*jsonorder.Object, 0, len(tools))
	for _, t := range tools {
		def := jsonorder.New()
		if t.Title != "" {
			def.Set("title", t.Title)
		}
		if t.Metadata != nil {
			def.Set("metadata", t.Metadata)
		}
		if t.Description != "" {
			def.Set("description", t.Description)
		}
		// asSchema(inputSchema).jsonSchema: the resolved schema, or TS
		// asSchema(undefined) for a tool without one.
		if t.Parameters != nil {
			def.Set("inputSchema", t.Parameters)
		} else {
			empty := jsonorder.New()
			empty.Set("type", "object")
			empty.Set("properties", jsonorder.New())
			empty.Set("additionalProperties", false)
			def.Set("inputSchema", empty)
		}
		if t.Strict != nil {
			def.Set("strict", *t.Strict)
		}
		if len(t.InputExamples) > 0 {
			examples := make([]map[string]interface{}, 0, len(t.InputExamples))
			for _, ex := range t.InputExamples {
				examples = append(examples, map[string]interface{}{"input": ex.Input})
			}
			def.Set("inputExamples", examples)
		}
		if t.ProviderOptions != nil {
			def.Set("providerOptions", t.ProviderOptions)
		}
		if orig, ok := originals[t.Name]; ok {
			if _, hasOwnValidator, err := serializeToolParameters(orig.Parameters); err == nil && hasOwnValidator {
				def.Set("hasOwnValidator", true)
			}
		}
		if t.Type == "dynamic" {
			def.Set("type", "dynamic")
		}
		if t.OnInputStart != nil {
			def.Set("hasOnInputStart", true)
		}
		if t.OnInputDelta != nil {
			def.Set("hasOnInputDelta", true)
		}
		if t.OnInputAvailable != nil {
			def.Set("hasOnInputAvailable", true)
		}
		if t.Type == "provider" {
			def.Set("type", "provider")
			def.Set("isProviderExecuted", t.ProviderExecuted)
			if t.SupportsDeferredResults {
				def.Set("supportsDeferredResults", true)
			}
			def.Set("id", t.ProviderID)
			if t.ProviderArgs != nil {
				def.Set("args", t.ProviderArgs)
			}
		}
		defs = append(defs, def)
	}
	return defs
}
