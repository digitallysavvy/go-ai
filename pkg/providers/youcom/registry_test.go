package youcom

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/registry"
)

func TestRegisterTools(t *testing.T) {
	r := registry.NewRegistry()
	if err := RegisterTools(r); err != nil {
		t.Fatal(err)
	}

	entry, err := r.LookupTool(ToolIDSearch)
	if err != nil {
		t.Fatal(err)
	}
	if entry.Kind != registry.ToolKindLocal {
		t.Fatalf("Kind = %q, want %q", entry.Kind, registry.ToolKindLocal)
	}
	if entry.ProviderName != ProviderName {
		t.Fatalf("ProviderName = %q, want %q", entry.ProviderName, ProviderName)
	}
	if entry.ProviderMetadata[ProviderKey] == nil {
		t.Fatalf("ProviderMetadata missing %q key: %#v", ProviderKey, entry.ProviderMetadata)
	}
	if entry.Factory == nil {
		t.Fatal("Factory is nil")
	}
	tool := entry.Factory()
	if tool.ProviderExecuted || tool.Name != ToolIDSearch {
		t.Fatalf("unexpected factory tool: %#v", tool)
	}
}

func TestRegistryEntryMetadataIsCopied(t *testing.T) {
	r := registry.NewRegistry()
	if err := RegisterTools(r); err != nil {
		t.Fatal(err)
	}

	entry, err := r.LookupTool(ToolIDResearch)
	if err != nil {
		t.Fatal(err)
	}
	entry.ProviderMetadata[ProviderKey] = "changed"

	again, err := r.LookupTool(ToolIDResearch)
	if err != nil {
		t.Fatal(err)
	}
	if again.ProviderMetadata[ProviderKey] == "changed" {
		t.Fatal("registry returned mutable provider metadata")
	}
}
