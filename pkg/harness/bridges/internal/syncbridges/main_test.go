package main

import (
	"encoding/json"
	"os"
	"testing"
)

func TestDefaultsMatchVersionsJSON(t *testing.T) {
	data, err := os.ReadFile("../../VERSIONS.json")
	if err != nil {
		t.Fatal(err)
	}
	var m struct {
		TsTag    string `json:"tsTag"`
		TsCommit string `json:"tsCommit"`
	}
	if err := json.Unmarshal(data, &m); err != nil {
		t.Fatal(err)
	}
	if m.TsTag != defaultTag || m.TsCommit != defaultCommit {
		t.Fatalf("defaults %s/%s != VERSIONS.json %s/%s", defaultTag, defaultCommit, m.TsTag, m.TsCommit)
	}
}
