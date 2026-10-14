package heygen

import (
	"encoding/json"
	"fmt"
	"net/url"
	"strings"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// Asset types accepted in HeyGen provider options.
const (
	AssetTypeURL     = "url"
	AssetTypeAssetID = "asset_id"
	AssetTypeBase64  = "base64"
)

// VideoAsset is an input asset: an HTTPS URL, an existing HeyGen asset ID, or
// inline base64 data (TS HeyGenVideoAsset).
type VideoAsset struct {
	// Type is "url", "asset_id", or "base64".
	Type string `json:"type"`
	// URL is the HTTPS URL for type "url".
	URL string `json:"url,omitempty"`
	// AssetID is the existing workspace asset for type "asset_id".
	AssetID string `json:"assetId,omitempty"`
	// MediaType is the media type for type "base64".
	MediaType string `json:"mediaType,omitempty"`
	// Data is the base64 payload for type "base64".
	Data string `json:"data,omitempty"`
}

// VideoModelOptions are the providerOptions.heygen options for video
// generation (TS HeyGenVideoModelOptions).
type VideoModelOptions struct {
	// Mode is inferred from the supplied image and references when empty:
	// "text_to_video", "image_to_video", or "reference_to_video".
	Mode string `json:"mode,omitempty"`
	// Resolution is the output size class ("480p", "768p", "1080p", "2k"). It
	// takes precedence over the standard resolution option.
	Resolution string `json:"resolution,omitempty"`
	// PromptEnhancement is "turbo", "quality", or "disabled". The API default
	// is turbo; use "disabled" to preserve the original prompt.
	PromptEnhancement string `json:"promptEnhancement,omitempty"`
	// Image is the first frame, including an existing HeyGen asset. It cannot
	// be combined with a standard image.
	Image *VideoAsset `json:"image,omitempty"`
	// ReferenceImages are appended after standard image references (max 9).
	ReferenceImages []VideoAsset `json:"referenceImages,omitempty"`
	// ReferenceVideos are appended after standard video references (max 3).
	ReferenceVideos []VideoAsset `json:"referenceVideos,omitempty"`
	// ReferenceAudio are appended after standard audio references (max 3).
	ReferenceAudio []VideoAsset `json:"referenceAudio,omitempty"`
}

func invalidOptions(msg string) error {
	return &providererrors.InvalidArgumentError{Field: "providerOptions", Message: "invalid heygen provider options: " + msg}
}

func isHTTPSURL(raw string) bool {
	if !strings.HasPrefix(raw, "https://") {
		return false
	}
	u, err := url.Parse(raw)
	return err == nil && u.Host != ""
}

func validateAssetShape(a VideoAsset, field string) error {
	switch a.Type {
	case AssetTypeURL:
		if !isHTTPSURL(a.URL) {
			return invalidOptions(field + ".url must be an absolute HTTPS URL")
		}
	case AssetTypeAssetID:
		if a.AssetID == "" {
			return invalidOptions(field + ".assetId must not be empty")
		}
	case AssetTypeBase64:
		if a.MediaType == "" || a.Data == "" {
			return invalidOptions(field + " base64 assets require mediaType and data")
		}
	default:
		return invalidOptions(field + ".type must be one of url, asset_id, base64")
	}
	return nil
}

// parseVideoModelOptions parses and validates providerOptions.heygen,
// mirroring parseProviderOptions with heygenVideoModelOptionsSchema. It
// returns nil options when no heygen options were supplied.
func parseVideoModelOptions(providerOptions map[string]interface{}) (*VideoModelOptions, error) {
	raw, ok := providerOptions["heygen"]
	if !ok || raw == nil {
		return nil, nil
	}
	if _, isMap := raw.(map[string]interface{}); !isMap {
		if typed, isTyped := raw.(*VideoModelOptions); isTyped {
			return validateVideoModelOptions(typed)
		}
		if typed, isTyped := raw.(VideoModelOptions); isTyped {
			return validateVideoModelOptions(&typed)
		}
		return nil, invalidOptions("expected an object")
	}
	data, err := json.Marshal(raw)
	if err != nil {
		return nil, invalidOptions(err.Error())
	}
	var opts VideoModelOptions
	if err := json.Unmarshal(data, &opts); err != nil {
		return nil, invalidOptions(err.Error())
	}
	return validateVideoModelOptions(&opts)
}

func validateVideoModelOptions(opts *VideoModelOptions) (*VideoModelOptions, error) {
	switch opts.Mode {
	case "", "text_to_video", "image_to_video", "reference_to_video":
	default:
		return nil, invalidOptions(fmt.Sprintf("mode %q is not one of text_to_video, image_to_video, reference_to_video", opts.Mode))
	}
	switch opts.Resolution {
	case "", "480p", "768p", "1080p", "2k":
	default:
		return nil, invalidOptions(fmt.Sprintf("resolution %q is not one of 480p, 768p, 1080p, 2k", opts.Resolution))
	}
	switch opts.PromptEnhancement {
	case "", "turbo", "quality", "disabled":
	default:
		return nil, invalidOptions(fmt.Sprintf("promptEnhancement %q is not one of turbo, quality, disabled", opts.PromptEnhancement))
	}
	if opts.Image != nil {
		if err := validateAssetShape(*opts.Image, "image"); err != nil {
			return nil, err
		}
	}
	for _, group := range []struct {
		name   string
		assets []VideoAsset
		max    int
	}{
		{"referenceImages", opts.ReferenceImages, 9},
		{"referenceVideos", opts.ReferenceVideos, 3},
		{"referenceAudio", opts.ReferenceAudio, 3},
	} {
		if len(group.assets) > group.max {
			return nil, invalidOptions(fmt.Sprintf("%s accepts at most %d assets", group.name, group.max))
		}
		for _, a := range group.assets {
			if err := validateAssetShape(a, group.name); err != nil {
				return nil, err
			}
		}
	}
	return opts, nil
}
