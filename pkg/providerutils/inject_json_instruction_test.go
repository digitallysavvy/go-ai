package providerutils

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// Ported from ai/packages/provider-utils/src/inject-json-instruction.test.ts

var basicSchema = map[string]interface{}{
	"type": "object",
	"properties": map[string]interface{}{
		"name": map[string]interface{}{"type": "string"},
		"age":  map[string]interface{}{"type": "number"},
	},
	"required": []interface{}{"name", "age"},
}

const basicSchemaJSON = `{"properties":{"age":{"type":"number"},"name":{"type":"string"}},"required":["name","age"],"type":"object"}`

func TestInjectJSONInstruction_PromptAndSchema(t *testing.T) {
	got := InjectJSONInstruction("Generate a person", basicSchema, InjectJSONInstructionOptions{})
	want := "Generate a person\n\n" +
		"JSON schema:\n" +
		basicSchemaJSON + "\n" +
		"You MUST answer with a JSON object that matches the JSON schema above."
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}
}

func TestInjectJSONInstruction_PromptOnly(t *testing.T) {
	got := InjectJSONInstruction("Generate a person", nil, InjectJSONInstructionOptions{})
	want := "Generate a person\n\nYou MUST answer with JSON."
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestInjectJSONInstruction_SchemaOnly(t *testing.T) {
	got := InjectJSONInstruction("", basicSchema, InjectJSONInstructionOptions{})
	want := "JSON schema:\n" + basicSchemaJSON + "\n" +
		"You MUST answer with a JSON object that matches the JSON schema above."
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestInjectJSONInstruction_NoPromptNoSchema(t *testing.T) {
	got := InjectJSONInstruction("", nil, InjectJSONInstructionOptions{})
	if got != "You MUST answer with JSON." {
		t.Fatalf("got %q", got)
	}
}

func TestInjectJSONInstruction_CustomPrefixSuffix(t *testing.T) {
	got := InjectJSONInstruction("Generate a person", basicSchema, InjectJSONInstructionOptions{
		SchemaPrefix: "Custom prefix:",
		SchemaSuffix: "Custom suffix",
	})
	want := "Generate a person\n\nCustom prefix:\n" + basicSchemaJSON + "\nCustom suffix"
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestInjectJSONInstruction_EmptyObjectSchema(t *testing.T) {
	got := InjectJSONInstruction("Generate something", map[string]interface{}{}, InjectJSONInstructionOptions{})
	want := "Generate something\n\nJSON schema:\n{}\nYou MUST answer with a JSON object that matches the JSON schema above."
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestInjectJSONInstructionIntoMessages_BasicSystemMessage(t *testing.T) {
	messages := []types.Message{
		{Role: types.RoleSystem, Content: []types.ContentPart{types.TextContent{Text: "Generate a person"}}},
	}
	result := InjectJSONInstructionIntoMessages(messages, basicSchema, InjectJSONInstructionOptions{})
	if len(result) != 1 || result[0].Role != types.RoleSystem {
		t.Fatalf("unexpected result: %+v", result)
	}
	got := result[0].Content[0].(types.TextContent).Text
	want := "Generate a person\n\nJSON schema:\n" + basicSchemaJSON + "\n" +
		"You MUST answer with a JSON object that matches the JSON schema above."
	if got != want {
		t.Fatalf("got:\n%s\nwant:\n%s", got, want)
	}

	// Must not mutate the input.
	if messages[0].Content[0].(types.TextContent).Text != "Generate a person" {
		t.Fatal("input messages were mutated")
	}
}

func TestInjectJSONInstructionIntoMessages_EmptyMessages(t *testing.T) {
	result := InjectJSONInstructionIntoMessages(nil, basicSchema, InjectJSONInstructionOptions{})
	if len(result) != 1 || result[0].Role != types.RoleSystem {
		t.Fatalf("unexpected result: %+v", result)
	}
}

func TestInjectJSONInstructionIntoMessages_NoLeadingSystemMessage(t *testing.T) {
	messages := []types.Message{
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "Hello"}}},
		{Role: types.RoleAssistant, Content: []types.ContentPart{types.TextContent{Text: "Hi there"}}},
	}
	result := InjectJSONInstructionIntoMessages(messages, basicSchema, InjectJSONInstructionOptions{})
	if len(result) != 3 {
		t.Fatalf("expected a prepended system message, got %d messages", len(result))
	}
	if result[0].Role != types.RoleSystem {
		t.Fatalf("expected leading system message, got %v", result[0].Role)
	}
	if result[1].Role != types.RoleUser || result[2].Role != types.RoleAssistant {
		t.Fatalf("original messages not preserved in order: %+v", result[1:])
	}
}

func TestInjectJSONInstructionIntoMessages_EmptySystemContent(t *testing.T) {
	messages := []types.Message{
		{Role: types.RoleSystem, Content: []types.ContentPart{}},
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "Generate data"}}},
	}
	result := InjectJSONInstructionIntoMessages(messages, basicSchema, InjectJSONInstructionOptions{})
	got := result[0].Content[0].(types.TextContent).Text
	want := "JSON schema:\n" + basicSchemaJSON + "\n" +
		"You MUST answer with a JSON object that matches the JSON schema above."
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestInjectJSONInstructionIntoMessages_PreservesNonSystemMessages(t *testing.T) {
	messages := []types.Message{
		{Role: types.RoleSystem, Content: []types.ContentPart{types.TextContent{Text: "You are helpful"}}},
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "Hello"}}},
		{Role: types.RoleAssistant, Content: []types.ContentPart{types.TextContent{Text: "Hi"}}},
		{Role: types.RoleUser, Content: []types.ContentPart{types.TextContent{Text: "Generate person"}}},
	}
	result := InjectJSONInstructionIntoMessages(messages, basicSchema, InjectJSONInstructionOptions{})
	if len(result) != 4 {
		t.Fatalf("expected 4 messages, got %d", len(result))
	}
	for i, role := range []types.MessageRole{types.RoleSystem, types.RoleUser, types.RoleAssistant, types.RoleUser} {
		if result[i].Role != role {
			t.Fatalf("message %d: role = %v, want %v", i, result[i].Role, role)
		}
	}
}

func TestInjectJSONInstructionIntoMessages_NoSchema(t *testing.T) {
	messages := []types.Message{
		{Role: types.RoleSystem, Content: []types.ContentPart{types.TextContent{Text: "Generate data"}}},
	}
	result := InjectJSONInstructionIntoMessages(messages, nil, InjectJSONInstructionOptions{})
	got := result[0].Content[0].(types.TextContent).Text
	want := "Generate data\n\nYou MUST answer with JSON."
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestInjectJSONInstructionIntoMessages_CustomPrefixSuffix(t *testing.T) {
	messages := []types.Message{
		{Role: types.RoleSystem, Content: []types.ContentPart{types.TextContent{Text: "Generate data"}}},
	}
	result := InjectJSONInstructionIntoMessages(messages, basicSchema, InjectJSONInstructionOptions{
		SchemaPrefix: "Custom schema:",
		SchemaSuffix: "Follow this format exactly.",
	})
	got := result[0].Content[0].(types.TextContent).Text
	want := "Generate data\n\nCustom schema:\n" + basicSchemaJSON + "\nFollow this format exactly."
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}
