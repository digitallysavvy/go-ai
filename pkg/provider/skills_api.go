package provider

import (
	"context"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// SkillsAPI mirrors the TypeScript SkillsV4 upload surface.
type SkillsAPI interface {
	UploadSkill(ctx context.Context, opts types.UploadSkillOptions) (*types.UploadSkillResult, error)
}

// SkillsProvider is a provider that exposes skill upload capabilities.
type SkillsProvider interface {
	Skills() SkillsAPI
}
