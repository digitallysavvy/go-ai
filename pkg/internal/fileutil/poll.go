package fileutil

import (
	"context"
	"encoding/json"
	"fmt"
)

// PollJSON performs a validated-redirect GET request against rawURL and
// decodes the JSON response body into target (skipped when target is nil).
//
// It is the polling counterpart to Download/DownloadWithMetadata, for
// provider job/task/video status pollers that GET a URL taken from a prior
// API response (a prediction ID, a task URL, a `result_url`, ...) rather than
// a URL the caller fully controls. Such a URL — and every redirect hop it
// produces — must be treated as untrusted input and validated the same way a
// downloaded file's URL is (TS fetchWithValidatedRedirects): reject
// SSRF-prone targets (loopback/private/link-local/metadata addresses) and
// drop credential headers before following a redirect across origins.
//
// Callers building opts should start from TrustedOriginDownloadOptions (or
// TrustedURLValidator/TrustRoutingTransport directly) with the provider's own
// base URL as the trusted origin, so the initial hop to that origin keeps its
// Authorization header while any redirect elsewhere does not:
//
//	opts := fileutil.TrustedOriginDownloadOptions(provider.BaseURL, nil)
//	opts.Headers = map[string]string{"Authorization": "Bearer " + apiKey}
//	var status jobStatusResponse
//	if _, err := fileutil.PollJSON(ctx, pollURL, opts, &status); err != nil {
//		return err
//	}
func PollJSON(ctx context.Context, rawURL string, opts DownloadOptions, target interface{}) (*DownloadResult, error) {
	result, err := DownloadWithMetadata(ctx, rawURL, opts)
	if err != nil {
		return nil, err
	}
	if target != nil {
		if err := json.Unmarshal(result.Data, target); err != nil {
			return nil, fmt.Errorf("fileutil: failed to decode poll response from %s: %w", rawURL, err)
		}
	}
	return result, nil
}
