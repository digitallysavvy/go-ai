package anthropic

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// cr-r90-prova-to-integrator: TS uploadSkill appends new Blob([content]) named
// file.path to a FormData. undici writes no filename parameter for a File
// named "" and escapes `"` in a filename as %22 (Node 24 recording).
func TestR90integSkillUploadPartHeaders(t *testing.T) {
	var mu sync.Mutex
	var body string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "/versions/") {
			_ = json.NewEncoder(w).Encode(map[string]interface{}{"type": "skill_version", "skill_id": "s", "name": "n", "description": "d"})
			return
		}
		raw, _ := io.ReadAll(r.Body)
		mu.Lock()
		body = string(raw)
		mu.Unlock()
		_ = json.NewEncoder(w).Encode(map[string]interface{}{"id": "s", "latest_version": "1", "source": "custom", "created_at": "2026-02-26T03:59:39Z", "updated_at": "2026-02-26T03:59:39Z"})
	}))
	defer srv.Close()
	p := New(Config{APIKey: "k", BaseURL: srv.URL})
	_, err := p.Skills().UploadSkill(context.Background(), types.UploadSkillOptions{
		Files: []types.UploadSkillFile{
			{Path: "", Data: types.FileData{Type: types.FileDataTypeData, Data: []byte("x")}},
			{Path: `dir/a"b.md`, Data: types.FileData{Type: types.FileDataTypeData, Data: []byte("y")}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	mu.Lock()
	defer mu.Unlock()
	for _, want := range []string{
		"Content-Disposition: form-data; name=\"files[]\"\r\nContent-Type: application/octet-stream\r\n\r\nx\r\n",
		"Content-Disposition: form-data; name=\"files[]\"; filename=\"dir/a%22b.md\"\r\nContent-Type: application/octet-stream\r\n\r\ny\r\n",
	} {
		if !strings.Contains(body, want) {
			t.Errorf("body = %q, want it to contain %q", body, want)
		}
	}
}
