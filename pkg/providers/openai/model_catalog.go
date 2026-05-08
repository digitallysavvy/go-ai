package openai

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/digitallysavvy/go-ai/pkg/modelcatalog"
)

const OpenAIModelsEndpoint = "https://api.openai.com/v1/models"

// ModelCatalogRefresher fetches OpenAI model IDs from the public models API.
type ModelCatalogRefresher struct {
	APIKey     string
	HTTPClient *http.Client
	Endpoint   string
}

// Fetch retrieves OpenAI model IDs for maintainer-triggered model_ids.go refreshes.
func (r ModelCatalogRefresher) Fetch(ctx context.Context) ([]modelcatalog.ModelID, error) {
	if r.APIKey == "" {
		return nil, fmt.Errorf("openai API key is required")
	}
	endpoint := r.Endpoint
	if endpoint == "" {
		endpoint = OpenAIModelsEndpoint
	}
	client := r.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Authorization", "Bearer "+r.APIKey)

	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("openai models request failed: %s", resp.Status)
	}

	var body struct {
		Data []struct {
			ID string `json:"id"`
		} `json:"data"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		return nil, err
	}
	models := make([]modelcatalog.ModelID, 0, len(body.Data))
	for _, item := range body.Data {
		if item.ID == "" {
			continue
		}
		models = append(models, modelcatalog.ModelID{ID: item.ID})
	}
	return models, nil
}
