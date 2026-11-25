package azure

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// TS 'advertises background only for MAI-Image-2.7 models'
// (azure-mai-image-model.test.ts) reads azureModelCatalog metadata through
// getAzureModelEntry. Go has no capability catalog: the metadata (capability
// and provider-option flags) is a TS-only surface, and neither TS nor Go gates
// the background option by model (the request sends whatever the caller
// passes). The only catalog fact that drives Go behavior is which model IDs
// route to the MAI image API, so this differential test compares that set with
// the TS catalog source when the TS checkout is present.
func TestPort2provbMAIImageModelsMatchTSCatalog(t *testing.T) {
	path := filepath.Join("..", "..", "..", "..", "..", "ai", "packages", "azure", "src", "azure-model-catalog.ts")
	if root := os.Getenv("GOAI_TS_CHECKOUT"); root != "" {
		path = filepath.Join(root, "packages", "azure", "src", "azure-model-catalog.ts")
	}
	src, err := os.ReadFile(path)
	if err != nil {
		t.Skipf("TS checkout not present: %v", err)
	}
	re := regexp.MustCompile(`(?m)^\s*'(mai-image-[^']+)':\s*imageEntry\(`)
	var want []string
	for _, m := range re.FindAllStringSubmatch(string(src), -1) {
		want = append(want, m[1])
	}
	if len(want) == 0 {
		t.Fatal("no mai-image entries found in the TS catalog source")
	}
	var got []string
	for id := range azureMaiImageModels {
		got = append(got, id)
	}
	sort.Strings(want)
	sort.Strings(got)
	if strings.Join(got, ",") != strings.Join(want, ",") {
		t.Fatalf("Go MAI image model IDs = %v, TS catalog = %v", got, want)
	}
	for _, id := range want {
		if !isMAIImageModel(strings.ToUpper(id[:1]) + id[1:]) {
			t.Errorf("%s: not routed case-insensitively", id)
		}
	}
	// Unknown IDs behave as before (no MAI routing).
	for _, id := range []string{"mai-image-2.8", "my-deployment", ""} {
		if isMAIImageModel(id) {
			t.Errorf("%q routed to the MAI image API", id)
		}
	}
}
