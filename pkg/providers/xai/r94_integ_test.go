package xai

import (
	"context"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TestR94integBatchInputFileExpiresAtISOString: TS
// `new Date(expires_at * 1000).toISOString()` (xai-batch.ts, no range
// guard): expanded years outside 0..9999, RangeError "Invalid time value"
// outside the ECMAScript time range (cr-r94-carry-to-provb).
func TestR94integBatchInputFileExpiresAtISOString(t *testing.T) {
	for _, tc := range []struct {
		expiresAt float64
		want      string
		wantErr   string
	}{
		{253402300800, "+010000-01-01T00:00:00.000Z", ""},
		{-62198755200, "-000001-01-01T00:00:00.000Z", ""},
		{1e13, "", "Invalid time value"},
	} {
		server := newXAIBatchTestServer(t)
		server.filesBody = map[string]interface{}{"id": "file_123", "filename": "batch.jsonl", "expires_at": tc.expiresAt}
		server.createBody = xaiBatchResponseBody(nil)
		result, err := newTestXAIBatchProvider(server.ts.URL).DoStartBatch(context.Background(), provider.BatchV4StartOptions{
			Requests: []provider.BatchV4Request{textBatchRequest("a", "grok-4.3", "Hi")},
		})
		server.close()
		if tc.wantErr != "" {
			if err == nil || err.Error() != tc.wantErr {
				t.Fatalf("expires_at %v: err = %v, want %q", tc.expiresAt, err, tc.wantErr)
			}
			continue
		}
		if err != nil {
			t.Fatalf("expires_at %v: %v", tc.expiresAt, err)
		}
		meta, _ := result.ProviderMetadata["xai"].(map[string]interface{})
		if meta["inputFileExpiresAt"] != tc.want {
			t.Errorf("expires_at %v: inputFileExpiresAt = %v, want %s", tc.expiresAt, meta["inputFileExpiresAt"], tc.want)
		}
	}
}
