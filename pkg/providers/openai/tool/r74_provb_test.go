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
