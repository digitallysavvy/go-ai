package replicate

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// newTestVideoModelServer builds a Provider + httptest server for the video
// model's DoStart/DoStatus, mirroring replicate-video-model.test.ts's
// createMockModel fetch stub.
func newTestVideoModelServer(t *testing.T, mux *http.ServeMux) (*VideoModel, *httptest.Server) {
	t.Helper()
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	p := New(Config{APIKey: "test-api-token", BaseURL: srv.URL})
	return NewVideoModel(p, "minimax/video-01"), srv
}

func TestVideoModel_DoStart_UnversionedModelUsesModelsPath(t *testing.T) {
	var gotPath string
	var gotBody map[string]interface{}
	mux := http.NewServeMux()
	mux.HandleFunc("/models/minimax/video-01/predictions", func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"id": "pred-1", "status": "starting",
			"urls": map[string]interface{}{"get": "http://" + r.Host + "/predictions/pred-1"},
		})
	})
	model, _ := newTestVideoModelServer(t, mux)

	result, err := model.DoStart(context.Background(), &provider.VideoModelV3StartOptions{
		VideoModelV3CallOptions: provider.VideoModelV3CallOptions{Prompt: "A rocket launching into space"},
	})
	if err != nil {
		t.Fatalf("DoStart() error = %v", err)
	}
	if gotPath != "/models/minimax/video-01/predictions" {
		t.Errorf("path = %q", gotPath)
	}
	if _, ok := gotBody["version"]; ok {
		t.Errorf("version should not be present for unversioned model: %#v", gotBody)
	}
	if result.Response.ModelID != "minimax/video-01" {
		t.Errorf("ModelID = %q", result.Response.ModelID)
	}
}

func TestVideoModel_DoStart_VersionedModelUsesPredictionsPath(t *testing.T) {
	var gotPath string
	var gotBody map[string]interface{}
	mux := http.NewServeMux()
	mux.HandleFunc("/predictions", func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"id": "pred-1", "status": "starting",
			"urls": map[string]interface{}{"get": "http://" + r.Host + "/predictions/pred-1"},
		})
	})
	srv := httptest.NewServer(mux)
	t.Cleanup(srv.Close)
	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	model := NewVideoModel(p, "owner/model:abcdef1234567890")

	_, err := model.DoStart(context.Background(), &provider.VideoModelV3StartOptions{
		VideoModelV3CallOptions: provider.VideoModelV3CallOptions{Prompt: "x"},
	})
	if err != nil {
		t.Fatalf("DoStart() error = %v", err)
	}
	if gotPath != "/predictions" {
		t.Errorf("path = %q", gotPath)
	}
	if gotBody["version"] != "abcdef1234567890" {
		t.Errorf("version = %v", gotBody["version"])
	}
}

func TestVideoModel_DoStart_WebhookFields(t *testing.T) {
	var gotBody map[string]interface{}
	mux := http.NewServeMux()
	mux.HandleFunc("/models/minimax/video-01/predictions", func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"id": "pred-1", "status": "starting",
			"urls": map[string]interface{}{"get": "http://" + r.Host + "/predictions/pred-1"},
		})
	})
	model, _ := newTestVideoModelServer(t, mux)

	_, err := model.DoStart(context.Background(), &provider.VideoModelV3StartOptions{
		VideoModelV3CallOptions: provider.VideoModelV3CallOptions{Prompt: "x"},
		WebhookURL:              "https://example.com/webhook",
	})
	if err != nil {
		t.Fatalf("DoStart() error = %v", err)
	}
	if gotBody["webhook"] != "https://example.com/webhook" {
		t.Errorf("webhook = %v", gotBody["webhook"])
	}
	filter, ok := gotBody["webhook_events_filter"].([]interface{})
	if !ok || len(filter) != 1 || filter[0] != "completed" {
		t.Errorf("webhook_events_filter = %v", gotBody["webhook_events_filter"])
	}
}

func TestVideoModel_DoStatus_Succeeded(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/models/minimax/video-01/predictions", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"id": "pred-1", "status": "starting",
			"urls": map[string]interface{}{"get": "http://" + r.Host + "/predictions/pred-1"},
		})
	})
	mux.HandleFunc("/predictions/pred-1", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"id": "pred-1", "status": "succeeded",
			"output":  "https://replicate.delivery/video.mp4",
			"metrics": map[string]interface{}{"predict_time": 25.5},
		})
	})
	model, _ := newTestVideoModelServer(t, mux)

	startResult, err := model.DoStart(context.Background(), &provider.VideoModelV3StartOptions{
		VideoModelV3CallOptions: provider.VideoModelV3CallOptions{Prompt: "x"},
	})
	if err != nil {
		t.Fatalf("DoStart() error = %v", err)
	}
	status, err := model.DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: startResult.Operation})
	if err != nil {
		t.Fatalf("DoStatus() error = %v", err)
	}
	if status.Status != provider.VideoOperationStatusCompleted {
		t.Fatalf("Status = %q", status.Status)
	}
	if len(status.Videos) != 1 || status.Videos[0].URL != "https://replicate.delivery/video.mp4" {
		t.Fatalf("Videos = %#v", status.Videos)
	}
	meta, ok := status.ProviderMetadata["replicate"].(map[string]interface{})
	if !ok || meta["predictionId"] != "pred-1" {
		t.Fatalf("ProviderMetadata = %#v", status.ProviderMetadata)
	}
}

func TestVideoModel_DoStatus_FailedAndCanceled(t *testing.T) {
	statusValue := "failed"
	mux := http.NewServeMux()
	mux.HandleFunc("/models/minimax/video-01/predictions", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"id": "pred-1", "status": "starting",
			"urls": map[string]interface{}{"get": "http://" + r.Host + "/predictions/pred-1"},
		})
	})
	mux.HandleFunc("/predictions/pred-1", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		body := map[string]interface{}{"id": "pred-1", "status": statusValue}
		if statusValue == "failed" {
			body["error"] = "model exploded"
		}
		_ = json.NewEncoder(w).Encode(body)
	})
	model, _ := newTestVideoModelServer(t, mux)

	startResult, err := model.DoStart(context.Background(), &provider.VideoModelV3StartOptions{
		VideoModelV3CallOptions: provider.VideoModelV3CallOptions{Prompt: "x"},
	})
	if err != nil {
		t.Fatalf("DoStart() error = %v", err)
	}

	status, err := model.DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: startResult.Operation})
	if err != nil {
		t.Fatalf("DoStatus() error = %v", err)
	}
	if status.Status != provider.VideoOperationStatusError {
		t.Fatalf("Status = %q, want error", status.Status)
	}

	statusValue = "canceled"
	status, err = model.DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: startResult.Operation})
	if err != nil {
		t.Fatalf("DoStatus() error = %v", err)
	}
	if status.Status != provider.VideoOperationStatusError || status.Error != "Video generation was canceled" {
		t.Fatalf("Status/Error = %q/%q", status.Status, status.Error)
	}
}

func TestVideoModel_DoStatus_Pending(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/models/minimax/video-01/predictions", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"id": "pred-1", "status": "starting",
			"urls": map[string]interface{}{"get": "http://" + r.Host + "/predictions/pred-1"},
		})
	})
	mux.HandleFunc("/predictions/pred-1", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": "pred-1", "status": "processing"})
	})
	model, _ := newTestVideoModelServer(t, mux)

	startResult, err := model.DoStart(context.Background(), &provider.VideoModelV3StartOptions{
		VideoModelV3CallOptions: provider.VideoModelV3CallOptions{Prompt: "x"},
	})
	if err != nil {
		t.Fatalf("DoStart() error = %v", err)
	}
	status, err := model.DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{Operation: startResult.Operation})
	if err != nil {
		t.Fatalf("DoStatus() error = %v", err)
	}
	if status.Status != provider.VideoOperationStatusPending {
		t.Fatalf("Status = %q, want pending", status.Status)
	}
}

func TestVideoModel_DoGenerate_EndToEnd(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("/models/minimax/video-01/predictions", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"id": "pred-1", "status": "starting",
			"urls": map[string]interface{}{"get": "http://" + r.Host + "/predictions/pred-1"},
		})
	})
	mux.HandleFunc("/predictions/pred-1", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"id": "pred-1", "status": "succeeded", "output": "https://replicate.delivery/video.mp4",
		})
	})
	model, _ := newTestVideoModelServer(t, mux)

	resp, err := model.DoGenerate(context.Background(), &provider.VideoModelV3CallOptions{
		Prompt: "x",
		ProviderOptions: map[string]interface{}{
			"replicate": map[string]interface{}{"pollIntervalMs": 1, "pollTimeoutMs": 5000},
		},
	})
	if err != nil {
		t.Fatalf("DoGenerate() error = %v", err)
	}
	if len(resp.Videos) != 1 || resp.Videos[0].URL != "https://replicate.delivery/video.mp4" {
		t.Fatalf("Videos = %#v", resp.Videos)
	}
}
