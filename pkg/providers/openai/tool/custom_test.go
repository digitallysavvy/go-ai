package tool

import (
	"encoding/json"
	"testing"
)

func TestNewCustomTool_NoOptions(t *testing.T) {
	ct := NewCustomTool("my-tool")
	if ct.Name != "my-tool" {
		t.Errorf("expected name %q, got %q", "my-tool", ct.Name)
	}
	if ct.Description != nil {
		t.Error("expected nil description")
	}
	if ct.Format != nil {
		t.Error("expected nil format")
	}
}

func TestNewCustomTool_WithDescription(t *testing.T) {
	ct := NewCustomTool("my-tool", WithDescription("does something"))
	if ct.Description == nil || *ct.Description != "does something" {
		t.Errorf("expected description %q", "does something")
	}
}

func TestNewCustomTool_WithGrammarFormat(t *testing.T) {
	syntax := "lark"
	def := `start: WORD`
	ct := NewCustomTool("grammar-tool", WithFormat(CustomToolFormat{
		Type:       "grammar",
		Syntax:     &syntax,
		Definition: &def,
	}))

	if ct.Format == nil {
		t.Fatal("expected non-nil format")
	}
	if ct.Format.Type != "grammar" {
		t.Errorf("expected type %q, got %q", "grammar", ct.Format.Type)
	}
	if ct.Format.Syntax == nil || *ct.Format.Syntax != "lark" {
		t.Error("expected syntax lark")
	}
	if ct.Format.Definition == nil || *ct.Format.Definition != `start: WORD` {
		t.Error("expected definition")
	}
}

func TestNewCustomTool_WithTextFormat(t *testing.T) {
	ct := NewCustomTool("text-tool", WithFormat(CustomToolFormat{
		Type: "text",
	}))

	if ct.Format == nil {
		t.Fatal("expected non-nil format")
	}
	if ct.Format.Type != "text" {
		t.Errorf("expected type %q, got %q", "text", ct.Format.Type)
	}
	if ct.Format.Syntax != nil {
		t.Error("expected nil syntax for text format")
	}
}

func TestCustomToolFormat_JSON_Grammar(t *testing.T) {
	syntax := "regex"
	def := `[0-9]+`
	format := CustomToolFormat{
		Type:       "grammar",
		Syntax:     &syntax,
		Definition: &def,
	}

	data, err := json.Marshal(format)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var got CustomToolFormat
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if got.Type != "grammar" {
		t.Errorf("type: got %q, want %q", got.Type, "grammar")
	}
	if got.Syntax == nil || *got.Syntax != "regex" {
		t.Error("syntax mismatch")
	}
	if got.Definition == nil || *got.Definition != `[0-9]+` {
		t.Error("definition mismatch")
	}
}

func TestCustomToolFormat_JSON_Text(t *testing.T) {
	format := CustomToolFormat{Type: "text"}

	data, err := json.Marshal(format)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	// syntax and definition should be omitted
	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal map failed: %v", err)
	}
	if _, ok := m["syntax"]; ok {
		t.Error("syntax should be omitted for text format")
	}
	if _, ok := m["definition"]; ok {
		t.Error("definition should be omitted for text format")
	}
	if m["type"] != "text" {
		t.Errorf("type: got %v, want text", m["type"])
	}
}

func TestCustomTool_JSON_RoundTrip(t *testing.T) {
	desc := "extract JSON"
	syntax := "lark"
	def := `start: WORD`

	original := CustomTool{
		Name:        "json-extractor",
		Description: &desc,
		Format: &CustomToolFormat{
			Type:       "grammar",
			Syntax:     &syntax,
			Definition: &def,
		},
	}

	data, err := json.Marshal(original)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var got CustomTool
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}

	if got.Name != original.Name {
		t.Errorf("name: got %q, want %q", got.Name, original.Name)
	}
	if got.Description == nil || *got.Description != *original.Description {
		t.Error("description mismatch")
	}
	if got.Format == nil {
		t.Fatal("format should not be nil")
	}
	if got.Format.Type != original.Format.Type {
		t.Errorf("format type: got %q, want %q", got.Format.Type, original.Format.Type)
	}
}

func TestCustomTool_JSON_NoFormat(t *testing.T) {
	ct := NewCustomTool("simple-tool")
	data, err := json.Marshal(ct)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var m map[string]interface{}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatalf("unmarshal map failed: %v", err)
	}
	if _, ok := m["format"]; ok {
		t.Error("format should be omitted when nil")
	}
	if _, ok := m["description"]; ok {
		t.Error("description should be omitted when nil")
	}
}

func TestCustomTool_ToTool(t *testing.T) {
	ct := NewCustomTool("my-tool", WithDescription("a tool"))
	sdkTool := ct.ToTool()

	if sdkTool.Name != "openai.custom" {
		t.Errorf("expected name %q, got %q", "openai.custom", sdkTool.Name)
	}
	if !sdkTool.ProviderExecuted {
		t.Error("expected ProviderExecuted to be true")
	}
	if sdkTool.ProviderOptions == nil {
		t.Error("expected ProviderOptions to be set")
	}
	// ProviderOptions should be the CustomTool itself
	ctOpts, ok := sdkTool.ProviderOptions.(CustomTool)
	if !ok {
		t.Errorf("ProviderOptions should be CustomTool, got %T", sdkTool.ProviderOptions)
	}
	if ctOpts.Name != "my-tool" {
		t.Errorf("ProviderOptions.Name: got %q, want %q", ctOpts.Name, "my-tool")
	}
}
