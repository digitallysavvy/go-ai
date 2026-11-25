package luma

import "testing"

func TestParseImageModelOptionsReadsGoTypedImages(t *testing.T) {
	opts := parseImageModelOptions(map[string]interface{}{"luma": map[string]interface{}{
		"images":          []map[string]interface{}{{"id": "abc", "weight": 0.5}},
		"maxPollAttempts": 3,
	}})
	if opts == nil || len(opts.Images) != 1 || opts.Images[0].ID == nil || *opts.Images[0].ID != "abc" || opts.MaxPollAttempts == nil || *opts.MaxPollAttempts != 3 {
		t.Fatalf("opts = %+v", opts)
	}
}
