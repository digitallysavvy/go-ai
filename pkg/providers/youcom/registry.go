package youcom

import (
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/registry"
)

// ToolEntries returns registry entries for the TypeScript-parity You.com tools.
func ToolEntries() []registry.ToolEntry {
	return []registry.ToolEntry{
		{
			Name:             ToolIDSearch,
			Kind:             registry.ToolKindLocal,
			ProviderName:     ProviderName,
			ProviderMetadata: ProviderMetadata(ToolIDSearch),
			Factory: func() types.Tool {
				return YouSearch()
			},
		},
		{
			Name:             ToolIDResearch,
			Kind:             registry.ToolKindLocal,
			ProviderName:     ProviderName,
			ProviderMetadata: ProviderMetadata(ToolIDResearch),
			Factory: func() types.Tool {
				return YouResearch()
			},
		},
		{
			Name:             ToolIDContents,
			Kind:             registry.ToolKindLocal,
			ProviderName:     ProviderName,
			ProviderMetadata: ProviderMetadata(ToolIDContents),
			Factory: func() types.Tool {
				return YouContents()
			},
		},
	}
}

// RegisterTools registers all You.com tools with the supplied registry.
func RegisterTools(r *registry.Registry) error {
	for _, entry := range ToolEntries() {
		if err := r.RegisterTool(entry); err != nil {
			return err
		}
	}
	return nil
}
