package anthropic

import (
	"strconv"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// r76provaMixedNames builds a per-request batchToolNames with n entries.
func r76provaMixedNames(n int) *batchToolNames {
	shared := map[string]string{"bash": "anthropic.bash_20250124"}
	by := make(map[string]map[string]string, n)
	for i := 0; i < n; i++ {
		by["req-"+strconv.Itoa(i)] = shared
	}
	return &batchToolNames{byRequest: by}
}

// The store bounds the per-request entries it keeps across batches, not
// only the batch count, and always keeps the newest batch.
func TestR76prova_BatchToolNameStoreBoundsRequestEntries(t *testing.T) {
	var s batchToolNameStore
	per := maxRememberedBatchToolNameRequests / 4
	for i := 0; i < 6; i++ {
		s.put("batch-"+strconv.Itoa(i), r76provaMixedNames(per))
	}
	if s.requests > maxRememberedBatchToolNameRequests {
		t.Fatalf("requests = %d, cap %d", s.requests, maxRememberedBatchToolNameRequests)
	}
	if len(s.names) != 4 || s.get("batch-1") != nil || s.get("batch-2") == nil || s.get("batch-5") == nil {
		t.Fatalf("kept %d batches", len(s.names))
	}

	// One batch over the cap on its own is still kept (it is the newest).
	s.put("huge", r76provaMixedNames(maxRememberedBatchToolNameRequests+1))
	if len(s.names) != 1 || s.get("huge") == nil || s.requests != maxRememberedBatchToolNameRequests+1 {
		t.Fatalf("names=%d requests=%d", len(s.names), s.requests)
	}

	// Re-putting a batch ID replaces its count.
	s.put("huge", &batchToolNames{all: map[string]string{"bash": "x"}})
	if s.requests != 1 || len(s.order) != 1 {
		t.Fatalf("requests=%d order=%d", s.requests, len(s.order))
	}
}

// TS anthropic prepareTools sends allowed_callers and input_examples (and the
// advanced-tool-use beta) whenever they are given, `!= null`: an empty list
// is sent, an absent one is not.
func TestR76prova_EmptyAllowedCallersAndInputExamplesSent(t *testing.T) {
	schema := map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}
	run := func(tool types.Tool) preparedTools {
		return prepareTools(prepareToolsOptions{tools: []types.Tool{tool}})
	}
	empty := run(types.Tool{Name: "f", Parameters: schema, InputExamples: []types.ToolInputExample{},
		ProviderOptions: map[string]interface{}{"anthropic": map[string]interface{}{"allowedCallers": []interface{}{}}}})
	m := empty.tools[0]
	if v, ok := m["allowed_callers"].([]string); !ok || len(v) != 0 {
		t.Fatalf("allowed_callers = %#v", m["allowed_callers"])
	}
	if v, ok := m["input_examples"].([]interface{}); !ok || len(v) != 0 {
		t.Fatalf("input_examples = %#v", m["input_examples"])
	}
	if len(empty.betas) != 1 || empty.betas[0] != BetaHeaderAdvancedToolUse {
		t.Fatalf("betas = %v", empty.betas)
	}
	typed := run(types.Tool{Name: "f", Parameters: schema, ProviderOptions: &ToolOptions{AllowedCallers: []string{}}})
	if _, ok := typed.tools[0]["allowed_callers"]; !ok {
		t.Fatalf("typed empty AllowedCallers dropped: %#v", typed.tools[0])
	}
	absent := run(types.Tool{Name: "f", Parameters: schema})
	if _, ok := absent.tools[0]["allowed_callers"]; ok {
		t.Fatal("allowed_callers sent without the option")
	}
	if _, ok := absent.tools[0]["input_examples"]; ok || len(absent.betas) != 0 {
		t.Fatalf("absent options sent: %#v %v", absent.tools[0], absent.betas)
	}
}
