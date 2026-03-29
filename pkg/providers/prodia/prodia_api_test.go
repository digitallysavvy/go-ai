package prodia

import (
	"encoding/json"
	"testing"
)

// TestBuildProdiaProviderMetadataFields verifies that buildProdiaProviderMetadata
// emits field names that match the TypeScript SDK exactly.
func TestBuildProdiaProviderMetadataFields(t *testing.T) {
	elapsed := 1.23
	ips := 4.56
	seed := float64(42)
	dollars := 0.001

	job := &prodiaJobResponse{
		ID:        "job-abc",
		CreatedAt: "2024-01-01T00:00:00Z",
		UpdatedAt: "2024-01-01T00:01:00Z",
		Config:    &prodiaJobConfig{Seed: &seed},
		Metrics:   &prodiaMetrics{Elapsed: &elapsed, IPS: &ips},
		Price:     &prodiaPrice{Product: "inference", Dollars: &dollars},
	}

	meta := buildProdiaProviderMetadata(job)

	// jobId
	if meta["jobId"] != "job-abc" {
		t.Errorf("jobId = %v, want %q", meta["jobId"], "job-abc")
	}
	// seed comes from config.seed, not metrics
	if meta["seed"] != seed {
		t.Errorf("seed = %v, want %v", meta["seed"], seed)
	}
	// elapsed (not "elapsedTime")
	if _, ok := meta["elapsedTime"]; ok {
		t.Error("unexpected field 'elapsedTime': TS SDK uses 'elapsed'")
	}
	if meta["elapsed"] != elapsed {
		t.Errorf("elapsed = %v, want %v", meta["elapsed"], elapsed)
	}
	// iterationsPerSecond (not "iterationsPerSec")
	if _, ok := meta["iterationsPerSec"]; ok {
		t.Error("unexpected field 'iterationsPerSec': TS SDK uses 'iterationsPerSecond'")
	}
	if meta["iterationsPerSecond"] != ips {
		t.Errorf("iterationsPerSecond = %v, want %v", meta["iterationsPerSecond"], ips)
	}
	// dollars (not "cost" or "total")
	if _, ok := meta["cost"]; ok {
		t.Error("unexpected field 'cost': TS SDK uses 'dollars'")
	}
	if _, ok := meta["total"]; ok {
		t.Error("unexpected field 'total': TS SDK uses 'dollars'")
	}
	if meta["dollars"] != dollars {
		t.Errorf("dollars = %v, want %v", meta["dollars"], dollars)
	}
	// createdAt / updatedAt
	if meta["createdAt"] != "2024-01-01T00:00:00Z" {
		t.Errorf("createdAt = %v, want %q", meta["createdAt"], "2024-01-01T00:00:00Z")
	}
	if meta["updatedAt"] != "2024-01-01T00:01:00Z" {
		t.Errorf("updatedAt = %v, want %q", meta["updatedAt"], "2024-01-01T00:01:00Z")
	}
}

// TestBuildProdiaProviderMetadataMinimal verifies that a minimal job response
// (only ID) returns just jobId without panicking and without extra fields.
func TestBuildProdiaProviderMetadataMinimal(t *testing.T) {
	job := &prodiaJobResponse{ID: "job-min"}
	meta := buildProdiaProviderMetadata(job)
	if meta["jobId"] != "job-min" {
		t.Errorf("jobId = %v, want %q", meta["jobId"], "job-min")
	}
	for _, unwanted := range []string{"seed", "elapsed", "iterationsPerSecond", "dollars", "createdAt", "updatedAt"} {
		if _, ok := meta[unwanted]; ok {
			t.Errorf("unexpected field %q in minimal metadata", unwanted)
		}
	}
}

// TestBuildProdiaProviderMetadataNil verifies that a nil job returns nil.
func TestBuildProdiaProviderMetadataNil(t *testing.T) {
	if got := buildProdiaProviderMetadata(nil); got != nil {
		t.Errorf("expected nil for nil job, got %v", got)
	}
}

// TestProdiaJobResponseJSONDeserialization verifies the struct correctly
// deserialises the Prodia API JSON response, matching the TS prodiaJobResultSchema.
func TestProdiaJobResponseJSONDeserialization(t *testing.T) {
	const raw = `{
		"id": "j-123",
		"created_at": "2024-03-01T12:00:00Z",
		"updated_at": "2024-03-01T12:00:05Z",
		"state": { "current": "succeeded" },
		"config": { "seed": 99.0, "prompt": "a cat" },
		"metrics": { "elapsed": 3.5, "ips": 10.2 },
		"price": { "product": "inference", "dollars": 0.0025 }
	}`

	var j prodiaJobResponse
	if err := json.Unmarshal([]byte(raw), &j); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if j.ID != "j-123" {
		t.Errorf("ID = %q, want %q", j.ID, "j-123")
	}
	if j.CreatedAt != "2024-03-01T12:00:00Z" {
		t.Errorf("CreatedAt = %q", j.CreatedAt)
	}
	if j.State == nil || j.State.Current != "succeeded" {
		t.Errorf("State.Current missing or wrong: %v", j.State)
	}
	if j.Config == nil || j.Config.Seed == nil || *j.Config.Seed != 99.0 {
		t.Errorf("Config.Seed = %v", j.Config)
	}
	if j.Metrics == nil || j.Metrics.Elapsed == nil || *j.Metrics.Elapsed != 3.5 {
		t.Errorf("Metrics.Elapsed = %v", j.Metrics)
	}
	if j.Metrics.IPS == nil || *j.Metrics.IPS != 10.2 {
		t.Errorf("Metrics.IPS = %v", j.Metrics)
	}
	if j.Price == nil || j.Price.Dollars == nil || *j.Price.Dollars != 0.0025 {
		t.Errorf("Price.Dollars = %v", j.Price)
	}
}

// TestProdiaJobResponseStateIsObject verifies that state unmarshals as a struct
// (not a plain string), matching the TS schema: state: { current: string }.
func TestProdiaJobResponseStateIsObject(t *testing.T) {
	const raw = `{"id":"j-1","state":{"current":"processing"}}`
	var j prodiaJobResponse
	if err := json.Unmarshal([]byte(raw), &j); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if j.State == nil {
		t.Fatal("State is nil")
	}
	if j.State.Current != "processing" {
		t.Errorf("State.Current = %q, want %q", j.State.Current, "processing")
	}
}
