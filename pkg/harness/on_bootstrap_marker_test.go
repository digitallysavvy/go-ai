package harness

import (
	"context"
	"regexp"
	"testing"
)

var onBootstrapMarkerPathPattern = regexp.MustCompile(`^/home/agent/\.ai-sdk-harness/\.on-bootstrap/[0-9a-f]{64}\.ok$`)

func TestOnBootstrapMarkerPath(t *testing.T) {
	sb := newMockSandbox()
	path, err := OnBootstrapMarkerPath(context.Background(), sb, "my-hash")
	if err != nil {
		t.Fatal(err)
	}
	if !onBootstrapMarkerPathPattern.MatchString(path) {
		t.Fatalf("path = %q", path)
	}
	// Deterministic: the same hash always resolves to the same path.
	again, err := OnBootstrapMarkerPath(context.Background(), sb, "my-hash")
	if err != nil {
		t.Fatal(err)
	}
	if again != path {
		t.Fatalf("path changed across calls: %q vs %q", path, again)
	}
	// A different hash resolves to a different path.
	other, err := OnBootstrapMarkerPath(context.Background(), sb, "other-hash")
	if err != nil {
		t.Fatal(err)
	}
	if other == path {
		t.Fatal("different bootstrapHash values must not collide")
	}
}

func TestHasOnBootstrapMarker_WriteOnBootstrapMarker(t *testing.T) {
	ctx := context.Background()
	sb := newMockSandbox()

	marked, err := HasOnBootstrapMarker(ctx, sb, "h1")
	if err != nil {
		t.Fatal(err)
	}
	if marked {
		t.Fatal("no marker should exist yet")
	}

	if err := WriteOnBootstrapMarker(ctx, sb, "h1"); err != nil {
		t.Fatal(err)
	}
	marked, err = HasOnBootstrapMarker(ctx, sb, "h1")
	if err != nil {
		t.Fatal(err)
	}
	if !marked {
		t.Fatal("marker should exist after WriteOnBootstrapMarker")
	}

	// A marker for a different hash is independent.
	marked, err = HasOnBootstrapMarker(ctx, sb, "h2")
	if err != nil {
		t.Fatal(err)
	}
	if marked {
		t.Fatal("marker for a different bootstrapHash must not be set")
	}
}
