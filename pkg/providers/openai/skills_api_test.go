package openai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func TestSkillsAPI_UploadSkill(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/skills" {
			t.Fatalf("path = %s", r.URL.Path)
		}
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"id":              "skill_1",
			"name":            "skill-name",
			"description":     "desc",
			"latest_version":  "1",
			"default_version": "1",
			"created_at":      100,
		})
	}))
	defer srv.Close()

	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	res, err := p.Skills().UploadSkill(context.Background(), types.UploadSkillOptions{
		DisplayTitle: "My Skill",
		Files: []types.UploadSkillFile{{
			Path: "index.ts",
			Data: types.FileData{Type: types.FileDataTypeData, Data: []byte("x")},
		}},
	})
	if err != nil {
		t.Fatalf("UploadSkill() err = %v", err)
	}
	if res.ProviderReference["openai"] != "skill_1" {
		t.Fatalf("ProviderReference = %+v", res.ProviderReference)
	}
	if len(res.Warnings) == 0 || res.Warnings[0].Feature != "displayTitle" {
		t.Fatalf("warnings = %+v", res.Warnings)
	}
}
