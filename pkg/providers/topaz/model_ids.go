package topaz

// Topaz image model ids.
//
// The AI SDK exposes a readable id that is mapped onto the name the Topaz
// API expects in the `model` form field (see topazImageAPIModelIDs). Passing
// a raw Topaz name such as "Wonder 3.5" also works - unknown ids are
// forwarded to the API unchanged.
//
// https://developer.topazlabs.com/image-models/wonder/wonder-3.5-new
const ImageModelWonder35 = "wonder-3.5"

// topazImageAPIModelIDs maps AI SDK image model ids onto Topaz API model
// names.
var topazImageAPIModelIDs = map[string]string{
	ImageModelWonder35: "Wonder 3.5",
}

// resolveTopazImageAPIModelID maps an AI SDK image model id onto the Topaz
// API model name. Unknown ids are forwarded unchanged.
func resolveTopazImageAPIModelID(modelID string) string {
	if resolved, ok := topazImageAPIModelIDs[modelID]; ok {
		return resolved
	}
	return modelID
}

// Topaz video model ids.
//
// The AI SDK exposes readable ids that are mapped onto the short names the
// Topaz API expects in the `filters[].model` field (see
// topazVideoAPIModelIDs). Passing a raw Topaz name such as "slp-2.6" also
// works - unknown ids are forwarded to the API unchanged.
//
// https://developer.topazlabs.com/video-models/proteus/proteus-1
// https://developer.topazlabs.com/video-models/starlight/starlight-precise-2.6
const (
	VideoModelProteus            = "proteus"
	VideoModelStarlightPrecise26 = "starlight-precise-2.6"
)

// topazVideoAPIModelIDs maps AI SDK video model ids onto Topaz API model
// names.
var topazVideoAPIModelIDs = map[string]string{
	VideoModelProteus:            "prob-4",
	VideoModelStarlightPrecise26: "slp-2.6",
}

// resolveTopazVideoAPIModelID maps an AI SDK video model id onto the Topaz
// API model name. Unknown ids are forwarded unchanged.
func resolveTopazVideoAPIModelID(modelID string) string {
	if resolved, ok := topazVideoAPIModelIDs[modelID]; ok {
		return resolved
	}
	return modelID
}
