//go:build codemode_stress

// This file is gated behind the codemode_stress build tag and is never part
// of a normal `go build`/`go test ./...` run, so its content never affects
// users or CI by default. It exists to soak-test the fix documented in
// pkg/internal/third_party/qjs/README.vendor.md (Mem.ReadString trusting
// QuickJS's reported string length instead of NUL-scanning) end to end,
// through RunCodeMode, under sustained allocation pressure from an
// unrelated workload in the same process -- mirroring how the original bug
// was found (esbuild interleaved with code-mode invocations).
//
// esbuild itself is intentionally NOT imported here, even under this build
// tag: pulling in a compiler as a dependency, even test-only and
// tag-gated, risks `go mod tidy` adding it to go.mod (Go's tooling
// resolves build-tag-gated files' imports too under some tidy/build
// invocations), which the task this file implements explicitly forbids
// ("so esbuild never enters go.mod for users"). go/parser (stdlib) stands
// in as the allocation-heavy workload instead -- parsing generates similar
// bursty AST/token allocation traffic without adding any dependency.
//
// Run explicitly with:
//
//	go test -tags codemode_stress -run TestRunCodeMode_StressManyInvocationsByteExact ./pkg/codemode/...
package codemode

import (
	"context"
	"fmt"
	"go/parser"
	"go/token"
	"math/rand"
	"strings"
	"sync"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// allocHeavyWorkload continuously parses synthetic Go source and churns
// large byte slices, generating sustained bursty allocation/GC pressure in
// this process for the duration of the stress test.
func allocHeavyWorkload(stop <-chan struct{}, wg *sync.WaitGroup) {
	defer wg.Done()

	fset := token.NewFileSet()
	var sb strings.Builder
	for i := 0; ; i++ {
		select {
		case <-stop:
			return
		default:
		}

		sb.Reset()
		sb.WriteString("package stress\n\n")
		for j := 0; j < 200; j++ {
			_, _ = fmt.Fprintf(&sb, "var v%d_%d = %d\nfunc f%d_%d() int { return v%d_%d * %d }\n", i, j, j, i, j, i, j, j)
		}
		_, _ = parser.ParseFile(fset, "", sb.String(), parser.AllErrors)

		junk := make([][]byte, 8)
		for k := range junk {
			junk[k] = make([]byte, 1<<uint(12+(k%8)))
			for b := range junk[k] {
				junk[k][b] = byte(b)
			}
		}
		_ = junk
	}
}

// stressLengths returns 500+ content lengths: every neighborhood around a
// power of two from 2^0 to 2^16 (the original bug report was exactly this
// shape -- "16384 -> 1638", one byte short of a power-of-two-adjacent
// length), plus pseudo-random fill lengths, deterministically seeded.
func stressLengths() []int {
	var lengths []int
	for shift := 0; shift <= 16; shift++ {
		p := 1 << uint(shift)
		for _, d := range []int{-1, 0, 1} {
			l := p + d
			if l < 0 {
				l = 0
			}
			lengths = append(lengths, l)
		}
	}

	rng := rand.New(rand.NewSource(42))
	for len(lengths) < 520 {
		lengths = append(lengths, rng.Intn(20000))
	}
	return lengths
}

// randomStressContent generates an ASCII-only string of exactly length
// runes (so rune count == UTF-16 length as JS sees it == byte length),
// optionally overwriting one rune with an embedded NUL character -- a
// literal "\u0000" inside the JS string value once round-tripped through
// QuickJS.
func randomStressContent(rng *rand.Rand, length int, embedNUL bool) string {
	if length == 0 {
		return ""
	}
	runes := make([]rune, length)
	for i := range runes {
		runes[i] = rune(0x20 + rng.Intn(0x7f-0x20)) // printable ASCII
	}
	if embedNUL {
		runes[rng.Intn(length)] = 0
	}
	return string(runes)
}

// TestRunCodeMode_StressManyInvocationsByteExact is the stress test required
// alongside TestRunCodeMode_ManyInvocationsRemainCorrect (run_code_mode_test.go):
// 500+ RunCodeMode invocations returning strings and JSON objects of
// randomized length (including lengths around powers of two, and content
// containing an embedded NUL inside the JS string), interleaved with an
// allocation-heavy workload in the same process. Every result must be
// byte-exact.
func TestRunCodeMode_StressManyInvocationsByteExact(t *testing.T) {
	stop := make(chan struct{})
	var wg sync.WaitGroup
	const numWorkers = 4
	wg.Add(numWorkers)
	for i := 0; i < numWorkers; i++ {
		go allocHeavyWorkload(stop, &wg)
	}
	defer func() {
		close(stop)
		wg.Wait()
	}()

	lengths := stressLengths()
	rng := rand.New(rand.NewSource(7))

	for i, length := range lengths {
		content := randomStressContent(rng, length, i%5 == 0)

		tools := ToolSet{"content": {
			Name:       "content",
			Parameters: map[string]interface{}{"type": "object"},
			Execute: func(ctx context.Context, input map[string]interface{}, opts types.ToolExecutionOptions) (interface{}, error) {
				return map[string]interface{}{"value": content}, nil
			},
		}}

		var js string
		var want interface{}
		if i%2 == 0 {
			js = "const r = await tools.content({}); return r.value;"
			want = content
		} else {
			js = "const r = await tools.content({}); return { value: r.value, length: r.value.length };"
			want = map[string]interface{}{"value": content, "length": float64(len([]rune(content)))}
		}

		got, err := RunCodeMode(context.Background(), RunInput{JS: js, Tools: tools})
		if err != nil {
			t.Fatalf("iter %d (len %d): unexpected error: %v", i, length, err)
		}
		assertDeepEqual(t, got, want)
	}
}
