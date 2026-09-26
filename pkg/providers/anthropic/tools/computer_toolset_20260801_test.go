package tools

import (
	"testing"
)

func TestComputerToolset20260801Serialization(t *testing.T) {
	tool := ComputerToolset20260801(ComputerToolset20260801Config{})

	if tool.Name != "anthropic.computer_toolset_20260801" {
		t.Errorf("tool.Name = %q, want %q", tool.Name, "anthropic.computer_toolset_20260801")
	}
	if !tool.ProviderExecuted {
		t.Error("tool.ProviderExecuted should be true")
	}

	opts, ok := tool.ProviderOptions.(*computerToolset20260801Opts)
	if !ok {
		t.Fatalf("tool.ProviderOptions is not *computerToolset20260801Opts")
	}

	apiMap := opts.ToAnthropicAPIMap()

	if apiMap["type"] != "computer_toolset_20260801" {
		t.Errorf("apiMap[type] = %v, want computer_toolset_20260801", apiMap["type"])
	}
	// Unlike other computer tools, the wire shape has no "name" field.
	if _, hasName := apiMap["name"]; hasName {
		t.Error("apiMap should not have a name field for computer_toolset_20260801")
	}
	if _, hasConfigs := apiMap["configs"]; hasConfigs {
		t.Error("apiMap should not have configs when Configs is empty")
	}
}

func TestComputerToolset20260801WithConfigs(t *testing.T) {
	disabled := false
	deferred := true
	tool := ComputerToolset20260801(ComputerToolset20260801Config{
		Configs: map[ComputerToolset20260801Member]ComputerToolset20260801MemberConfig{
			ComputerToolsetZoom: {Enabled: &disabled},
			ComputerToolsetKey:  {DeferLoading: &deferred},
		},
	})

	opts := tool.ProviderOptions.(*computerToolset20260801Opts)
	apiMap := opts.ToAnthropicAPIMap()

	configs, ok := apiMap["configs"].(map[string]interface{})
	if !ok {
		t.Fatalf("apiMap[configs] is not a map, got %T", apiMap["configs"])
	}

	zoomCfg, ok := configs["zoom"].(map[string]interface{})
	if !ok {
		t.Fatalf("configs[zoom] is not a map")
	}
	if zoomCfg["enabled"] != false {
		t.Errorf("configs[zoom].enabled = %v, want false", zoomCfg["enabled"])
	}
	if _, has := zoomCfg["defer_loading"]; has {
		t.Error("configs[zoom] should not have defer_loading when unset")
	}

	keyCfg, ok := configs["key"].(map[string]interface{})
	if !ok {
		t.Fatalf("configs[key] is not a map")
	}
	if keyCfg["defer_loading"] != true {
		t.Errorf("configs[key].defer_loading = %v, want true", keyCfg["defer_loading"])
	}
	if _, has := keyCfg["enabled"]; has {
		t.Error("configs[key] should not have enabled when unset")
	}
}
