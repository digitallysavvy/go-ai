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
