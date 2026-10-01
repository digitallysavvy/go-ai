package anthropic

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func TestSkillsAPI_UploadSkill(t *testing.T) {
	var calledVersion bool
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/versions/") {
			calledVersion = true
			_ = json.NewEncoder(w).Encode(map[string]interface{}{
				"name":        "test-capture-skill",
				"description": "updated description",
			})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"id":             "skill_123",
			"display_title":  "Test Capture Skill",
			"name":           "stale-name",
			"description":    "stale-description",
			"latest_version": "1",
			"source":         "custom",
			"created_at":     "2026-02-26T03:59:39.314772Z",
			"updated_at":     "2026-02-26T03:59:39.314772Z",
		})
	}))
	defer srv.Close()

	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	res, err := p.Skills().UploadSkill(context.Background(), types.UploadSkillOptions{
		Files: []types.UploadSkillFile{{Path: "index.ts", Data: types.FileData{Type: types.FileDataTypeData, Data: []byte("x")}}},
	})
	if err != nil {
		t.Fatalf("UploadSkill() err = %v", err)
	}
	if !calledVersion {
		t.Fatal("expected version metadata request")
	}
	if res.ProviderReference["anthropic"] != "skill_123" || res.Name != "test-capture-skill" {
		t.Fatalf("result = %+v", res)
	}
}

// TestSkillsAPI_UploadSkill_VersionMetadataErrorPropagates is a regression
// test for a (nil, nil) error-path bug: fetchVersionMetadata's
// `if err != nil || resp.StatusCode >= 400 { return nil, err }` returned
// (nil, nil) for a non-transport-error 4xx/5xx response (err is nil in that
// branch). UploadSkill's `if err == nil { meta.Name ... }` then treated that
// as success and dereferenced the nil *struct, panicking. The fix returns a
// proper non-nil error for the >=400 branch, and UploadSkill now propagates
// it instead of swallowing it (matching TS AnthropicSkills.uploadSkill,
// where fetchVersionMetadata's failedResponseHandler throws and the error is
// not caught).
func TestSkillsAPI_UploadSkill_VersionMetadataErrorPropagates(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/versions/") {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":{"message":"boom"}}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"id":             "skill_123",
			"latest_version": "1",
		})
	}))
	defer srv.Close()

	p := New(Config{APIKey: "k", BaseURL: srv.URL})

	res, err := p.Skills().UploadSkill(context.Background(), types.UploadSkillOptions{
		Files: []types.UploadSkillFile{{Path: "index.ts", Data: types.FileData{Type: types.FileDataTypeData, Data: []byte("x")}}},
	})
	if err == nil {
		t.Fatalf("expected an error when version metadata fetch fails, got result %+v", res)
	}
	if res != nil {
		t.Fatalf("expected nil result on error, got %+v", res)
	}
}
