package gateway

import "testing"

// TS acea2b5ec5 removed xiaomi/mimo-v2.5 and -pro from GatewayModelId: they
// stay as deprecated constants and leave the suggested catalog.
func TestR87provaMimoV25Deprecated(t *testing.T) {
	if GatewayLanguageModelXiaomiMimoV25 != "xiaomi/mimo-v2.5" || GatewayLanguageModelXiaomiMimoV25Pro != "xiaomi/mimo-v2.5-pro" {
		t.Fatal("deprecated constants changed value")
	}
	for _, v := range catalogs()["language"] {
		if v == "xiaomi/mimo-v2.5" || v == "xiaomi/mimo-v2.5-pro" {
			t.Fatalf("%q still in the language catalog", v)
		}
	}
}
