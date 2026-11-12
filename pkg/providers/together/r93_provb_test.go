package together

import "testing"

// TS images = response.data.map(item => item.b64_json): every item is an
// image ("" included), decoded by core with atob's forgiving rules.
func TestR93provbImageBase64(t *testing.T) {
	m := NewImageModel(New(Config{APIKey: "k"}), "black-forest-labs/FLUX.1-schnell")
	res, err := m.convertResponse([]byte(`{"data":[{"b64_json":"aGVs\nbG8"},{"b64_json":""}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Images) != 2 || string(res.Images[0]) != "hello" || len(res.Base64Images) != 2 || res.Base64Images[1] != "" {
		t.Fatalf("images = %q, base64 = %q", res.Images, res.Base64Images)
	}
}
