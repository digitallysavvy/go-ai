package gateway

import (
	"net/http"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// TS convertGatewayBatchStatus `providerMetadata != null`: an empty object
// is reported as {} (only absent or null is left out).
func TestR92provgBatchEmptyProviderMetadataKept(t *testing.T) {
	for _, tc := range []struct {
		body string
		want bool
	}{{`{"status":"pending","providerMetadata":{}}`, true}, {`{"status":"pending","providerMetadata":null}`, false}, {`{"status":"pending"}`, false}} {
		p := r91provgBatchServer(t, func(w http.ResponseWriter, r *http.Request, body []byte) {
			_, _ = w.Write([]byte(tc.body))
		})
		status, err := p.ExperimentalBatch().DoGetBatchStatus(t.Context(), provider.BatchV4OperationOptions{BatchID: "b"})
		if err != nil {
			t.Fatal(err)
		}
		if got := status.ProviderMetadata != nil; got != tc.want {
			t.Fatalf("%s: providerMetadata = %#v", tc.body, status.ProviderMetadata)
		}
	}
}
