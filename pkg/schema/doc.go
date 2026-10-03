// Package schema describes the shape of data the model must produce or accept:
// the output of ai.GenerateObject, the input of a tool, or the context of a
// tool call. A Schema holds a JSON Schema for the provider and a Validator that
// checks the model's output.
//
// The simplest way to build one is from a JSON Schema map:
//
//	recipe := schema.NewSimpleJSONSchema(map[string]interface{}{
//		"type": "object",
//		"properties": map[string]interface{}{
//			"name": map[string]interface{}{"type": "string"},
//		},
//		"required": []string{"name"},
//	})
//
// Pass the result as the Schema field of ai.GenerateObjectOptions. For Go
// structs, see SimpleStructSchema and StructValidator. ApplyDefaults fills in
// default values from a schema.
//
// Guide: https://goaisdk.com/docs/ai-sdk-core/generating-structured-data.
package schema
