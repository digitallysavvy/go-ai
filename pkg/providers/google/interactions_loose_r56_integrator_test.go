package google

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
)

// r56 integrator: the loose decode keeps the valid items of a slice whose
// other item fails its shape, and runs the type hooks (usage raw, block name
// presence) like the strict decode does.
func TestR56InteractionsLooseDecodeSlicesAndHooks(t *testing.T) {
	t.Run("slice item mismatch keeps the other items", func(t *testing.T) {
		chunks := runInteractionsFrames(t,
			`{"event_type":"step.start","index":0,"step":{"type":"model_output"}}`,
			`{"event_type":"step.delta","index":0,"delta":{"type":"text","text":"hi"}}`,
			`{"event_type":"step.delta","index":0,"delta":{"type":"text_annotation","annotations":[`+
				`{"type":"url_citation","url":"https://a.example","start_index":"x"},`+
				`{"type":"url_citation","url":"https://b.example"}]}}`,
			`{"event_type":"step.stop","index":0}`,
			looseCompleted)
		var urls []string
		for _, c := range chunks {
			if c.Type == provider.ChunkTypeSource && c.SourceContent != nil {
				urls = append(urls, c.SourceContent.URL)
			}
		}
		if len(urls) != 2 || urls[0] != "https://a.example" || urls[1] != "https://b.example" {
			t.Fatalf("source urls = %q, want both annotations", urls)
		}
	})

	t.Run("usage raw kept", func(t *testing.T) {
		chunks := runInteractionsFrames(t,
			`{"event_type":"interaction.completed","interaction":{"id":"v1","status":"completed","usage":{"total_input_tokens":3,"total_output_tokens":"x"}}}`)
		var finish *provider.StreamChunk
		for _, c := range chunks {
			if c.Type == provider.ChunkTypeFinish {
				finish = c
			}
		}
		if finish == nil || finish.Usage == nil || finish.Usage.Raw == nil || finish.Usage.Raw["total_output_tokens"] != "x" {
			t.Fatalf("finish usage = %#v, want the wire usage object as raw", finish)
		}
	})

	t.Run("block name presence kept", func(t *testing.T) {
		chunks := runInteractionsFrames(t,
			`{"event_type":"step.start","index":0,"step":{"type":"function_call","id":"c1","name":"","signature":5}}`,
			`{"event_type":"step.stop","index":0}`,
			looseCompleted)
		var found bool
		for _, c := range chunks {
			if c.Type == provider.ChunkTypeToolCall && c.ToolCall.ID == "c1" {
				found = true
				if c.ToolCall.ToolName != "" {
					t.Fatalf("tool name = %q, want the empty wire name (not the fallback)", c.ToolCall.ToolName)
				}
			}
		}
		if !found {
			t.Fatal("no tool call c1")
		}
	})
}
