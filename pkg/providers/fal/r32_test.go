package fal

import (
	"context"
	"testing"
)

// TS removeOnlyUndefined keeps an explicit null content_type, file_name and
// file_size (all nullish in falImageSchema).
func TestImageMetadataKeepsExplicitNulls(t *testing.T) {
	m := NewImageModel(New(Config{APIKey: "k"}), "fal-fast")
	res, err := m.convertBody(context.Background(), []byte(`{"images":[{"url":"data:image/png;base64,YQ==","content_type":null,"file_name":null,"file_size":null}]}`))
	if err != nil {
		t.Fatal(err)
	}
	img := res.ProviderMetadata["fal"].(map[string]interface{})["images"].([]map[string]interface{})[0]
	for _, k := range []string{"contentType", "fileName", "fileSize"} {
		if v, ok := img[k]; !ok || v != nil {
			t.Errorf("%s = %v, present %v; want present null", k, v, ok)
		}
	}
}
