package tool

import "testing"

// TestR74ProvbFileSearchArgsWithoutIDs: TS fileSearch({vectorStoreIds:
// undefined}) has no vectorStoreIds arg (the AI Gateway gets {}); Go sent
// vectorStoreIds: null. An empty list is kept.
func TestR74ProvbFileSearchArgsWithoutIDs(t *testing.T) {
	if _, ok := FileSearch(FileSearchConfig{}).ProviderArgs["vectorStoreIds"]; ok {
		t.Error("FileSearch without IDs sets a vectorStoreIds arg")
	}
	ids, ok := FileSearch(FileSearchConfig{VectorStoreIDs: []string{}}).ProviderArgs["vectorStoreIds"].([]interface{})
	if !ok || len(ids) != 0 {
		t.Errorf("empty IDs arg = %#v, want []", ids)
	}
}

// TestR74ProvbMCPArgsWithoutServerLabel: an unset ServerLabel is absent from
// the TS args (mcp({serverUrl}) without serverLabel), so the args schema
// rejects it as TS does; Go sent serverLabel "".
func TestR74ProvbMCPArgsWithoutServerLabel(t *testing.T) {
	if _, ok := MCP(MCPConfig{ServerURL: "https://m"}).ProviderArgs["serverLabel"]; ok {
		t.Error("MCP without ServerLabel sets a serverLabel arg")
	}
	if got := MCP(MCPConfig{ServerLabel: "x", ServerURL: "https://m"}).ProviderArgs["serverLabel"]; got != "x" {
		t.Errorf("serverLabel arg = %v", got)
	}
}
