package luma

// Image model ID constants for Luma AI models.
// Any model ID is accepted by Provider.ImageModel; this list mirrors the
// TypeScript SDK's LumaImageModelId documentation set.
// See https://luma.ai/models?type=image
const (
	// ModelPhoton1 is Luma's Photon 1 image model.
	ModelPhoton1 = "photon-1"
	// ModelPhotonFlash1 is Luma's Photon Flash 1 image model.
	ModelPhotonFlash1 = "photon-flash-1"
)

// ReferenceType is the type of image reference to use when providing input
// images (TS LumaReferenceType).
type ReferenceType = string

const (
	// ReferenceTypeImage guides generation using reference images (up to 4). Default.
	ReferenceTypeImage ReferenceType = "image"
	// ReferenceTypeStyle applies a specific style from reference image(s).
	ReferenceTypeStyle ReferenceType = "style"
	// ReferenceTypeCharacter creates consistent characters from reference images (up to 4).
	ReferenceTypeCharacter ReferenceType = "character"
	// ReferenceTypeModifyImage transforms a single input image with prompt guidance.
	ReferenceTypeModifyImage ReferenceType = "modify_image"
)
