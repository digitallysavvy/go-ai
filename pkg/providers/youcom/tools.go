// Package youcom provides You.com AI SDK plugin tool factories.
package youcom

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

const (
	ProviderKey  = "you"
	ProviderName = "you.com"

	ToolIDSearch   = "you.search"
	ToolIDResearch = "you.research"
	ToolIDContents = "you.contents"

	defaultSearchURL   = "https://api.you.com/v1/agents/search"
	defaultResearchURL = "https://api.you.com/v1/research"
	defaultContentsURL = "https://ydc-index.io/v1/contents"
)

// YouToolsConfig configures You.com AI SDK tools.
//
// APIKey matches the TypeScript plugin's apiKey option and falls back to
// YDC_API_KEY when empty.
type YouToolsConfig struct {
	APIKey string

	// SearchURL, ResearchURL, ContentsURL, and HTTPClient are Go-specific
	// testability hooks. Leave empty to use the same defaults/env overrides as
	// the TypeScript package.
	SearchURL   string
	ResearchURL string
	ContentsURL string
	BaseURL     string // Deprecated: use the endpoint-specific URL fields.
	HTTPClient  *http.Client
}

// YouSearchConfig is the search tool input schema.
type YouSearchConfig struct {
	Query            string   `json:"query"`
	Count            *int     `json:"count,omitempty"`
	Freshness        *string  `json:"freshness,omitempty"`
	Offset           *int     `json:"offset,omitempty"`
	Country          *string  `json:"country,omitempty"`
	SafeSearch       *string  `json:"safesearch,omitempty"`
	LiveCrawl        *string  `json:"livecrawl,omitempty"`
	LiveCrawlFormats []string `json:"livecrawl_formats,omitempty"`
	Language         *string  `json:"language,omitempty"`
	IncludeDomains   []string `json:"include_domains,omitempty"`
	ExcludeDomains   []string `json:"exclude_domains,omitempty"`
	CrawlTimeout     *int     `json:"crawl_timeout,omitempty"`
}

// YouResearchConfig is the research tool input schema.
type YouResearchConfig struct {
	Input          string  `json:"input"`
	ResearchEffort *string `json:"research_effort,omitempty"`
}

// YouContentsConfig is the content extraction tool input schema.
type YouContentsConfig struct {
	URLs         []string `json:"urls"`
	Formats      []string `json:"formats,omitempty"`
	Format       string   `json:"format,omitempty"`
	CrawlTimeout *int     `json:"crawl_timeout,omitempty"`
}

// YouSearch creates a locally executed You.com web search tool, matching the
// TypeScript @youdotcom-oss/ai-sdk-plugin youSearch(config?) factory.
func YouSearch(config ...YouToolsConfig) types.Tool {
	cfg := firstConfig(config)
	return localTool(ToolIDSearch, "You.com Search", "Search the web for current information, news, articles, and content using You.com. Returns web results with snippets and news articles. Use this when you need up-to-date information or facts from the internet.", searchSchema(), cfg, endpointSearch)
}

// YouResearch creates a locally executed You.com research tool, matching the
// TypeScript @youdotcom-oss/ai-sdk-plugin youResearch(config?) factory.
func YouResearch(config ...YouToolsConfig) types.Tool {
	cfg := firstConfig(config)
	return localTool(ToolIDResearch, "You.com Research", "Research a topic with comprehensive answers and cited sources using You.com. Supports configurable effort levels (lite, standard, deep, exhaustive). Returns a detailed answer with inline citations and a list of sources. Use this when you need thorough, well-researched answers to complex questions.", researchSchema(), cfg, endpointResearch)
}

// YouContents creates a locally executed You.com content extraction tool,
// matching the TypeScript @youdotcom-oss/ai-sdk-plugin youContents(config?) factory.
func YouContents(config ...YouToolsConfig) types.Tool {
	cfg := firstConfig(config)
	return localTool(ToolIDContents, "You.com Contents", "Extract full page content from web URLs using You.com. Returns page content in markdown or HTML format. Use this when you need to read and process entire web pages.", contentsSchema(), cfg, endpointContents)
}

type endpointKind string

const (
	endpointSearch   endpointKind = "search"
	endpointResearch endpointKind = "research"
	endpointContents endpointKind = "contents"
)

func localTool(id, title, description string, parameters map[string]interface{}, config YouToolsConfig, endpoint endpointKind) types.Tool {
	return types.Tool{
		Name:             id,
		Title:            title,
		Description:      description,
		Parameters:       parameters,
		ProviderName:     ProviderName,
		ProviderMetadata: ProviderMetadata(id),
		Execute: func(ctx context.Context, input map[string]interface{}, options types.ToolExecutionOptions) (interface{}, error) {
			return executeYouAPI(ctx, config, endpoint, input)
		},
	}
}

func firstConfig(config []YouToolsConfig) YouToolsConfig {
	if len(config) == 0 {
		return YouToolsConfig{}
	}
	return config[0]
}

func executeYouAPI(ctx context.Context, config YouToolsConfig, endpoint endpointKind, input map[string]interface{}) (interface{}, error) {
	apiKey := config.APIKey
	if apiKey == "" {
		apiKey = os.Getenv("YDC_API_KEY")
	}
	if apiKey == "" {
		return nil, errors.New("YDC_API_KEY is required. Set it in environment variables or pass it in config.")
	}

	requestURL := resolveEndpointURL(config, endpoint)
	client := config.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}

	if err := validateYouInput(endpoint, input); err != nil {
		return nil, err
	}
	if endpoint == endpointContents {
		input = normalizeContentsInput(input)
	}

	body, err := json.Marshal(input)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, requestURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("X-API-Key", apiKey)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", "Go-AI-SDK You.com")

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, youAPIHTTPError(endpoint, resp)
	}

	var result interface{}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return nil, err
	}
	if err := checkResponseForErrors(result); err != nil {
		return nil, err
	}
	return result, nil
}

func resolveEndpointURL(config YouToolsConfig, endpoint endpointKind) string {
	if config.BaseURL != "" {
		switch endpoint {
		case endpointSearch:
			return config.BaseURL + "/search"
		case endpointResearch:
			return config.BaseURL + "/research"
		case endpointContents:
			return config.BaseURL + "/contents"
		}
	}
	switch endpoint {
	case endpointSearch:
		if config.SearchURL != "" {
			return config.SearchURL
		}
		if v := os.Getenv("YDC_SEARCH_API_URL"); v != "" {
			return v
		}
		return defaultSearchURL
	case endpointResearch:
		if config.ResearchURL != "" {
			return config.ResearchURL
		}
		if v := os.Getenv("YDC_RESEARCH_API_URL"); v != "" {
			return v
		}
		return defaultResearchURL
	case endpointContents:
		if config.ContentsURL != "" {
			return config.ContentsURL
		}
		if v := os.Getenv("YDC_CONTENTS_API_URL"); v != "" {
			return v
		}
		return defaultContentsURL
	default:
		return ""
	}
}

func normalizeContentsInput(input map[string]interface{}) map[string]interface{} {
	out := make(map[string]interface{}, len(input)+1)
	for k, v := range input {
		if k != "format" {
			out[k] = v
		}
	}
	if _, ok := out["formats"]; !ok {
		if format, ok := input["format"].(string); ok && format != "" {
			out["formats"] = []string{format}
		} else {
			out["formats"] = []string{"markdown"}
		}
	}
	return out
}

func validateYouInput(endpoint endpointKind, input map[string]interface{}) error {
	switch endpoint {
	case endpointSearch:
		return validateSearchInput(input)
	case endpointResearch:
		return validateResearchInput(input)
	case endpointContents:
		return validateContentsInput(input)
	default:
		return nil
	}
}

func validateSearchInput(input map[string]interface{}) error {
	if _, err := requiredString(input, "query", false); err != nil {
		return err
	}
	if err := optionalIntegerRange(input, "count", 1, 100); err != nil {
		return err
	}
	if err := optionalIntegerRange(input, "offset", 0, 9); err != nil {
		return err
	}
	if err := optionalIntegerRange(input, "crawl_timeout", 1, 60); err != nil {
		return err
	}
	if err := optionalEnum(input, "country", countryValues); err != nil {
		return err
	}
	if err := optionalEnum(input, "safesearch", []string{"off", "moderate", "strict"}); err != nil {
		return err
	}
	if err := optionalEnum(input, "livecrawl", []string{"web", "news", "all"}); err != nil {
		return err
	}
	if err := optionalEnum(input, "language", languageValues); err != nil {
		return err
	}
	if err := optionalStringArray(input, "include_domains", 500, nil); err != nil {
		return err
	}
	if err := optionalStringArray(input, "exclude_domains", 500, nil); err != nil {
		return err
	}
	if err := optionalStringArray(input, "livecrawl_formats", 0, []string{"html", "markdown"}); err != nil {
		return err
	}
	if _, hasInclude := input["include_domains"]; hasInclude {
		if _, hasExclude := input["exclude_domains"]; hasExclude {
			return errors.New("Cannot combine include_domains and exclude_domains")
		}
	}
	return nil
}

func validateResearchInput(input map[string]interface{}) error {
	if _, err := requiredString(input, "input", true); err != nil {
		return err
	}
	return optionalEnum(input, "research_effort", []string{"lite", "standard", "deep", "exhaustive"})
}

func validateContentsInput(input map[string]interface{}) error {
	urls, err := requiredStringArray(input, "urls", 1, nil)
	if err != nil {
		return err
	}
	for _, rawURL := range urls {
		parsed, err := url.Parse(rawURL)
		if err != nil || parsed.Scheme == "" || parsed.Host == "" {
			return fmt.Errorf("urls contains invalid URL %q", rawURL)
		}
	}
	if err := optionalStringArray(input, "formats", 0, []string{"markdown", "html", "metadata"}); err != nil {
		return err
	}
	if err := optionalEnum(input, "format", []string{"markdown", "html"}); err != nil {
		return err
	}
	return optionalNumberRange(input, "crawl_timeout", 1, 60)
}

func requiredString(input map[string]interface{}, field string, minOne bool) (string, error) {
	value, ok := input[field]
	if !ok {
		return "", fmt.Errorf("%s is required", field)
	}
	s, ok := value.(string)
	if !ok {
		return "", fmt.Errorf("%s must be a string", field)
	}
	if minOne && s == "" {
		return "", fmt.Errorf("%s must contain at least 1 character", field)
	}
	return s, nil
}

func optionalNumberRange(input map[string]interface{}, field string, min, max float64) error {
	value, ok := input[field]
	if !ok || value == nil {
		return nil
	}
	number, ok := numberValue(value)
	if !ok {
		return fmt.Errorf("%s must be a number", field)
	}
	if number < min || number > max {
		return fmt.Errorf("%s must be between %v and %v", field, min, max)
	}
	return nil
}

func optionalIntegerRange(input map[string]interface{}, field string, min, max float64) error {
	value, ok := input[field]
	if !ok || value == nil {
		return nil
	}
	number, ok := numberValue(value)
	if !ok {
		return fmt.Errorf("%s must be a number", field)
	}
	if number != float64(int64(number)) {
		return fmt.Errorf("%s must be an integer", field)
	}
	if number < min || number > max {
		return fmt.Errorf("%s must be between %v and %v", field, min, max)
	}
	return nil
}

func numberValue(value interface{}) (float64, bool) {
	switch v := value.(type) {
	case int:
		return float64(v), true
	case int8:
		return float64(v), true
	case int16:
		return float64(v), true
	case int32:
		return float64(v), true
	case int64:
		return float64(v), true
	case uint:
		return float64(v), true
	case uint8:
		return float64(v), true
	case uint16:
		return float64(v), true
	case uint32:
		return float64(v), true
	case uint64:
		return float64(v), true
	case float32:
		return float64(v), true
	case float64:
		return v, true
	default:
		return 0, false
	}
}

func optionalEnum(input map[string]interface{}, field string, allowed []string) error {
	value, ok := input[field]
	if !ok || value == nil {
		return nil
	}
	s, ok := value.(string)
	if !ok {
		return fmt.Errorf("%s must be a string", field)
	}
	if !containsString(allowed, s) {
		return fmt.Errorf("%s must be one of %v", field, allowed)
	}
	return nil
}

func requiredStringArray(input map[string]interface{}, field string, minItems int, allowed []string) ([]string, error) {
	value, ok := input[field]
	if !ok || value == nil {
		return nil, fmt.Errorf("%s is required", field)
	}
	items, err := stringArray(value, field)
	if err != nil {
		return nil, err
	}
	if len(items) < minItems {
		return nil, fmt.Errorf("%s must contain at least %d item(s)", field, minItems)
	}
	if allowed != nil {
		for _, item := range items {
			if !containsString(allowed, item) {
				return nil, fmt.Errorf("%s must contain only %v", field, allowed)
			}
		}
	}
	return items, nil
}

func optionalStringArray(input map[string]interface{}, field string, maxItems int, allowed []string) error {
	value, ok := input[field]
	if !ok || value == nil {
		return nil
	}
	items, err := stringArray(value, field)
	if err != nil {
		return err
	}
	if maxItems > 0 && len(items) > maxItems {
		return fmt.Errorf("%s must contain at most %d item(s)", field, maxItems)
	}
	if allowed != nil {
		for _, item := range items {
			if !containsString(allowed, item) {
				return fmt.Errorf("%s must contain only %v", field, allowed)
			}
		}
	}
	return nil
}

func stringArray(value interface{}, field string) ([]string, error) {
	switch v := value.(type) {
	case []string:
		return v, nil
	case []interface{}:
		items := make([]string, 0, len(v))
		for _, item := range v {
			s, ok := item.(string)
			if !ok {
				return nil, fmt.Errorf("%s must contain only strings", field)
			}
			items = append(items, s)
		}
		return items, nil
	default:
		return nil, fmt.Errorf("%s must be an array of strings", field)
	}
}

func containsString(values []string, value string) bool {
	for _, candidate := range values {
		if candidate == value {
			return true
		}
	}
	return false
}

var countryValues = []string{
	"AR", "AU", "AT", "BE", "BR", "CA", "CL", "DK", "FI", "FR", "DE", "HK", "IN", "ID", "IT", "JP", "KR", "MY", "MX", "NL", "NZ", "NO", "CN", "PL", "PT", "PH", "RU", "SA", "ZA", "ES", "SE", "CH", "TW", "TR", "GB", "US",
}

var languageValues = []string{
	"AR", "EU", "BN", "BG", "CA", "ZH-HANS", "ZH-HANT", "HR", "CS", "DA", "NL", "EN", "EN-GB", "ET", "FI", "FR", "GL", "DE", "EL", "GU", "HE", "HI", "HU", "IS", "IT", "JP", "KN", "KO", "LV", "LT", "MS", "ML", "MR", "NB", "PL", "PT-BR", "PT-PT", "PA", "RO", "RU", "SR", "SK", "SL", "ES", "SV", "TA", "TE", "TH", "TR", "UK", "VI",
}

func youAPIHTTPError(endpoint endpointKind, resp *http.Response) error {
	code := resp.StatusCode
	if code == http.StatusTooManyRequests {
		return errors.New("Rate limited by You.com API. Please try again later.")
	}
	if endpoint == endpointContents {
		detail := fmt.Sprintf("Failed to fetch contents. HTTP %d", code)
		var body map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&body); err == nil {
			if v, ok := body["detail"]; ok {
				detail = fmt.Sprint(v)
			}
		}
		switch code {
		case http.StatusUnauthorized:
			return fmt.Errorf("Authentication failed: %s. Please check your You.com API key.", detail)
		case http.StatusForbidden:
			return fmt.Errorf("Forbidden: %s. Your API key may not have access to the Contents API.", detail)
		}
		if code >= 500 {
			return fmt.Errorf("You.com API server error: %s", detail)
		}
		return errors.New(detail)
	}
	if code == http.StatusForbidden {
		return errors.New("Forbidden. Please check your You.com API key.")
	}
	if code == http.StatusPaymentRequired {
		if endpoint == endpointResearch {
			return errors.New("Free tier limit exceeded. Please upgrade at: https://you.com/platform")
		}
		message := "Free tier limit exceeded. Please upgrade to continue."
		upgradeURL := "https://you.com/platform"
		var body map[string]interface{}
		if err := json.NewDecoder(resp.Body).Decode(&body); err == nil {
			if v, ok := body["message"].(string); ok && v != "" {
				message = v
			}
			if v, ok := body["upgrade_url"].(string); ok && v != "" {
				upgradeURL = v
			}
			if v, ok := body["reset_at"].(string); ok && v != "" {
				message += " Limit resets on " + v + "."
			}
		}
		return fmt.Errorf("%s Upgrade at: %s", message, upgradeURL)
	}
	if endpoint == endpointResearch {
		return fmt.Errorf("Research API request failed. Error code: %d", code)
	}
	return fmt.Errorf("Failed to perform search. Error code: %d", code)
}

func checkResponseForErrors(responseData interface{}) error {
	m, ok := responseData.(map[string]interface{})
	if !ok {
		return nil
	}
	if errValue, ok := m["error"]; ok {
		if s, ok := errValue.(string); ok {
			return fmt.Errorf("You.com API Error: %s", s)
		}
		data, err := json.Marshal(errValue)
		if err != nil {
			return fmt.Errorf("You.com API Error: %v", errValue)
		}
		return fmt.Errorf("You.com API Error: %s", data)
	}
	return nil
}

// ProviderMetadata returns metadata keyed by the provider key, matching the AI SDK provider metadata shape.
func ProviderMetadata(toolID string) map[string]interface{} {
	return map[string]interface{}{
		ProviderKey: map[string]interface{}{
			"providerName": ProviderName,
			"toolId":       toolID,
		},
	}
}

func searchSchema() map[string]interface{} {
	return objectSchema(map[string]interface{}{
		"query":             stringProp("Search query. Supports operators: site:domain.com (domain filter), filetype:pdf (file type), +term (include), -term (exclude), AND/OR/NOT (boolean logic), lang:en (language)."),
		"count":             map[string]interface{}{"type": "integer", "minimum": 1, "maximum": 100, "description": "Max results per section"},
		"freshness":         stringProp("day/week/month/year or YYYY-MM-DDtoYYYY-MM-DD"),
		"offset":            map[string]interface{}{"type": "integer", "minimum": 0, "maximum": 9, "description": "Pagination offset"},
		"country":           enumProp("Country code", "AR", "AU", "AT", "BE", "BR", "CA", "CL", "DK", "FI", "FR", "DE", "HK", "IN", "ID", "IT", "JP", "KR", "MY", "MX", "NL", "NZ", "NO", "CN", "PL", "PT", "PH", "RU", "SA", "ZA", "ES", "SE", "CH", "TW", "TR", "GB", "US"),
		"safesearch":        enumProp("Filter level", "off", "moderate", "strict"),
		"livecrawl":         enumProp("Live-crawl sections for full content", "web", "news", "all"),
		"livecrawl_formats": arrayEnumProp("Formats for crawled content", 0, "html", "markdown"),
		"language":          enumProp("Language code (BCP 47 format)", "AR", "EU", "BN", "BG", "CA", "ZH-HANS", "ZH-HANT", "HR", "CS", "DA", "NL", "EN", "EN-GB", "ET", "FI", "FR", "GL", "DE", "EL", "GU", "HE", "HI", "HU", "IS", "IT", "JP", "KN", "KO", "LV", "LT", "MS", "ML", "MR", "NB", "PL", "PT-BR", "PT-PT", "PA", "RO", "RU", "SR", "SK", "SL", "ES", "SV", "TA", "TE", "TH", "TR", "UK", "VI"),
		"include_domains":   arrayStringProp("Domains to include in results (up to 500)", 500),
		"exclude_domains":   arrayStringProp("Domains to exclude from results (up to 500)", 500),
		"crawl_timeout":     map[string]interface{}{"type": "integer", "minimum": 1, "maximum": 60, "description": "Crawl timeout in seconds (1-60)"},
	}, []string{"query"})
}

func researchSchema() map[string]interface{} {
	return objectSchema(map[string]interface{}{
		"input":           stringProp("The research question or complex query requiring in-depth investigation and multi-step reasoning. Maximum length: 40,000 characters."),
		"research_effort": enumProp("Controls how much time and effort the Research API spends on your question. lite: fast answers, standard: balanced (default), deep: thorough, exhaustive: most comprehensive.", "lite", "standard", "deep", "exhaustive"),
	}, []string{"input"})
}

func contentsSchema() map[string]interface{} {
	return objectSchema(map[string]interface{}{
		"urls": map[string]interface{}{
			"type":        "array",
			"description": "URLs to extract full page content from.",
			"items":       map[string]interface{}{"type": "string"},
			"minItems":    1,
		},
		"formats": arrayEnumProp(`Output formats: array of "markdown" (text), "html" (layout), or "metadata" (structured data)`, 0, "markdown", "html", "metadata"),
		"format": map[string]interface{}{
			"type":        "string",
			"description": "(Deprecated) Output format - use formats array instead",
			"enum":        []string{"markdown", "html"},
		},
		"crawl_timeout": map[string]interface{}{"type": "number", "minimum": 1, "maximum": 60, "description": "Optional timeout in seconds (1-60) for page crawling"},
	}, []string{"urls"})
}

func objectSchema(properties map[string]interface{}, required []string) map[string]interface{} {
	return map[string]interface{}{
		"type":                 "object",
		"properties":           properties,
		"required":             required,
		"additionalProperties": false,
	}
}

func stringProp(description string) map[string]interface{} {
	return map[string]interface{}{"type": "string", "description": description}
}

func enumProp(description string, values ...string) map[string]interface{} {
	return map[string]interface{}{"type": "string", "enum": values, "description": description}
}

func arrayStringProp(description string, maxItems int) map[string]interface{} {
	prop := map[string]interface{}{"type": "array", "items": map[string]interface{}{"type": "string"}, "description": description}
	if maxItems > 0 {
		prop["maxItems"] = maxItems
	}
	return prop
}

func arrayEnumProp(description string, maxItems int, values ...string) map[string]interface{} {
	prop := arrayStringProp(description, maxItems)
	prop["items"] = map[string]interface{}{"type": "string", "enum": values}
	return prop
}
