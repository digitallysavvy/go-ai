package ai

import (
	"testing"

	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
)

// Ported from TS provider-utils/src/generate-id.test.ts.
func TestCreateIDGenerator_CorrectLength(t *testing.T) {
	t.Parallel()

	gen, err := CreateIDGenerator(CreateIDGeneratorOptions{Size: 10})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := len(gen()); got != 10 {
		t.Errorf("expected length 10, got %d", got)
	}
}

func TestCreateIDGenerator_DefaultLength(t *testing.T) {
	t.Parallel()

	gen, err := CreateIDGenerator()
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if got := len(gen()); got != 16 {
		t.Errorf("expected default length 16, got %d", got)
	}
}

func TestCreateIDGenerator_SeparatorInAlphabetErrors(t *testing.T) {
	t.Parallel()

	_, err := CreateIDGenerator(CreateIDGeneratorOptions{Separator: "a", Prefix: "b"})
	if err == nil {
		t.Fatal("expected error when separator is part of the alphabet")
	}
	if !providererrors.IsInvalidArgumentError(err) {
		t.Errorf("expected InvalidArgumentError, got %T: %v", err, err)
	}
}

func TestCreateIDGenerator_SeparatorNotInAlphabetNoError(t *testing.T) {
	t.Parallel()

	// Default alphabet doesn't contain '-', the default separator.
	if _, err := CreateIDGenerator(CreateIDGeneratorOptions{Prefix: "msg"}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestCreateIDGenerator_PrefixAndSeparatorComposition(t *testing.T) {
	t.Parallel()

	gen, err := CreateIDGenerator(CreateIDGeneratorOptions{Prefix: "msg", Separator: "_", Size: 4})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	id := gen()
	wantPrefix := "msg_"
	if len(id) != len(wantPrefix)+4 {
		t.Fatalf("unexpected id length: %q", id)
	}
	if id[:len(wantPrefix)] != wantPrefix {
		t.Errorf("expected id to start with %q, got %q", wantPrefix, id)
	}
}

func TestCreateIDGenerator_CustomAlphabet(t *testing.T) {
	t.Parallel()

	gen, err := CreateIDGenerator(CreateIDGeneratorOptions{Alphabet: "01", Size: 20})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	id := gen()
	for _, c := range id {
		if c != '0' && c != '1' {
			t.Fatalf("id %q contains character outside alphabet", id)
		}
	}
}

func TestGenerateID_UniqueIDs(t *testing.T) {
	t.Parallel()

	id1 := GenerateID()
	id2 := GenerateID()

	if id1 == id2 {
		t.Errorf("expected unique IDs, got %q twice", id1)
	}
	if len(id1) != 16 {
		t.Errorf("expected default length 16, got %d", len(id1))
	}
}
