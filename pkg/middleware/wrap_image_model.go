package middleware

import (
	"context"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// ImageModelMiddleware defines middleware that can be applied to image models
// to transform parameters and wrap generate operations. Mirrors TypeScript's
// ImageModelV4Middleware (ai/packages/provider/src/image-model-middleware/v4/image-model-v4-middleware.ts).
type ImageModelMiddleware struct {
	// SpecificationVersion should be "v3" or "v4" for the current version
	SpecificationVersion string

	// OverrideProvider allows overriding the provider name
	OverrideProvider func(model provider.ImageModel) string

	// OverrideModelID allows overriding the model ID
	OverrideModelID func(model provider.ImageModel) string

	// OverrideMaxImagesPerCall allows overriding the limit of how many images
	// can be generated in a single API call.
	OverrideMaxImagesPerCall func(model provider.ImageModel) int

	// OverrideSupportsFileInputs allows overriding whether the model
	// supports image file inputs for image editing. Return nil to advertise
	// unknown support explicitly. A nil field (no override configured, as
	// opposed to a configured override that returns nil) falls through to
	// the wrapped model's own capability.
	OverrideSupportsFileInputs func(model provider.ImageModel) *bool

	// OverrideSupportsMaskInputs allows overriding whether the model
	// supports mask inputs for image editing. Same nil-handling as
	// OverrideSupportsFileInputs.
	OverrideSupportsMaskInputs func(model provider.ImageModel) *bool

	// TransformParams transforms the parameters before they are passed to the image model
	TransformParams func(ctx context.Context, params *provider.ImageGenerateOptions, model provider.ImageModel) (*provider.ImageGenerateOptions, error)

	// WrapGenerate wraps the generate operation of the image model
	WrapGenerate func(ctx context.Context, doGenerate func() (*types.ImageResult, error), params *provider.ImageGenerateOptions, model provider.ImageModel) (*types.ImageResult, error)
}

// wrappedImageModel wraps an ImageModel with middleware
type wrappedImageModel struct {
	model      provider.ImageModel
	middleware *ImageModelMiddleware
	modelID    *string
	providerID *string
}

// WrapImageModel wraps an ImageModel instance with middleware functionality.
// This function allows you to apply middleware to transform parameters and
// wrap generate operations of an image model.
//
// When multiple middlewares are provided, the first middleware will transform
// the input first, and the last middleware will be wrapped directly around the model.
func WrapImageModel(model provider.ImageModel, middleware []*ImageModelMiddleware, modelID, providerID *string) provider.ImageModel {
	if len(middleware) == 0 {
		return model
	}

	// Apply middleware in reverse order (last middleware wraps directly around model)
	wrappedModel := model
	for i := len(middleware) - 1; i >= 0; i-- {
		wrappedModel = doWrapImageModel(wrappedModel, middleware[i], modelID, providerID)
	}
	return wrappedModel
}

func doWrapImageModel(model provider.ImageModel, middleware *ImageModelMiddleware, modelID, providerID *string) provider.ImageModel {
	return &wrappedImageModel{
		model:      model,
		middleware: middleware,
		modelID:    modelID,
		providerID: providerID,
	}
}

// SpecificationVersion returns the specification version
func (w *wrappedImageModel) SpecificationVersion() string {
	return w.model.SpecificationVersion()
}

// Provider returns the provider name
func (w *wrappedImageModel) Provider() string {
	if w.providerID != nil {
		return *w.providerID
	}
	if w.middleware.OverrideProvider != nil {
		return w.middleware.OverrideProvider(w.model)
	}
	return w.model.Provider()
}

// ModelID returns the model ID
func (w *wrappedImageModel) ModelID() string {
	if w.modelID != nil {
		return *w.modelID
	}
	if w.middleware.OverrideModelID != nil {
		return w.middleware.OverrideModelID(w.model)
	}
	return w.model.ModelID()
}

// imageModelMaxImagesPerCall is the optional capability interface implemented
// by image models that expose a per-call image generation limit. Matches the
// pattern generate_image.go's resolveMaxImagesPerCall uses to type-assert
// provider.ImageModel for TS's model.maxImagesPerCall.
type imageModelMaxImagesPerCall interface {
	MaxImagesPerCall() int
}

// MaxImagesPerCall returns the maximum number of images that can be
// generated in a single API call. Returns 0 (no override/no limit known) when
// neither the middleware nor the wrapped model expose one, matching how
// pkg/ai/generate_image.go's resolveMaxImagesPerCall treats a non-positive
// value as "no override" and falls through to its own defaults.
func (w *wrappedImageModel) MaxImagesPerCall() int {
	if w.middleware.OverrideMaxImagesPerCall != nil {
		return w.middleware.OverrideMaxImagesPerCall(w.model)
	}
	if m, ok := w.model.(imageModelMaxImagesPerCall); ok {
		return m.MaxImagesPerCall()
	}
	return 0
}

// SupportsFileInputs reports whether the wrapped model (after any override)
// supports image file inputs for image editing. Mirrors TS doWrap's
// `overrideSupportsFileInputs !== undefined ? overrideSupportsFileInputs({model}) : model.supportsFileInputs`.
func (w *wrappedImageModel) SupportsFileInputs() *bool {
	if w.middleware.OverrideSupportsFileInputs != nil {
		return w.middleware.OverrideSupportsFileInputs(w.model)
	}
	return provider.ImageModelSupportsFileInputs(w.model)
}

// SupportsMaskInputs reports whether the wrapped model (after any override)
// supports mask inputs for image editing.
func (w *wrappedImageModel) SupportsMaskInputs() *bool {
	if w.middleware.OverrideSupportsMaskInputs != nil {
		return w.middleware.OverrideSupportsMaskInputs(w.model)
	}
	return provider.ImageModelSupportsMaskInputs(w.model)
}

// DoGenerate performs image generation
func (w *wrappedImageModel) DoGenerate(ctx context.Context, opts *provider.ImageGenerateOptions) (*types.ImageResult, error) {
	// Transform parameters if middleware provides transformParams
	transformedOpts := opts
	if w.middleware.TransformParams != nil {
		var err error
		transformedOpts, err = w.middleware.TransformParams(ctx, opts, w.model)
		if err != nil {
			return nil, err
		}
	}

	// Create the doGenerate function
	doGenerate := func() (*types.ImageResult, error) {
		return w.model.DoGenerate(ctx, transformedOpts)
	}

	// Wrap generate if middleware provides wrapGenerate
	if w.middleware.WrapGenerate != nil {
		return w.middleware.WrapGenerate(ctx, doGenerate, transformedOpts, w.model)
	}

	return doGenerate()
}
