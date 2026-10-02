package azure

import (
	"context"
	stdhttp "net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/internal/fileutil"
	internalhttp "github.com/digitallysavvy/go-ai/pkg/internal/http"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

const azureDefaultVoice = "en-US-Harper"
const azureDefaultOutputFormat = "audio-24khz-160kbitrate-mono-mp3"

// azureDefaultVoicesByLanguage maps an ISO 639-1 language code to its default
// MAI voice, mirroring TS DEFAULT_VOICES.
var azureDefaultVoicesByLanguage = map[string]string{
	"de": "de-DE-Mia",
	"en": azureDefaultVoice,
	"es": "es-MX-Valeria",
	"fr": "fr-FR-Soleil",
	"hi": "hi-IN-Kavya",
	"hu": "hu-HU-Lilla",
	"it": "it-IT-Rosa",
	"ko": "ko-KR-Haena",
	"nl": "nl-NL-Fleur",
	"pt": "pt-BR-Luana",
	"ro": "ro-RO-Elena",
	"ru": "ru-RU-Masha",
	"th": "th-TH-Krit",
	"tr": "tr-TR-Elif",
	"zh": "zh-CN-Mei",
}

// azureOutputFormatShorthands maps an `outputFormat` shorthand to its Azure
// X-Microsoft-OutputFormat value, mirroring TS OUTPUT_FORMATS.
var azureOutputFormatShorthands = map[string]string{
	"mp3":  azureDefaultOutputFormat,
	"opus": "ogg-24khz-16bit-mono-opus",
	"pcm":  "raw-24khz-16bit-mono-pcm",
	"wav":  "riff-24khz-16bit-mono-pcm",
}

// azureNativeOutputFormatPattern matches any other native
// X-Microsoft-OutputFormat value, mirroring TS NATIVE_OUTPUT_FORMAT.
var azureNativeOutputFormatPattern = regexp.MustCompile(`^(?:amr|audio|g722|ogg|raw|riff|webm)-[a-z0-9-]+$`)

// azureVoiceLocalePattern extracts a voice name's locale prefix, mirroring
// TS's /^([a-z]{2,3}-[a-z]{2,4})-/i (used to derive the SSML xml:lang).
var azureVoiceLocalePattern = regexp.MustCompile(`(?i)^([a-z]{2,3}-[a-z]{2,4})-`)

// azureVoiceLanguagePattern extracts a voice name's ISO 639-1 language code,
// mirroring TS's /^([a-z]{2,3})-[a-z]{2,4}-/i.
var azureVoiceLanguagePattern = regexp.MustCompile(`(?i)^([a-z]{2,3})-[a-z]{2,4}-`)

// AzureSpeechSpeechModel is the Azure Speech SSML text-to-speech backend for
// MAI-Voice-2, MAI-Voice-2-Flash, MAI-Voice-2.1, and MAI-Voice-2.1-Flash,
// mirroring TS AzureSpeechSpeechModel (azure-speech-speech-model.ts): POST
// /tts/cognitiveservices/v1 with an SSML body. The model name is appended to
// the voice name, e.g. "en-US-Harper:MAI-Voice-2".
type AzureSpeechSpeechModel struct {
	modelID    string
	url        func() (string, error)
	headers    func(ctx context.Context) (map[string]string, error)
	httpClient *stdhttp.Client
}

func newAzureSpeechSpeechModel(modelID string, url func() (string, error), headers func(ctx context.Context) (map[string]string, error), httpClient *stdhttp.Client) *AzureSpeechSpeechModel {
	return &AzureSpeechSpeechModel{modelID: modelID, url: url, headers: headers, httpClient: httpClient}
}

func (m *AzureSpeechSpeechModel) SpecificationVersion() string { return "v4" }
func (m *AzureSpeechSpeechModel) Provider() string             { return "azure.speech" }
func (m *AzureSpeechSpeechModel) ModelID() string              { return m.modelID }

// DoGenerate synthesizes speech via the Azure Speech SSML API, mirroring TS
// AzureSpeechSpeechModel#doGenerate.
func (m *AzureSpeechSpeechModel) DoGenerate(ctx context.Context, opts *provider.SpeechGenerateOptions, azureOpts AzureSpeechModelSpeechOptions) (*types.SpeechResult, error) {
	if opts == nil {
		opts = &provider.SpeechGenerateOptions{}
	}
	currentDate := time.Now()
	var warnings []types.Warning

	if opts.Instructions != "" {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported",
			Feature: "instructions",
			Details: "Use providerOptions.azure.style to control speaking style.",
		})
	}

	voice, languageWarning := azureResolveVoice(opts.Voice, opts.Language)
	if languageWarning != "" {
		warnings = append(warnings, types.Warning{Type: "unsupported", Feature: "language", Details: languageWarning})
	}
	if azureOpts.StyleDegree != nil && azureOpts.Style == "" {
		warnings = append(warnings, types.Warning{
			Type:    "unsupported",
			Feature: "providerOptions.azure.styleDegree",
			Details: "styleDegree requires style.",
		})
	}

	outputFormat := azureDefaultOutputFormat
	if opts.OutputFormat != "" {
		format := strings.ToLower(opts.OutputFormat)
		if shorthand, ok := azureOutputFormatShorthands[format]; ok {
			outputFormat = shorthand
		} else if azureNativeOutputFormatPattern.MatchString(format) {
			outputFormat = format
		} else {
			warnings = append(warnings, types.Warning{
				Type:    "unsupported",
				Feature: "outputFormat",
				Details: "Unsupported output format: " + opts.OutputFormat + ". Using mp3 instead.",
			})
		}
	}

	voiceName := voice
	if !strings.Contains(voice, ":") {
		modelName := m.modelID
		if name, ok := getMAIVoiceModel(m.modelID); ok {
			modelName = name
		}
		voiceName = voice + ":" + modelName
	}
	locale := "en-US"
	if match := azureVoiceLocalePattern.FindStringSubmatch(voice); match != nil {
		locale = match[1]
	}

	styleDegree := azureOpts.StyleDegree
	if azureOpts.Style == "" {
		styleDegree = nil
	}
	ssml := buildAzureSSML(azureSSMLOptions{
		text:        opts.Text,
		voiceName:   voiceName,
		locale:      locale,
		speed:       opts.Speed,
		style:       azureOpts.Style,
		styleDegree: styleDegree,
	})

	urlStr, err := m.url()
	if err != nil {
		return nil, err
	}
	headers, err := m.headers(ctx)
	if err != nil {
		return nil, err
	}

	req, err := stdhttp.NewRequestWithContext(ctx, stdhttp.MethodPost, urlStr, strings.NewReader(ssml))
	if err != nil {
		return nil, err
	}
	for k, v := range internalhttp.MergeHeaders(headers, map[string]string{
		"Content-Type":             "application/ssml+xml",
		"X-Microsoft-OutputFormat": outputFormat,
	}, opts.Headers) {
		req.Header.Set(k, v)
	}

	resp, err := m.httpClient.Do(req)
	if err != nil {
		return nil, providererrors.NewProviderError(m.Provider(), 0, "", err.Error(), err)
	}
	defer resp.Body.Close() //nolint:errcheck

	respBody, err := fileutil.ReadResponseWithSizeLimit(resp, urlStr, fileutil.DefaultMaxDownloadSize)
	if err != nil {
		return nil, err
	}

	if resp.StatusCode != stdhttp.StatusOK {
		return nil, azureSpeechAPIError(m.Provider(), resp.StatusCode, resp.Header, respBody)
	}

	if warnings == nil {
		warnings = []types.Warning{}
	}
	return &types.SpeechResult{
		Audio:    respBody,
		Warnings: warnings,
		Request:  &types.StepRequest{Body: ssml},
		Response: &types.ResponseMetadata{
			Timestamp: currentDate,
			ModelID:   m.modelID,
			Headers:   providerutils.ExtractHeaders(resp.Header),
			Body:      respBody,
		},
	}, nil
}

// azureResolveVoice picks the voice to use, mirroring TS resolveVoice: an
// explicit voice always wins; otherwise the language's default voice is
// used, falling back to azureDefaultVoice with a warning for "auto" or an
// unmapped language.
func azureResolveVoice(voice, language string) (resolved string, languageWarning string) {
	code := ""
	if language != "" {
		code = strings.ToLower(strings.SplitN(language, "-", 2)[0])
	}
	if voice == "" {
		if code == "" {
			return azureDefaultVoice, ""
		}
		if code == "auto" {
			return azureDefaultVoice, "Automatic language detection is not supported. " + azureDefaultVoice + " was used."
		}
		if defaultVoice, ok := azureDefaultVoicesByLanguage[code]; ok {
			return defaultVoice, ""
		}
		return azureDefaultVoice, "No default MAI voice for language \"" + language + "\". " + azureDefaultVoice + " was used."
	}
	voiceLanguage := ""
	if match := azureVoiceLanguagePattern.FindStringSubmatch(voice); match != nil {
		voiceLanguage = strings.ToLower(match[1])
	}
	if code != "" && code != "auto" && voiceLanguage != "" && code != voiceLanguage {
		return voice, "The voice " + voice + " selects the language. Language \"" + language + "\" was ignored."
	}
	return voice, ""
}

type azureSSMLOptions struct {
	text        string
	voiceName   string
	locale      string
	speed       *float64
	style       string
	styleDegree *float64
}

// buildAzureSSML builds the SSML payload sent to Azure Speech, mirroring TS
// buildSsml. Text and attributes are XML-escaped so input cannot inject SSML.
func buildAzureSSML(opts azureSSMLOptions) string {
	content := azureEscapeXML(opts.text)
	if opts.speed != nil {
		content = `<prosody rate="` + strconv.FormatFloat(*opts.speed, 'g', -1, 64) + `">` + content + `</prosody>`
	}
	if opts.style != "" {
		degree := ""
		if opts.styleDegree != nil {
			degree = ` styledegree="` + strconv.FormatFloat(*opts.styleDegree, 'g', -1, 64) + `"`
		}
		content = `<mstts:express-as style="` + azureEscapeXML(opts.style) + `"` + degree + `>` + content + `</mstts:express-as>`
	}
	return `<speak version="1.0" xmlns="http://www.w3.org/2001/10/synthesis" ` +
		`xmlns:mstts="http://www.w3.org/2001/mstts" xml:lang="` + azureEscapeXML(opts.locale) + `">` +
		`<voice name="` + azureEscapeXML(opts.voiceName) + `">` + content + `</voice></speak>`
}

func azureEscapeXML(value string) string {
	var b strings.Builder
	for _, r := range value {
		switch r {
		case '&':
			b.WriteString("&amp;")
		case '<':
			b.WriteString("&lt;")
		case '>':
			b.WriteString("&gt;")
		case '"':
			b.WriteString("&quot;")
		case '\'':
			b.WriteString("&apos;")
		default:
			b.WriteRune(r)
		}
	}
	return b.String()
}
