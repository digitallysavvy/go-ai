package alibaba

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TestR99provbStatusOperationTaskIDKeyExact: TS reads options.operation.taskId
// with its exact key, so a wrong-case "TaskID" is not the task id.
func TestR99provbStatusOperationTaskIDKeyExact(t *testing.T) {
	var gotPath string
	model, server := newAlibabaVideoTestModel(t, func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]interface{}{
			"output": map[string]interface{}{"task_id": "x", "task_status": "PENDING"},
		})
	})
	defer server.Close()

	_, _ = model.DoStatus(context.Background(), &provider.VideoModelV3StatusOptions{
		Operation: json.RawMessage(`{"TaskID":"task_wrong_case"}`),
	})
	if gotPath == "/api/v1/tasks/task_wrong_case" {
		t.Fatalf("wrong-case TaskID key was read as taskId (path %s)", gotPath)
	}
}
