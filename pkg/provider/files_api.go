package provider

import (
	"context"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// FilesAPI mirrors the TypeScript FilesV4 upload surface.
type FilesAPI interface {
	UploadFile(ctx context.Context, opts types.UploadFileOptions) (*types.UploadFileResult, error)
}

// FilesProvider is a provider that exposes file upload capabilities.
type FilesProvider interface {
	Files() FilesAPI
}
