package fileutil

import "testing"

func TestR89integNoTimeoutDownloadOptions(t *testing.T) {
	got, def := NoTimeoutDownloadOptions(), DefaultDownloadOptions()
	if got.Timeout != NoTimeout {
		t.Fatalf("Timeout = %v, want NoTimeout", got.Timeout)
	}
	if got.MaxSize != def.MaxSize || (got.URLValidator == nil) != (def.URLValidator == nil) {
		t.Fatalf("limits changed: %+v vs %+v", got, def)
	}
	if c := NewDownloadClient("https://example.com/a", got); c.Timeout != 0 {
		t.Fatalf("client Timeout = %v, want none", c.Timeout)
	}
}
