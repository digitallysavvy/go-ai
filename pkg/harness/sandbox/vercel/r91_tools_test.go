package vercel

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// core3-08 (undici fetch httpRedirectFetch): a cross-origin redirect hop
// drops Authorization, Proxy-Authorization and Cookie.
func TestR91toolsVercelRedirectDropsCredentialHeaders(t *testing.T) {
	var got http.Header
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = r.Header.Clone()
	}))
	t.Cleanup(target.Close)
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/next", http.StatusFound)
	}))
	t.Cleanup(origin.Close)
	client := &http.Client{CheckRedirect: dropCredentialsOnCrossOriginRedirect}
	req, _ := http.NewRequest(http.MethodGet, origin.URL, nil)
	req.Header.Set("Authorization", "Bearer x")
	req.Header.Set("Proxy-Authorization", "Basic x")
	req.Header.Set("Cookie", "s=1")
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	for _, h := range []string{"Authorization", "Proxy-Authorization", "Cookie"} {
		if got.Get(h) != "" {
			t.Errorf("%s reached the other origin", h)
		}
	}
}
