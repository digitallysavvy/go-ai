package fireworks

// AsyncSubmitResponse is the response from the async image generation submit endpoint.
type AsyncSubmitResponse struct {
	RequestID string `json:"request_id"`
}

// AsyncPollResponse is the response from the async poll endpoint.
type AsyncPollResponse struct {
	ID     string           `json:"id"`
	Status string           `json:"status"` // "Pending", "Running", "Ready", "Error", "Failed"
	Result *AsyncPollResult `json:"result"`
}

// AsyncPollResult contains the generated sample when status is "Ready".
type AsyncPollResult struct {
	Sample *string `json:"sample"` // URL when generation has succeeded
}
