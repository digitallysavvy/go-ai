package middleware

import (
	"context"
	"io"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

func TestExtractReasoningMiddleware_Generate(t *testing.T) {
	tests := []struct {
		name               string
		input              string
		tagName            string
		startWithReasoning bool
		expectedText       string
	}{
		{
			name:         "single reasoning block",
			input:        "Some text <think>reasoning here</think> more text",
			tagName:      "think",
			expectedText: "Some text \n more text",
		},
		{
			name:         "multiple reasoning blocks",
			input:        "<think>reason1</think>text1<think>reason2</think>text2",
			tagName:      "think",
			expectedText: "text1\ntext2",
		},
		{
			name:         "no reasoning blocks",
			input:        "just plain text",
			tagName:      "think",
			expectedText: "just plain text",
		},
		{
			name:         "empty reasoning block",
			input:        "text<think></think>more",
			tagName:      "think",
			expectedText: "text\nmore",
		},
		{
			name:               "start with reasoning",
			input:              "reasoning here</think> text after",
			tagName:            "think",
			startWithReasoning: true,
			expectedText:       " text after",
		},
		{
			name:         "different tag name",
			input:        "<reasoning>thinking</reasoning> result",
			tagName:      "reasoning",
			expectedText: " result",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockModel := &mockLanguageModel{
				generateResult: &types.GenerateResult{
					Text: tt.input,
				},
			}

			middleware := ExtractReasoningMiddleware(&ExtractReasoningOptions{
				TagName:            tt.tagName,
				Separator:          "\n",
				StartWithReasoning: tt.startWithReasoning,
			})

			wrapped := WrapLanguageModel(mockModel, []*LanguageModelMiddleware{middleware}, nil, nil)

			result, err := wrapped.DoGenerate(context.Background(), &provider.GenerateOptions{})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			if result.Text != tt.expectedText {
				t.Errorf("expected %q, got %q", tt.expectedText, result.Text)
			}
		})
	}
}

func TestExtractReasoningMiddleware_Stream(t *testing.T) {
	tests := []struct {
		name              string
		chunks            []*provider.StreamChunk
		tagName           string
		expectedReasoning []string
		expectedText      []string
	}{
		{
			name: "simple reasoning block",
			chunks: []*provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: "<think>"},
				{Type: provider.ChunkTypeText, Text: "reasoning"},
				{Type: provider.ChunkTypeText, Text: "</think>"},
				{Type: provider.ChunkTypeText, Text: "text"},
			},
			tagName:           "think",
			expectedReasoning: []string{"reasoning"},
			expectedText:      []string{"text"},
		},
		{
			name: "text then reasoning",
			chunks: []*provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: "some text "},
				{Type: provider.ChunkTypeText, Text: "<think>"},
				{Type: provider.ChunkTypeText, Text: "thinking"},
				{Type: provider.ChunkTypeText, Text: "</think>"},
			},
			tagName:           "think",
			expectedReasoning: []string{"thinking"},
			expectedText:      []string{"some text "},
		},
		{
			name: "multiple switches",
			chunks: []*provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: "text1"},
				{Type: provider.ChunkTypeText, Text: "<think>"},
				{Type: provider.ChunkTypeText, Text: "reason1"},
				{Type: provider.ChunkTypeText, Text: "</think>"},
				{Type: provider.ChunkTypeText, Text: "text2"},
				{Type: provider.ChunkTypeText, Text: "<think>"},
				{Type: provider.ChunkTypeText, Text: "reason2"},
				{Type: provider.ChunkTypeText, Text: "</think>"},
			},
			tagName: "think",
			// TS extractReasoningMiddleware inserts the separator between
			// sibling blocks of the same kind once the first one has
			// published (matches the non-streaming path, which joins
			// "text1"+"\n"+"text2" and "reason1"+"\n"+"reason2").
			expectedReasoning: []string{"reason1", "\nreason2"},
			expectedText:      []string{"text1", "\ntext2"},
		},
		{
			name: "partial tag buffering",
			chunks: []*provider.StreamChunk{
				{Type: provider.ChunkTypeText, Text: "<th"},
				{Type: provider.ChunkTypeText, Text: "ink>"},
				{Type: provider.ChunkTypeText, Text: "reasoning"},
				{Type: provider.ChunkTypeText, Text: "</th"},
				{Type: provider.ChunkTypeText, Text: "ink>"},
				{Type: provider.ChunkTypeText, Text: "text"},
			},
			tagName:           "think",
			expectedReasoning: []string{"reasoning"},
			expectedText:      []string{"text"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockStream := &mockTextStream{chunks: tt.chunks}
			mockModel := &mockLanguageModel{stream: mockStream}

			middleware := ExtractReasoningMiddleware(&ExtractReasoningOptions{
				TagName:   tt.tagName,
				Separator: "\n",
			})

			wrapped := WrapLanguageModel(mockModel, []*LanguageModelMiddleware{middleware}, nil, nil)

			stream, err := wrapped.DoStream(context.Background(), &provider.GenerateOptions{})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			var reasoningChunks []string
			var textChunks []string

			for {
				chunk, err := stream.Next()
				if err == io.EOF {
					break
				}
				if err != nil {
					t.Fatalf("unexpected error during streaming: %v", err)
				}

				switch chunk.Type {
				case provider.ChunkTypeReasoning:
					reasoningChunks = append(reasoningChunks, chunk.Reasoning)
				case provider.ChunkTypeText:
					textChunks = append(textChunks, chunk.Text)
				}
			}

			// Compare reasoning chunks
			if len(reasoningChunks) != len(tt.expectedReasoning) {
				t.Errorf("reasoning: expected %d chunks, got %d", len(tt.expectedReasoning), len(reasoningChunks))
			} else {
				for i, expected := range tt.expectedReasoning {
					if reasoningChunks[i] != expected {
						t.Errorf("reasoning chunk %d: expected %q, got %q", i, expected, reasoningChunks[i])
					}
				}
			}

			// Compare text chunks
			if len(textChunks) != len(tt.expectedText) {
				t.Errorf("text: expected %d chunks, got %d", len(tt.expectedText), len(textChunks))
			} else {
				for i, expected := range tt.expectedText {
					if textChunks[i] != expected {
						t.Errorf("text chunk %d: expected %q, got %q", i, expected, textChunks[i])
					}
				}
			}
		})
	}
}

func TestGetPotentialStartIndex(t *testing.T) {
	tests := []struct {
		name         string
		text         string
		searchedText string
		expected     int
	}{
		{
			name:         "complete match",
			text:         "hello world",
			searchedText: "world",
			expected:     6,
		},
		{
			name:         "partial match at end",
			text:         "hello wo",
			searchedText: "world",
			expected:     6,
		},
		{
			name:         "no match",
			text:         "hello",
			searchedText: "world",
			expected:     -1,
		},
		{
			name:         "empty search",
			text:         "hello",
			searchedText: "",
			expected:     -1,
		},
		{
			name:         "match at beginning",
			text:         "world",
			searchedText: "world",
			expected:     0,
		},
		{
			name:         "single char partial",
			text:         "hello w",
			searchedText: "world",
			expected:     6,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := getPotentialStartIndex(tt.text, tt.searchedText)
			if result != tt.expected {
				t.Errorf("expected %d, got %d", tt.expected, result)
			}
		})
	}
}

// TestExtractReasoningMiddleware_Stream_OverlappingTextBlocks ports the TS
// "should preserve overlapping text parts" case (audit row 2b105fa / WG12):
// two interleaved text blocks with distinct IDs must not share extraction
// state, and their deltas must reassemble in full without dropped parts.
func TestExtractReasoningMiddleware_Stream_OverlappingTextBlocks(t *testing.T) {
	chunks := []*provider.StreamChunk{
		{Type: provider.ChunkTypeTextStart, ID: "a"},
		{Type: provider.ChunkTypeTextStart, ID: "b"},
		{Type: provider.ChunkTypeText, ID: "a", Text: "Alpha."},
		{Type: provider.ChunkTypeText, ID: "b", Text: "Beta."},
		{Type: provider.ChunkTypeTextEnd, ID: "a"},
		{Type: provider.ChunkTypeTextEnd, ID: "b"},
	}
	mockStream := &mockTextStream{chunks: chunks}
	mockModel := &mockLanguageModel{stream: mockStream}

	middleware := ExtractReasoningMiddleware(&ExtractReasoningOptions{TagName: "think", Separator: "\n"})
	wrapped := WrapLanguageModel(mockModel, []*LanguageModelMiddleware{middleware}, nil, nil)

	stream, err := wrapped.DoStream(context.Background(), &provider.GenerateOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var textByID = map[string]string{}
	var order []string
	seenStart := map[string]bool{}
	seenEnd := map[string]bool{}
	for {
		chunk, err := stream.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("unexpected error during streaming: %v", err)
		}
		switch chunk.Type {
		case provider.ChunkTypeTextStart:
			seenStart[chunk.ID] = true
			order = append(order, "start:"+chunk.ID)
		case provider.ChunkTypeTextEnd:
			seenEnd[chunk.ID] = true
			order = append(order, "end:"+chunk.ID)
		case provider.ChunkTypeText:
			textByID[chunk.ID] += chunk.Text
			order = append(order, "delta:"+chunk.ID)
		}
	}

	if textByID["a"] != "Alpha." {
		t.Errorf("text[a] = %q, want %q", textByID["a"], "Alpha.")
	}
	if textByID["b"] != "Beta." {
		t.Errorf("text[b] = %q, want %q", textByID["b"], "Beta.")
	}
	if !seenStart["a"] || !seenStart["b"] {
		t.Errorf("missing text-start chunk(s): %v", order)
	}
	if !seenEnd["a"] || !seenEnd["b"] {
		t.Errorf("missing text-end chunk(s): %v", order)
	}
}

// TestExtractReasoningMiddleware_Stream_ReasoningBoundaryIDs verifies that
// each reasoning block gets its own stream-wide "reasoning-N" ID with
// matching start/end chunks (audit row 2b105fa / WG12).
func TestExtractReasoningMiddleware_Stream_ReasoningBoundaryIDs(t *testing.T) {
	chunks := []*provider.StreamChunk{
		{Type: provider.ChunkTypeText, Text: "text1<think>reason1</think>text2<think>reason2</think>"},
	}
	mockStream := &mockTextStream{chunks: chunks}
	mockModel := &mockLanguageModel{stream: mockStream}

	middleware := ExtractReasoningMiddleware(&ExtractReasoningOptions{TagName: "think", Separator: "\n"})
	wrapped := WrapLanguageModel(mockModel, []*LanguageModelMiddleware{middleware}, nil, nil)

	stream, err := wrapped.DoStream(context.Background(), &provider.GenerateOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var startIDs, endIDs []string
	for {
		chunk, err := stream.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("unexpected error during streaming: %v", err)
		}
		switch chunk.Type {
		case provider.ChunkTypeReasoningStart:
			startIDs = append(startIDs, chunk.ID)
		case provider.ChunkTypeReasoningEnd:
			endIDs = append(endIDs, chunk.ID)
		}
	}

	wantStart := []string{"reasoning-0", "reasoning-1"}
	wantEnd := []string{"reasoning-0", "reasoning-1"}
	if len(startIDs) != len(wantStart) || startIDs[0] != wantStart[0] || startIDs[1] != wantStart[1] {
		t.Errorf("reasoning-start IDs = %v, want %v", startIDs, wantStart)
	}
	if len(endIDs) != len(wantEnd) || endIDs[0] != wantEnd[0] || endIDs[1] != wantEnd[1] {
		t.Errorf("reasoning-end IDs = %v, want %v", endIDs, wantEnd)
	}
}

// TestExtractReasoningMiddleware_Stream_DelayedTextStart verifies that a
// text-start chunk is withheld until after any leading reasoning-start,
// matching TS's fix for https://github.com/vercel/ai/issues/7774.
func TestExtractReasoningMiddleware_Stream_DelayedTextStart(t *testing.T) {
	chunks := []*provider.StreamChunk{
		{Type: provider.ChunkTypeTextStart, ID: "1"},
		{Type: provider.ChunkTypeText, ID: "1", Text: "<think>"},
		{Type: provider.ChunkTypeText, ID: "1", Text: "reasoning"},
		{Type: provider.ChunkTypeText, ID: "1", Text: "</think>"},
		{Type: provider.ChunkTypeText, ID: "1", Text: "answer"},
		{Type: provider.ChunkTypeTextEnd, ID: "1"},
	}
	mockStream := &mockTextStream{chunks: chunks}
	mockModel := &mockLanguageModel{stream: mockStream}

	middleware := ExtractReasoningMiddleware(&ExtractReasoningOptions{TagName: "think", Separator: "\n"})
	wrapped := WrapLanguageModel(mockModel, []*LanguageModelMiddleware{middleware}, nil, nil)

	stream, err := wrapped.DoStream(context.Background(), &provider.GenerateOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var order []string
	for {
		chunk, err := stream.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("unexpected error during streaming: %v", err)
		}
		order = append(order, string(chunk.Type))
	}

	textStartIdx, reasoningStartIdx := -1, -1
	for i, ty := range order {
		if ty == string(provider.ChunkTypeTextStart) && textStartIdx == -1 {
			textStartIdx = i
		}
		if ty == string(provider.ChunkTypeReasoningStart) && reasoningStartIdx == -1 {
			reasoningStartIdx = i
		}
	}
	if reasoningStartIdx == -1 || textStartIdx == -1 {
		t.Fatalf("missing reasoning-start or text-start in %v", order)
	}
	if textStartIdx < reasoningStartIdx {
		t.Errorf("text-start (index %d) emitted before reasoning-start (index %d): %v", textStartIdx, reasoningStartIdx, order)
	}
}

// TestExtractReasoningMiddleware_Stream_PreservesPartialTagAtTextEnd ports
// extract-reasoning-middleware.test.ts "should preserve a partial tag in
// $location when the text part ends" (TS #21726): a text block that ends
// while the buffer holds an unresolved partial opening or closing tag must
// still flush that buffer (as text or reasoning) when text-end arrives,
// instead of silently dropping it.
func TestExtractReasoningMiddleware_Stream_PreservesPartialTagAtTextEnd(t *testing.T) {
	tests := []struct {
		name              string
		delta             string
		expectedText      string
		expectedReasoning string
	}{
		{
			name:         "partial opening tag in text",
			delta:        "Use the <th",
			expectedText: "Use the <th",
		},
		{
			name:              "partial closing tag in reasoning",
			delta:             "<think>a </th",
			expectedReasoning: "a </th",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			chunks := []*provider.StreamChunk{
				{Type: provider.ChunkTypeTextStart, ID: "1"},
				{Type: provider.ChunkTypeText, ID: "1", Text: tt.delta},
				{Type: provider.ChunkTypeTextEnd, ID: "1"},
			}
			mockStream := &mockTextStream{chunks: chunks}
			mockModel := &mockLanguageModel{stream: mockStream}

			middleware := ExtractReasoningMiddleware(&ExtractReasoningOptions{TagName: "think", Separator: "\n"})
			wrapped := WrapLanguageModel(mockModel, []*LanguageModelMiddleware{middleware}, nil, nil)

			stream, err := wrapped.DoStream(context.Background(), &provider.GenerateOptions{})
			if err != nil {
				t.Fatalf("unexpected error: %v", err)
			}

			var text, reasoning string
			for {
				chunk, err := stream.Next()
				if err == io.EOF {
					break
				}
				if err != nil {
					t.Fatalf("unexpected error during streaming: %v", err)
				}
				switch chunk.Type {
				case provider.ChunkTypeText:
					text += chunk.Text
				case provider.ChunkTypeReasoning:
					reasoning += chunk.Reasoning
				}
			}

			if text != tt.expectedText {
				t.Errorf("text = %q, want %q", text, tt.expectedText)
			}
			if reasoning != tt.expectedReasoning {
				t.Errorf("reasoning = %q, want %q", reasoning, tt.expectedReasoning)
			}
		})
	}
}

func TestExtractReasoningMiddleware_NilOptions(t *testing.T) {
	mockModel := &mockLanguageModel{
		generateResult: &types.GenerateResult{
			Text: "<think>reasoning</think>text",
		},
	}

	// Test with nil options - should use defaults
	middleware := ExtractReasoningMiddleware(nil)
	wrapped := WrapLanguageModel(mockModel, []*LanguageModelMiddleware{middleware}, nil, nil)

	result, err := wrapped.DoGenerate(context.Background(), &provider.GenerateOptions{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	expected := "text"
	if result.Text != expected {
		t.Errorf("expected %q, got %q", expected, result.Text)
	}
}

func TestExtractReasoningMiddleware_CustomDelimiters(t *testing.T) {
	gemma := &ReasoningDelimiters{Opening: "<|channel>thought\n", Closing: "<channel|>"}
	meta := &ReasoningDelimiters{Opening: "[.*+?^${}()|\\]", Closing: "(end.*+?^${}|\\)"}
	cases := []struct {
		name, input, text, reasoning string
		d                            *ReasoningDelimiters
		startWithReasoning           bool
		separator                    string
	}{
		{"gemma", "<|channel>thought\nChecking the sum.\nIt is four.<channel|>4", "4", "Checking the sum.\nIt is four.", gemma, false, ""},
		{"empty gemma", "<|channel>thought\n<channel|>4", "4", "", gemma, false, ""},
		{"omitted opening", "Checking the sum.<channel|>4", "4", "Checking the sum.", gemma, true, ""},
		{"metachars", "[.*+?^${}()|\\]Checking the sum.(end.*+?^${}|\\)4", "4", "Checking the sum.", meta, false, ""},
		{"multiple blocks and separator", "Before<|channel>thought\nFirst<channel|>Between<|channel>thought\nSecond<channel|>After", "Before / Between / After", "First / Second", gemma, false, " / "},
		{"no delimiters", "4", "4", "", gemma, false, ""},
	}
	for _, tc := range cases {
		opts := &ExtractReasoningOptions{Delimiters: tc.d, StartWithReasoning: tc.startWithReasoning, Separator: tc.separator}
		t.Run("generate/"+tc.name, func(t *testing.T) {
			mw := ExtractReasoningMiddleware(opts)
			wrapped := WrapLanguageModel(&mockLanguageModel{generateResult: &types.GenerateResult{Text: tc.input}}, []*LanguageModelMiddleware{mw}, nil, nil)
			res, err := wrapped.DoGenerate(context.Background(), &provider.GenerateOptions{})
			if err != nil {
				t.Fatal(err)
			}
			if res.Text != tc.text {
				t.Errorf("text: want %q got %q", tc.text, res.Text)
			}
		})
		t.Run("stream/"+tc.name, func(t *testing.T) {
			var chunks []*provider.StreamChunk
			for _, r := range tc.input {
				chunks = append(chunks, &provider.StreamChunk{Type: provider.ChunkTypeText, ID: "text-0", Text: string(r)})
			}
			mw := ExtractReasoningMiddleware(opts)
			wrapped := WrapLanguageModel(&mockLanguageModel{stream: &mockTextStream{chunks: chunks}}, []*LanguageModelMiddleware{mw}, nil, nil)
			stream, err := wrapped.DoStream(context.Background(), &provider.GenerateOptions{})
			if err != nil {
				t.Fatal(err)
			}
			var text, reasoning string
			for {
				c, err := stream.Next()
				if err != nil {
					break
				}
				switch c.Type {
				case provider.ChunkTypeText:
					text += c.Text
				case provider.ChunkTypeReasoning:
					reasoning += c.Reasoning
				}
			}
			if text != tc.text || reasoning != tc.reasoning {
				t.Errorf("want text %q reasoning %q, got %q / %q", tc.text, tc.reasoning, text, reasoning)
			}
		})
	}
}

func TestExtractReasoningMiddleware_RejectsEmptyDelimiters(t *testing.T) {
	for _, d := range []*ReasoningDelimiters{{Opening: "", Closing: "<channel|>"}, {Opening: "<|channel>thought\n", Closing: ""}} {
		func() {
			defer func() {
				e, ok := recover().(*providererrors.InvalidArgumentError)
				if !ok || e.Message != "Reasoning delimiters must not be empty." {
					t.Errorf("expected InvalidArgumentError panic, got %v", e)
				}
			}()
			ExtractReasoningMiddleware(&ExtractReasoningOptions{Delimiters: d})
		}()
	}
}
