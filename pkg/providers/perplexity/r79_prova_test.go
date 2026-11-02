package perplexity

import (
	"encoding/json"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/internal/tsfixture"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// TS builds ids with String(number): getTextId's String(outputIndex),
// `reasoning-${sequence_number}` and a search result's String(result.id).
// JS prints 1e21 as "1e+21" and -0 as "0".
func TestR79prova_StreamIDsUseJSNumberText(t *testing.T) {
	body := `data: {"type":"response.reasoning.started","sequence_number":1e21,"thought":"t"}` + "\n\n" +
		`data: {"type":"response.reasoning.search_results","results":[{"id":-0,"title":"T","url":"https://a.example"}]}` + "\n\n" +
		`data: {"type":"response.output_text.delta","output_index":1e21,"content_index":-0,"delta":"x"}` + "\n\n"
	s := newPerplexityAgentStream(io.NopCloser(strings.NewReader(body)), nil, false, nil)
	defer func() { _ = s.Close() }()
	var got []string
	for i := 0; i < 100; i++ {
		c, err := s.Next()
		if err != nil {
			break
		}
		switch c.Type {
		case provider.ChunkTypeReasoningStart, provider.ChunkTypeTextStart:
			got = append(got, string(c.Type)+"="+c.ID)
		case provider.ChunkTypeSource:
			got = append(got, "source="+c.SourceContent.ID)
		case provider.ChunkTypeError:
			t.Fatalf("error part: %s", c.Text)
		}
	}
	want := "reasoning-start=reasoning-1e+21,source=0,text-start=1e+21"
	if strings.Join(got, ",") != want {
		t.Fatalf("got %v, want %s", got, want)
	}
}

// TestR79prova_TSCasesConvertInput replays convert-to-perplexity-input.test.ts
// ({input, warnings}).
func TestR79prova_TSCasesConvertInput(t *testing.T) {
	cases := tsfixture.LoadCases(t, "testdata/tscases/convert_input.json.gz")
	tsfixture.RunFn(t, cases, func(args []interface{}) (interface{}, map[string]interface{}, error) {
		msgs, why := tsfixture.BuildPromptMessages(args[0])
		if why != "" {
			return nil, nil, fmt.Errorf("%w: %s", tsfixture.ErrUnsupported, why)
		}
		input, warnings, err := convertToPerplexityInput(msgs)
		if err != nil {
			return nil, nil, err
		}
		return map[string]interface{}{"input": input, "warnings": tsfixture.WarningsToTS(warnings)}, nil, nil
	}, nil)
}

// TestR79prova_TSCasesConvertUsage replays convert-perplexity-usage.test.ts.
func TestR79prova_TSCasesConvertUsage(t *testing.T) {
	cases := tsfixture.LoadCases(t, "testdata/tscases/convert_usage.json.gz")
	tsfixture.RunFn(t, cases, func(args []interface{}) (interface{}, map[string]interface{}, error) {
		raw, err := json.Marshal(args[0])
		if err != nil {
			return nil, nil, err
		}
		var u *perplexityUsage
		if err := json.Unmarshal(raw, &u); err != nil {
			return nil, nil, err
		}
		return tsfixture.UsageToTS(convertPerplexityUsage(u, raw)), nil, nil
	}, nil)
}

// TestR79prova_TSCasesDecisionState replays
// convert-to-perplexity-decision-state.test.ts.
func TestR79prova_TSCasesDecisionState(t *testing.T) {
	cases := tsfixture.LoadCases(t, "testdata/tscases/decision_state.json.gz")
	tsfixture.RunFn(t, cases, func(args []interface{}) (interface{}, map[string]interface{}, error) {
		list, _ := args[0].([]interface{})
		var parts []provider.DecisionStatePart
		for _, e := range list {
			m, _ := e.(map[string]interface{})
			switch m["type"] {
			case "text":
				text, _ := m["text"].(string)
				parts = append(parts, provider.DecisionTextPart(text))
			case "json":
				parts = append(parts, provider.DecisionJSONPart(m["value"]))
			case "file":
				mt, _ := m["mediaType"].(string)
				d, _ := m["data"].(map[string]interface{})
				fd := types.FileData{}
				switch d["type"] {
				case "data":
					fd.Type = types.FileDataTypeData
					fd.DataString, _ = d["data"].(string)
				case "url":
					fd.Type = types.FileDataTypeURL
					fd.URL, _ = d["url"].(string)
				default:
					return nil, nil, fmt.Errorf("%w: file data %v", tsfixture.ErrUnsupported, d["type"])
				}
				parts = append(parts, provider.DecisionFilePart(types.FileContent{MediaType: mt, FileData: fd}))
			default:
				return nil, nil, fmt.Errorf("%w: state part %v", tsfixture.ErrUnsupported, m["type"])
			}
		}
		v, err := convertToPerplexityDecisionState(parts)
		return v, nil, err
	}, map[string]string{
		// the throw assertion calls the function with [...state, image]; the
		// extractor cannot evaluate the spread (no callArgs), so the harness
		// would call it with state. TestConvertDecisionState_Limits covers it.
		"counts native nested images together with SDK file parts": "spread call argument not extractable; hand-ported in TestConvertDecisionState_Limits",
	})
}
