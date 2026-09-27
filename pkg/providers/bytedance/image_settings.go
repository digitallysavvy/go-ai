package bytedance

// ByteDanceImageModelID is a ByteDance (Seedream) image generation model
// identifier.
type ByteDanceImageModelID string

const (
	// ModelDolaSeedream50Pro is the Dola Seedream 5.0 Pro image model.
	ModelDolaSeedream50Pro ByteDanceImageModelID = "dola-seedream-5-0-pro-260628"

	// ModelSeedream50 is the Seedream 5.0 image model.
	ModelSeedream50 ByteDanceImageModelID = "seedream-5-0-260128"

	// ModelSeedream50Lite is the Seedream 5.0 Lite image model.
	ModelSeedream50Lite ByteDanceImageModelID = "seedream-5-0-lite-260128"

	// ModelSeedream45 is the Seedream 4.5 image model.
	ModelSeedream45 ByteDanceImageModelID = "seedream-4-5-251128"

	// ModelSeedream40 is the Seedream 4.0 image model.
	ModelSeedream40 ByteDanceImageModelID = "seedream-4-0-250828"
)
