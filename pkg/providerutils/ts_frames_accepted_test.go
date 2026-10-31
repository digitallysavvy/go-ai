package providerutils

import (
	"compress/gzip"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type tsFrameCase struct {
	Test     string `json:"test"`
	Response *struct {
		Kind   string        `json:"kind"`
		Chunks []interface{} `json:"chunks"`
		Raw    string        `json:"raw"`
	} `json:"response"`
}

// tsDataFrames returns the JSON payloads of the data: lines (or JSON events)
// of a TS stream test response.
func tsDataFrames(c tsFrameCase) []string {
	if c.Response == nil {
		return nil
	}
	var out []string
	add := func(text string) {
		for _, ln := range strings.Split(text, "\n") {
			ln = strings.TrimSpace(ln)
			// Recorded fixtures are bare JSON lines; inline chunks carry data:.
			rest := strings.TrimSpace(strings.TrimPrefix(ln, "data:"))
			if strings.HasPrefix(rest, "{") {
				out = append(out, rest)
			}
		}
	}
	switch c.Response.Kind {
	case "chunks", "fixture-chunks":
		if c.Response.Raw != "" {
			add(c.Response.Raw)
		}
		for _, ch := range c.Response.Chunks {
			switch x := ch.(type) {
			case string:
				add(x)
			default:
				b, _ := json.Marshal(x)
				out = append(out, string(b))
			}
		}
	}
	return out
}

func loadTSFrameCases(t *testing.T, rel string) []tsFrameCase {
	t.Helper()
	f, err := os.Open(filepath.Join("..", "providers", rel))
	if err != nil {
		t.Skipf("case file unavailable: %v", err)
	}
	defer func() { _ = f.Close() }()
	zr, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	b, err := io.ReadAll(zr)
	if err != nil {
		t.Fatal(err)
	}
	var cases []tsFrameCase
	if err := json.Unmarshal(b, &cases); err != nil {
		t.Fatal(err)
	}
	return cases
}

// Every data frame of the real TS stream tests (fixtures and inline chunks)
// that TS parses through a chunk schema must be accepted by the Go port. The
// reject list holds frames the TS tests send on purpose as schema-invalid.
func TestTSFixtureFramesAccepted(t *testing.T) {
	chat := func(s ChatChunkShape) func(string) error {
		return func(d string) error { return ValidateChatChunk([]byte(d), s) }
	}
	unparsable := map[string]bool{"should handle unparsable stream parts": true}
	suites := []struct {
		file     string
		validate func(string) error
		reject   map[string]bool // test names that send invalid frames
	}{
		{"openai/testdata/tscases/chat.json.gz", chat(ChatChunkShape{Kind: ChunkKindOpenAI}), unparsable},
		{"openai/testdata/tscases/completion.json.gz", chat(ChatChunkShape{Kind: ChunkKindOpenAICompletion}), unparsable},
		{"groq/testdata/tscases/chat.json.gz", chat(ChatChunkShape{Kind: ChunkKindGroq}), unparsable},
		{"deepseek/testdata/tscases/chat.json.gz", chat(ChatChunkShape{Kind: ChunkKindDeepSeek}), nil},
		{"moonshot/testdata/tscases/chat.json.gz", chat(ChatChunkShape{Kind: ChunkKindMoonshot}), nil},
		{"mistral/testdata/tscases/chat.json.gz", chat(ChatChunkShape{Kind: ChunkKindMistral}), nil},
		{"alibaba/testdata/tscases/chat.json.gz", chat(ChatChunkShape{Kind: ChunkKindAlibaba}), nil},
		{"testdata/tscases/compat_chat.json.gz", chat(ChatChunkShape{Kind: ChunkKindOpenAICompatible}), unparsable},
		{"cerebras/testdata/tscases/chat.json.gz", chat(ChatChunkShape{Kind: ChunkKindOpenAICompatible, Error: ChunkErrorCerebras}), nil},
		{"openai/testdata/tscases/responses.json.gz", func(d string) error { return ValidateResponsesChunk([]byte(d)) }, nil},
		{"xai/testdata/tscases/responses.json.gz", func(d string) error { return ValidateXAIResponsesChunk([]byte(d)) }, nil},
	}
	for _, s := range suites {
		s := s
		t.Run(s.file, func(t *testing.T) {
			n := 0
			for _, c := range loadTSFrameCases(t, s.file) {
				for _, fr := range tsDataFrames(c) {
					n++
					if err := s.validate(fr); err != nil && !s.reject[c.Test] {
						t.Errorf("%q: frame rejected: %v\n  %.300s", c.Test, err, fr)
					}
				}
			}
			t.Logf("%d frames", n)
		})
	}
}

// Frames from the TS fixtures (src/__fixtures__/*.chunks.txt at b658e8503c) of
// providers whose tests are not in tscases: alibaba, cerebras, moonshotai and
// the perplexity agent stream. The malformed-tool-call-index moonshot fixture
// is left out; TS rejects it too (see TestValidateChatChunkSchemas).
func TestTSFixtureChunkFilesAccepted(t *testing.T) {
	for _, c := range []struct {
		file     string
		validate func(string) error
	}{
		{"ts_frames_alibaba.txt.gz", func(d string) error { return ValidateChatChunk([]byte(d), ChatChunkShape{Kind: ChunkKindAlibaba}) }},
		{"ts_frames_cerebras.txt.gz", func(d string) error {
			return ValidateChatChunk([]byte(d), ChatChunkShape{Error: ChunkErrorCerebras})
		}},
		{"ts_frames_moonshot.txt.gz", func(d string) error { return ValidateChatChunk([]byte(d), ChatChunkShape{Kind: ChunkKindMoonshot}) }},
		{"ts_frames_perplexity.txt.gz", func(d string) error {
			if msg, _ := CheckPerplexityAgentChunk(d); msg != "" {
				return errors.New(msg)
			}
			return nil
		}},
	} {
		f, err := os.Open(filepath.Join("testdata", c.file))
		if err != nil {
			t.Fatal(err)
		}
		zr, err := gzip.NewReader(f)
		if err != nil {
			t.Fatal(err)
		}
		b, _ := io.ReadAll(zr)
		_ = f.Close()
		n := 0
		for _, ln := range strings.Split(string(b), "\n") {
			if ln == "" {
				continue
			}
			n++
			if err := c.validate(ln); err != nil {
				t.Errorf("%s: rejected: %v\n  %.250s", c.file, err, ln)
			}
		}
		if n == 0 {
			t.Errorf("%s: no frames", c.file)
		}
	}
}
