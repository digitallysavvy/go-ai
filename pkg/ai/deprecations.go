package ai

import (
	"fmt"
	"strings"
	"unicode/utf16"
)

// deprecationCodes are stable identifiers for built-in deprecations. Do not
// rename or reuse an existing code. Mirrors TS logger/deprecations.ts.
var deprecationCodes = map[string]string{
	"generateObject":                                       "AISDK_DEP_GENERATE_OBJECT",
	"streamObject":                                         "AISDK_DEP_STREAM_OBJECT",
	"experimental_generateSpeech":                          "AISDK_DEP_EXPERIMENTAL_GENERATE_SPEECH",
	"experimental_transcribe":                              "AISDK_DEP_EXPERIMENTAL_TRANSCRIBE",
	`"image" content part`:                                 "AISDK_DEP_IMAGE_CONTENT_PART",
	`"tool-result" content of type "file-data"`:            "AISDK_DEP_TOOL_RESULT_FILE_DATA",
	`"tool-result" content of type "file-url"`:             "AISDK_DEP_TOOL_RESULT_FILE_URL",
	`"tool-result" content of type "file-id"`:              "AISDK_DEP_TOOL_RESULT_FILE_ID",
	`"tool-result" content of type "file-reference"`:       "AISDK_DEP_TOOL_RESULT_FILE_REFERENCE",
	`"tool-result" content of type "image-data"`:           "AISDK_DEP_TOOL_RESULT_IMAGE_DATA",
	`"tool-result" content of type "image-url"`:            "AISDK_DEP_TOOL_RESULT_IMAGE_URL",
	`"tool-result" content of type "image-file-id"`:        "AISDK_DEP_TOOL_RESULT_IMAGE_FILE_ID",
	`"tool-result" content of type "image-file-reference"`: "AISDK_DEP_TOOL_RESULT_IMAGE_FILE_REFERENCE",
	"rawInput in output-error UI message parts":            "AISDK_DEP_UI_MESSAGE_RAW_INPUT",
}

// encodeDeprecationCodePart escapes every non-alphanumeric UTF-16 code unit
// as `_XXXX` (uppercase hex) without losing case or collapsing distinct
// settings. Underscores are escaped too, so `__` separates code parts.
func encodeDeprecationCodePart(value string) string {
	var b strings.Builder
	for _, r := range value {
		if r < 0x80 && (r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z' || r >= '0' && r <= '9') {
			b.WriteRune(r)
			continue
		}
		for _, unit := range utf16.Encode([]rune{r}) {
			fmt.Fprintf(&b, "_%04X", unit)
		}
	}
	return b.String()
}

// GetDeprecationCode returns the stable code for a deprecated setting.
// Provider-scoped warnings (provider != "") get a provider-scoped code, but
// are not scoped to a particular model or message. Mirrors TS
// getDeprecationCode.
func GetDeprecationCode(setting, provider string) string {
	if provider == "" {
		if code, ok := deprecationCodes[setting]; ok {
			return code
		}
		return "AISDK_DEP_SETTING_" + encodeDeprecationCodePart(setting)
	}
	return "AISDK_DEP_PROVIDER_" + encodeDeprecationCodePart(provider) + "__" + encodeDeprecationCodePart(setting)
}
