package fx

import (
	"encoding/json"
	"fmt"
	"unicode/utf16"

	"github.com/digitallysavvy/go-ai/pkg/harness"
)

// Port of TS fx-translate.ts: libfx text and reasoning deltas and model-end
// hooks to harness stream parts. Offsets count UTF-16 code units, as
// JavaScript string lengths do, so a cursor saved by either host replays
// on the other.

type fxStep struct {
	finishReason harness.FinishReason
	usage        harness.Usage
}

type translator struct {
	turnID   string
	emit     func(harness.StreamPart)
	emitOnce func(key string, part harness.StreamPart)
	steps    map[int]fxStep
	offsets  map[string]int
}

func newTranslator(turnID string, emit func(harness.StreamPart), emitOnce func(string, harness.StreamPart)) *translator {
	return &translator{turnID: turnID, emit: emit, emitOnce: emitOnce, steps: map[int]fxStep{}, offsets: map[string]int{}}
}

func utf16Len(s string) int { return len(utf16.Encode([]rune(s))) }

// utf16From returns s from UTF-16 offset n.
func utf16From(s string, n int) string {
	if n <= 0 {
		return s
	}
	units := utf16.Encode([]rune(s))
	if n >= len(units) {
		return ""
	}
	return string(utf16.Decode(units[n:]))
}

func (tr *translator) outputID(requestIndex int, record *fxModelRecord, kind string) string {
	return fmt.Sprintf("%s:%d:%d:%s", tr.turnID, requestIndex, record.Attempt, kind)
}

// onDelta emits the part of a delta not emitted before (a replayed
// response resends text the client already has).
func (tr *translator) onDelta(kind, delta string, requestIndex int, record *fxModelRecord) {
	id := tr.outputID(requestIndex, record, kind)
	start := tr.offsets[id]
	length := utf16Len(delta)
	tr.offsets[id] = start + length
	if record.EmittedOutput == nil {
		record.EmittedOutput = &fxModelOutput{}
	}
	seen := &record.EmittedOutput.Text
	if kind == "reasoning" {
		seen = &record.EmittedOutput.Reasoning
	}
	skip := *seen - start
	if skip < 0 {
		skip = 0
	}
	unseen := utf16From(delta, skip)
	if unseen == "" {
		return
	}
	if *seen == 0 {
		if kind == "text" {
			tr.emitOnce(id+":start", &harness.TextStartPart{ID: id})
		} else {
			tr.emitOnce(id+":start", &harness.ReasoningStartPart{ID: id})
		}
	}
	*seen = start + length
	if kind == "text" {
		tr.emit(&harness.TextDeltaPart{ID: id, Delta: unseen})
	} else {
		tr.emit(&harness.ReasoningDeltaPart{ID: id, Delta: unseen})
	}
}

// endOutput closes a request's open text and reasoning once.
func (tr *translator) endOutput(requestIndex int, record *fxModelRecord) {
	out := record.EmittedOutput
	if out == nil || (out.Finished != nil && *out.Finished) {
		return
	}
	if out.Text != 0 {
		id := tr.outputID(requestIndex, record, "text")
		tr.emitOnce(id+":end", &harness.TextEndPart{ID: id})
	}
	if out.Reasoning != 0 {
		id := tr.outputID(requestIndex, record, "reasoning")
		tr.emitOnce(id+":end", &harness.ReasoningEndPart{ID: id})
	}
	out.Finished = boolPtr(true)
}

type modelEndData struct {
	FinishReason *string `json:"finishReason"`
	Usage        *struct {
		InputTokens      *int `json:"inputTokens"`
		OutputTokens     *int `json:"outputTokens"`
		CacheReadTokens  *int `json:"cacheReadTokens"`
		CacheWriteTokens *int `json:"cacheWriteTokens"`
		ReasoningTokens  *int `json:"reasoningTokens"`
	} `json:"usage"`
}

// onModelEnd records a request's finish reason and usage.
func (tr *translator) onModelEnd(raw json.RawMessage, requestIndex int) {
	var data modelEndData
	if json.Unmarshal(raw, &data) != nil {
		return
	}
	if data.Usage == nil && (data.FinishReason == nil || *data.FinishReason == "") {
		return
	}
	var usage harness.Usage
	if u := data.Usage; u != nil {
		usage.InputTokens.Total = u.InputTokens
		usage.InputTokens.CacheRead = u.CacheReadTokens
		usage.InputTokens.CacheWrite = u.CacheWriteTokens
		usage.OutputTokens.Total = u.OutputTokens
		usage.OutputTokens.Reasoning = u.ReasoningTokens
	}
	if t := usage.InputTokens.Total; t != nil {
		n := *t - deref(usage.InputTokens.CacheRead) - deref(usage.InputTokens.CacheWrite)
		usage.InputTokens.NoCache = &n
	}
	if t := usage.OutputTokens.Total; t != nil {
		n := *t - deref(usage.OutputTokens.Reasoning)
		usage.OutputTokens.Text = &n
	}
	reason := harness.FinishReason{Unified: harness.FinishReasonOther}
	if data.FinishReason != nil && *data.FinishReason != "" {
		reason = harness.FinishReason{Unified: *data.FinishReason, Raw: *data.FinishReason}
	}
	tr.steps[requestIndex] = fxStep{finishReason: reason, usage: usage}
}

func deref(p *int) int {
	if p == nil {
		return 0
	}
	return *p
}

func addUsage(dst **int, v *int) {
	if v == nil {
		return
	}
	n := *v
	if *dst != nil {
		n += **dst
	}
	*dst = &n
}

// totalUsage sums every recorded step.
func (tr *translator) totalUsage() harness.Usage {
	var total harness.Usage
	for _, s := range tr.steps {
		addUsage(&total.InputTokens.Total, s.usage.InputTokens.Total)
		addUsage(&total.InputTokens.NoCache, s.usage.InputTokens.NoCache)
		addUsage(&total.InputTokens.CacheRead, s.usage.InputTokens.CacheRead)
		addUsage(&total.InputTokens.CacheWrite, s.usage.InputTokens.CacheWrite)
		addUsage(&total.OutputTokens.Total, s.usage.OutputTokens.Total)
		addUsage(&total.OutputTokens.Text, s.usage.OutputTokens.Text)
		addUsage(&total.OutputTokens.Reasoning, s.usage.OutputTokens.Reasoning)
	}
	return total
}
