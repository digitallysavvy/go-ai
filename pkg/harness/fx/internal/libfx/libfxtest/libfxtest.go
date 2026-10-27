// Package libfxtest gates the tests that run the real fx-core.wasm.
//
// Locally they run when the pinned module is cached and skip otherwise. In
// CI a skip would hide a broken runtime, so they must run: with
// GOAI_FX_WASM_TEST=1, or whenever CI is set (GitHub Actions sets CI=true),
// a module that cannot be prepared fails the test instead of skipping it.
// GOAI_FX_WASM_TEST=0 skips them explicitly.
package libfxtest

import (
	"context"
	"os"
	"testing"
	"time"
)

// EnvWasmTest selects the real-wasm test mode: "1" downloads the module
// if needed and fails when it cannot be prepared; "0" skips the tests.
const EnvWasmTest = "GOAI_FX_WASM_TEST"

// Prepare makes the pinned module available. offline forbids the network;
// unavailable reports whether an error means the module is simply not
// cached.
type Prepare func(ctx context.Context, offline bool) error

// Mode is how RequireWasm treats a missing module.
type Mode int

const (
	// ModeCachedOnly runs the tests only when the module is cached.
	ModeCachedOnly Mode = iota
	// ModeRequired downloads the module if needed and fails without it.
	ModeRequired
	// ModeSkip skips the tests.
	ModeSkip
)

// CurrentMode reads the mode from the environment.
func CurrentMode() Mode {
	switch os.Getenv(EnvWasmTest) {
	case "1":
		return ModeRequired
	case "0":
		return ModeSkip
	}
	if os.Getenv("CI") != "" {
		return ModeRequired
	}
	return ModeCachedOnly
}

// RequireWasm prepares the pinned fx-core.wasm or skips or fails t, as
// CurrentMode says. After it returns the module is cached, so tests can
// load it offline.
func RequireWasm(t testing.TB, version string, prepare Prepare, unavailable func(error) bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	switch CurrentMode() {
	case ModeSkip:
		t.Skipf("%s=0: skipping the real fx-core.wasm %s tests", EnvWasmTest, version)
	case ModeRequired:
		if err := prepare(ctx, false); err != nil {
			t.Fatalf("the real fx-core.wasm %s tests are required here (%s=1 or CI set) but the module could not be prepared: %v", version, EnvWasmTest, err)
		}
	default:
		if err := prepare(ctx, true); err != nil {
			if unavailable(err) {
				t.Skipf("fx-core.wasm %s is not cached; set %s=1 to download it: %v", version, EnvWasmTest, err)
			}
			t.Fatal(err)
		}
	}
}
