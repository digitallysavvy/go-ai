package jsonexact

import "testing"

type r95carryNested struct {
	ID string `json:"ID"`
}

type r95carryOuter struct {
	ID    string         `json:"id"`
	Inner r95carryNested `json:"inner"`
}

// A key spelled exactly like a field of some other reachable struct must
// still be dropped where its own struct only folds to it: "ID" is exact for
// r95carryNested but only a case-insensitive match for r95carryOuter.id.
func TestR95carryFilterDropsKeyExactElsewhere(t *testing.T) {
	var out r95carryOuter
	if err := Unmarshal([]byte(`{"ID":"x","inner":{"ID":"y"}}`), &out); err != nil {
		t.Fatal(err)
	}
	if out.ID != "" || out.Inner.ID != "y" {
		t.Errorf("got %+v, want id empty and inner.ID y", out)
	}
}

type r95carryY struct {
	Value string `json:"value"`
}

type r95carryX struct {
	Value string `json:"VALUE"`
}

type r95carryBase struct {
	Data r95carryY `json:"data"`
}

type r95carryDominant struct {
	r95carryBase
	Data r95carryX `json:"data"`
}

// Field names follow encoding/json's embedding rules: the shallower field
// wins (the outer data is a r95carryX, so its exact "VALUE" key is kept).
func TestR95carryFilterFollowsEmbeddingRules(t *testing.T) {
	var out r95carryDominant
	if err := Unmarshal([]byte(`{"data":{"VALUE":"v"}}`), &out); err != nil {
		t.Fatal(err)
	}
	if out.Data.Value != "v" {
		t.Errorf("outer data.VALUE = %q, want v", out.Data.Value)
	}
}
