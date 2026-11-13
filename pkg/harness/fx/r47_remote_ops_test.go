package fx

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/digitallysavvy/go-ai/pkg/providerutils"
)

// TS bash builds AbortSignal.timeout(timeout * 1000): Node rejects a delay
// that is not an integer or exceeds 4294967295 ms (the tool fails with
// ERR_OUT_OF_RANGE's message), and fires after 1 ms for one past the signed
// 32-bit range. Go used to convert the seconds straight to a Duration, which
// overflows for huge values.
func TestR47BashTimeoutFollowsNodeAbortSignalTimeout(t *testing.T) {
	for _, tc := range []struct {
		timeout string
		wantErr string
		wantMax time.Duration
	}{
		{"1.005", `The value of "delay" is out of range. It must be an integer. Received 1004.9999999999999`, 0},
		{"4294968", `The value of "delay" is out of range. It must be >= 0 && <= 4294967295. Received 4_294_968_000`, 0},
		{"1e10", `The value of "delay" is out of range. It must be >= 0 && <= 4294967295. Received 10_000_000_000_000`, 0},
		{"2147484", "", 50 * time.Millisecond},
		{"120", "", 121 * time.Second},
	} {
		sb := newFakeSandbox()
		var deadline time.Duration
		var ran bool
		sb.handle = func(ctx context.Context, _ providerutils.SandboxProcessOptions) (providerutils.SandboxRunResult, error) {
			ran = true
			if d, ok := ctx.Deadline(); ok {
				deadline = time.Until(d)
			}
			return providerutils.SandboxRunResult{}, nil
		}
		r := newRemoteOps(sb, "/workspace")
		_, err := r.execute(context.Background(), "bash", json.RawMessage(`{"command":"true","timeout":`+tc.timeout+`}`))
		if tc.wantErr != "" {
			if err == nil || err.Error() != tc.wantErr || ran {
				t.Errorf("timeout %s: err = %v, ran = %v, want %q", tc.timeout, err, ran, tc.wantErr)
			}
			continue
		}
		if err != nil || !ran || deadline <= -time.Second || deadline > tc.wantMax {
			t.Errorf("timeout %s: err = %v, deadline in %v, want at most %v", tc.timeout, err, deadline, tc.wantMax)
		}
	}
}

// Array.prototype.slice(0, limit) truncates the limit toward zero, so a
// limit in (-1, 0) is -0 and returns nothing; a large limit formats like
// JavaScript's String(n) in the grep head command.
func TestR47LimitSliceAndNumberFormatMatchJS(t *testing.T) {
	values := []string{"a", "b", "c"}
	for _, tc := range []struct {
		limit float64
		want  []string
	}{
		{-0.5, []string{}}, {-1.5, []string{"a", "b"}}, {-5, []string{}}, {2.7, []string{"a", "b"}}, {1e300, values},
	} {
		l := tc.limit
		if got := limitStrings(values, &l, 10); !reflect.DeepEqual(append([]string{}, got...), tc.want) {
			t.Errorf("limit %v = %q, want %q", tc.limit, got, tc.want)
		}
	}
	sb := newFakeSandbox()
	var grepCmd string
	sb.handle = func(_ context.Context, opts providerutils.SandboxProcessOptions) (providerutils.SandboxRunResult, error) {
		if strings.Contains(opts.Command, "realpath") {
			return providerutils.SandboxRunResult{Stdout: b64("/workspace\x00")}, nil
		}
		grepCmd = opts.Command
		return providerutils.SandboxRunResult{}, nil
	}
	r := newRemoteOps(sb, "/workspace")
	if _, err := r.execute(context.Background(), "grep", json.RawMessage(`{"pattern":"x","limit":1e21}`)); err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(grepCmd, "| head -n 1e+21") {
		t.Errorf("grep command = %q", grepCmd)
	}
}
