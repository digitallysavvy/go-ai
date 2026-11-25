package anthropic

import (
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"testing"
)

// portprovaCatalogPath locates the TS Anthropic model catalog, or returns "".
func portprovaCatalogPath() string {
	if root := os.Getenv("GOAI_TS_CHECKOUT"); root != "" {
		return filepath.Join(root, "packages", "anthropic", "src", "anthropic-model-catalog.ts")
	}
	return filepath.Join("..", "..", "..", "..", "..", "ai", "packages", "anthropic", "src", "anthropic-model-catalog.ts")
}

var (
	portprovaRowStart = regexp.MustCompile(`(?m)^  '([^']+)': \{$`)
	portprovaThinking = regexp.MustCompile(`(?s)thinking:\s*(false|\{\s*modes:\s*\[([^\]]*)\],\s*canBeDisabled:\s*(false|'[a-z-]+')\s*,?\s*\})`)
	portprovaEfforts  = regexp.MustCompile(`reasoningEfforts: ([^\n]*)`)
	portprovaField    = func(name string) *regexp.Regexp { return regexp.MustCompile(name + `: ([^,\n]+),`) }
)

// TestPortprovaCatalogDifferential ports TS ad5f2ad965 (per-model-ID
// capability matrix): every listed TS row must produce the same
// getModelCapabilities projection as the Go GetModelCapabilities. Skipped
// when the TS checkout is absent.
func TestPortprovaCatalogDifferential(t *testing.T) {
	raw, err := os.ReadFile(portprovaCatalogPath())
	if err != nil {
		t.Skip("Skipping: TS checkout not available")
	}
	src := string(raw)
	a := strings.Index(src, "const languageModelDefinitions = {")
	b := strings.Index(src, "} satisfies Readonly<Record<string, AnthropicLanguageModelRow>>;")
	if a < 0 || b < a {
		t.Fatal("cannot locate languageModelDefinitions in TS catalog")
	}
	block := src[a:b]
	starts := portprovaRowStart.FindAllStringSubmatchIndex(block, -1)
	if len(starts) < 20 {
		t.Fatalf("parsed only %d rows", len(starts))
	}
	for i, s := range starts {
		id := block[s[2]:s[3]]
		end := len(block)
		if i+1 < len(starts) {
			end = starts[i+1][0]
		}
		row := block[s[1]:end]
		field := func(name string) string {
			m := portprovaField(name).FindStringSubmatch(row)
			if m == nil {
				t.Fatalf("%s: no %s", id, name)
			}
			return strings.TrimSpace(m[1])
		}
		th := portprovaThinking.FindStringSubmatch(row)
		if th == nil {
			t.Fatalf("%s: cannot parse thinking", id)
		}
		maxOut, _ := strconv.Atoi(field("maxOutputTokens"))
		var modes, canBeDisabled string
		if th[1] != "false" {
			modes, canBeDisabled = th[2], th[3]
		}
		efforts := portprovaEfforts.FindStringSubmatch(row)
		want := ModelCapabilities{
			MaxOutputTokens:                        maxOut,
			SupportsStructuredOutput:               field("nativeStructuredOutput") == "true",
			SupportsAdaptiveThinking:               strings.Contains(modes, "'adaptive'"),
			RejectsSamplingParameters:              field("sampling") != "true",
			SupportsXHighEffort:                    efforts != nil && strings.Contains(efforts[1], "'xhigh'"),
			RejectsThinkingDisabledAboveHighEffort: canBeDisabled != "'always'",
			RejectsThinkingDisabled:                canBeDisabled == "false" || th[1] == "false",
			RejectsBudgetThinking:                  !strings.Contains(modes, "'budget'"),
			RejectsForcedToolUse:                   field("forcedToolUse") != "true",
			SupportsBetweenToolsThinking:           strings.Contains(modes, "'between-tools'"),
			IsKnownModel:                           field("knownModel") == "true",
		}
		if got := GetModelCapabilities(id); got != want {
			t.Errorf("%s: GetModelCapabilities = %#v, want %#v", id, got, want)
		}
	}
}

// TestPortprovaFableThinkingDisabledAboveHighEffort pins the three models whose
// snapshot flipped to rejectsThinkingDisabledAboveHighEffort=true in TS
// ad5f2ad965 (claude-fable-5, 5-1, 5-2); independent of the TS checkout.
func TestPortprovaFableThinkingDisabledAboveHighEffort(t *testing.T) {
	for _, id := range []string{"claude-fable-5", "claude-fable-5-1", "claude-fable-5-2"} {
		if !GetModelCapabilities(id).RejectsThinkingDisabledAboveHighEffort {
			t.Errorf("%s: RejectsThinkingDisabledAboveHighEffort = false, want true", id)
		}
	}
}

// TestPortprovaUnknownModelCapabilities is the ruling-13 check for the
// catalog refactor: an unknown ID keeps the conservative legacy row for
// non-Claude IDs and the unknown-Claude row for unlisted Claude IDs.
func TestPortprovaUnknownModelCapabilities(t *testing.T) {
	legacy := GetModelCapabilities("llama-3-70b")
	if legacy != (ModelCapabilities{MaxOutputTokens: 4096}) {
		t.Errorf("non-Claude = %#v", legacy)
	}
	c := GetModelCapabilities("claude-future-9")
	if c.IsKnownModel || c.MaxOutputTokens != 128000 || !c.RejectsThinkingDisabledAboveHighEffort || c.RejectsThinkingDisabled || c.RejectsBudgetThinking {
		t.Errorf("unlisted Claude = %#v", c)
	}
}
