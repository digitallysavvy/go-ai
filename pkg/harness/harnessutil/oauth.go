package harnessutil

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/harness/harnessutil/subscription"
)

// DefaultRefreshWindow is the default window before expiry in which an access
// token is considered expiring.
const DefaultRefreshWindow = 5 * time.Minute

// OAuthCredential is a stored OAuth credential. ExpiresAt is epoch
// milliseconds.
type OAuthCredential struct {
	AccessToken  string `json:"accessToken"`
	RefreshToken string `json:"refreshToken"`
	ExpiresAt    int64  `json:"expiresAt"`
}

// RefreshOAuthAccessTokenResult mirrors TS `RefreshOAuthAccessTokenResult`.
// RefreshToken is empty when the server did not rotate it.
type RefreshOAuthAccessTokenResult struct {
	AccessToken  string `json:"accessToken"`
	ExpiresAt    int64  `json:"expiresAt"`
	RefreshToken string `json:"refreshToken,omitempty"`
}

// IsAccessTokenExpiringSoon reports expiresAt <= now + refreshWindow
// (inclusive). All values are epoch milliseconds; refreshWindowMs <= 0 uses the
// 5 minute default. Mirrors TS `isAccessTokenExpiringSoon`.
func IsAccessTokenExpiringSoon(expiresAt, now, refreshWindowMs int64) bool {
	if refreshWindowMs <= 0 {
		refreshWindowMs = DefaultRefreshWindow.Milliseconds()
	}
	return expiresAt <= now+refreshWindowMs
}

// RefreshOAuthAccessTokenOptions is the input of RefreshOAuthAccessToken.
type RefreshOAuthAccessTokenOptions struct {
	TokenURL     string
	ClientID     string
	RefreshToken string
	// RequestFormat is "form" (default) or "json".
	RequestFormat string
	Headers       map[string]string
	// HTTPClient defaults to http.DefaultClient.
	HTTPClient *http.Client
	// Now defaults to time.Now.
	Now func() time.Time
}

// RefreshOAuthAccessToken performs a refresh_token grant. Error messages never
// include tokens or the response body. Mirrors TS `refreshOAuthAccessToken`.
func RefreshOAuthAccessToken(ctx context.Context, opts RefreshOAuthAccessTokenOptions) (*RefreshOAuthAccessTokenResult, error) {
	client := opts.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	now := opts.Now
	if now == nil {
		now = time.Now
	}

	var body []byte
	contentType := "application/x-www-form-urlencoded"
	if opts.RequestFormat == "json" {
		contentType = "application/json"
		// Key order matches the TS object literal.
		body = []byte(fmt.Sprintf(`{"grant_type":"refresh_token","client_id":%s,"refresh_token":%s}`, jsonString(opts.ClientID), jsonString(opts.RefreshToken)))
	} else {
		body = []byte("grant_type=refresh_token&client_id=" + url.QueryEscape(opts.ClientID) + "&refresh_token=" + url.QueryEscape(opts.RefreshToken))
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, opts.TokenURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("content-type", contentType)
	for k, v := range opts.Headers {
		req.Header.Set(k, v)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close() //nolint:errcheck
	responseText, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		return nil, fmt.Errorf("OAuth access token refresh failed with status %d.", resp.StatusCode)
	}

	var parsed map[string]any
	if err := json.Unmarshal(responseText, &parsed); err != nil || parsed == nil {
		return nil, errors.New("OAuth access token refresh returned invalid JSON.")
	}
	accessToken, _ := parsed["access_token"].(string)
	if accessToken == "" {
		return nil, errors.New("OAuth access token refresh response is missing access_token.")
	}

	var expiresAt int64
	if expiresIn, ok := parsed["expires_in"].(float64); ok && !math.IsInf(expiresIn, 0) && !math.IsNaN(expiresIn) && expiresIn >= 0 {
		expiresAt = now().UnixMilli() + int64(expiresIn*1000)
	} else if jwtExpiresAt, ok := subscription.GetJWTExpiresAt(accessToken); ok {
		expiresAt = jwtExpiresAt
	} else {
		return nil, errors.New("OAuth access token refresh response does not include a usable expiry.")
	}

	result := &RefreshOAuthAccessTokenResult{AccessToken: accessToken, ExpiresAt: expiresAt}
	if rotated, present := parsed["refresh_token"]; present && rotated != nil {
		s, ok := rotated.(string)
		if !ok || s == "" {
			return nil, errors.New("OAuth access token refresh response contains an invalid refresh_token.")
		}
		result.RefreshToken = s
	}
	return result, nil
}

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}
