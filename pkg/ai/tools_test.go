package ai

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func TestFilterActiveTools(t *testing.T) {
	tools := []types.Tool{
		{Name: "weather"},
		{Name: "time"},
		{Name: "search"},
	}

	if got := FilterActiveTools(nil, []string{"weather"}); got != nil {
		t.Fatalf("nil input should return nil, got %#v", got)
	}

	all := FilterActiveTools(tools, nil)
	if len(all) != 3 {
		t.Fatalf("active=nil should keep all tools, got %d", len(all))
	}
	if &all[0] != &tools[0] {
		t.Fatal("active=nil should return original slice")
	}

	filtered := FilterActiveTools(tools, []string{"time", "missing"})
	if len(filtered) != 1 || filtered[0].Name != "time" {
		t.Fatalf("filtered tools mismatch: %#v", filtered)
	}
}

func TestExperimentalFilterActiveToolsAlias(t *testing.T) {
	tools := []types.Tool{{Name: "a"}, {Name: "b"}}
	got := ExperimentalFilterActiveTools(tools, []string{"b"})
	if len(got) != 1 || got[0].Name != "b" {
		t.Fatalf("alias filtering mismatch: %#v", got)
	}
}
