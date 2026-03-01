package openai

// WebSearchCallItem represents a web_search_call output item in the
// OpenAI Responses API. The model emits this when it performs a web search.
//
// The Action field is optional: the API may omit it in some responses (#12706).
type WebSearchCallItem struct {
	// Type is always "web_search_call".
	Type string `json:"type"`

	// ID is the unique identifier for this output item.
	ID string `json:"id,omitempty"`

	// Status indicates the current search state.
	// Values: "in_progress", "searching", "completed"
	Status string `json:"status,omitempty"`

	// Action describes the search action performed.
	// It is a pointer because the API may omit this field (#12706).
	Action *WebSearchAction `json:"action,omitempty"`
}

// WebSearchAction describes the web search action performed by the model.
type WebSearchAction struct {
	// Type indicates the kind of search action.
	// Values: "search", "open_page", "find_in_page", "done"
	Type string `json:"type,omitempty"`

	// Query is the search query (for "search" type).
	Query *string `json:"query,omitempty"`

	// URL is the page URL (for "open_page" and "find_in_page" types).
	URL *string `json:"url,omitempty"`

	// Pattern is the search pattern (for "find_in_page" type).
	Pattern *string `json:"pattern,omitempty"`

	// Sources are the search result sources (for "search" type).
	Sources []WebSearchSource `json:"sources,omitempty"`
}

// WebSearchSource represents a single result source from a web search.
type WebSearchSource struct {
	// Type is "url" or "api".
	Type string `json:"type"`

	// URL is the result URL (for "url" type).
	URL string `json:"url,omitempty"`

	// Name is the API name (for "api" type).
	Name string `json:"name,omitempty"`
}
