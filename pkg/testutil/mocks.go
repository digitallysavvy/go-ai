// Package testutil provides mock implementations for testing the Go AI SDK.
package testutil

import (
	"context"
	"io"
	"sync"

	"github.com/digitallysavvy/go-ai/pkg/provider"
	"github.com/digitallysavvy/go-ai/pkg/provider/types"
)

// MockLanguageModel is a mock implementation of provider.LanguageModel for testing.
type MockLanguageModel struct {
	DoGenerateFunc    func(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error)
	DoStreamFunc      func(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error)
	ProviderName      string
	ModelName         string
	ToolSupport       bool
	StructuredSupport bool
	ImageSupport      bool

	// Call tracking
	mu              sync.Mutex
	GenerateCalls   []*provider.GenerateOptions
	StreamCalls     []*provider.GenerateOptions
}

func (m *MockLanguageModel) SpecificationVersion() string { return "v3" }
func (m *MockLanguageModel) Provider() string {
	if m.ProviderName == "" {
		return "mock"
	}
	return m.ProviderName
}
func (m *MockLanguageModel) ModelID() string {
	if m.ModelName == "" {
		return "mock-model"
	}
	return m.ModelName
}
func (m *MockLanguageModel) SupportsTools() bool            { return m.ToolSupport }
func (m *MockLanguageModel) SupportsStructuredOutput() bool { return m.StructuredSupport }
func (m *MockLanguageModel) SupportsImageInput() bool       { return m.ImageSupport }

func (m *MockLanguageModel) DoGenerate(ctx context.Context, opts *provider.GenerateOptions) (*types.GenerateResult, error) {
	m.mu.Lock()
	m.GenerateCalls = append(m.GenerateCalls, opts)
	m.mu.Unlock()

	if m.DoGenerateFunc != nil {
		return m.DoGenerateFunc(ctx, opts)
	}
	inputTokens := int64(10)
	outputTokens := int64(5)
	totalTokens := int64(15)
	return &types.GenerateResult{
		Text:         "mock response",
		FinishReason: types.FinishReasonStop,
		Usage:        types.Usage{InputTokens: &inputTokens, OutputTokens: &outputTokens, TotalTokens: &totalTokens},
	}, nil
}

func (m *MockLanguageModel) DoStream(ctx context.Context, opts *provider.GenerateOptions) (provider.TextStream, error) {
	m.mu.Lock()
	m.StreamCalls = append(m.StreamCalls, opts)
	m.mu.Unlock()

	if m.DoStreamFunc != nil {
		return m.DoStreamFunc(ctx, opts)
	}
	return NewMockTextStream([]provider.StreamChunk{
		{Type: provider.ChunkTypeText, Text: "mock "},
		{Type: provider.ChunkTypeText, Text: "response"},
		{Type: provider.ChunkTypeFinish, FinishReason: types.FinishReasonStop},
	}), nil
}

// MockEmbeddingModel is a mock implementation of provider.EmbeddingModel for testing.
type MockEmbeddingModel struct {
	DoEmbedFunc     func(ctx context.Context, input string) (*types.EmbeddingResult, error)
	DoEmbedManyFunc func(ctx context.Context, inputs []string) (*types.EmbeddingsResult, error)
	ProviderName    string
	ModelName       string
	MaxBatchSize    int
	MaxEmbeddings   int // Alias for MaxBatchSize for clarity
	ParallelSupport bool

	// Call tracking
	mu            sync.Mutex
	EmbedCalls    []string
	EmbedManyCalls [][]string
}

func (m *MockEmbeddingModel) SpecificationVersion() string { return "v3" }
func (m *MockEmbeddingModel) Provider() string {
	if m.ProviderName == "" {
		return "mock"
	}
	return m.ProviderName
}
func (m *MockEmbeddingModel) ModelID() string {
	if m.ModelName == "" {
		return "mock-embedding"
	}
	return m.ModelName
}
func (m *MockEmbeddingModel) MaxEmbeddingsPerCall() int {
	if m.MaxEmbeddings > 0 {
		return m.MaxEmbeddings
	}
	if m.MaxBatchSize > 0 {
		return m.MaxBatchSize
	}
	return 100
}
func (m *MockEmbeddingModel) SupportsParallelCalls() bool { return m.ParallelSupport }

func (m *MockEmbeddingModel) DoEmbed(ctx context.Context, input string) (*types.EmbeddingResult, error) {
	m.mu.Lock()
	m.EmbedCalls = append(m.EmbedCalls, input)
	m.mu.Unlock()

	if m.DoEmbedFunc != nil {
		return m.DoEmbedFunc(ctx, input)
	}
	return &types.EmbeddingResult{
		Embedding: []float64{0.1, 0.2, 0.3, 0.4, 0.5},
		Usage:     types.EmbeddingUsage{InputTokens: 5, TotalTokens: 5},
	}, nil
}

func (m *MockEmbeddingModel) DoEmbedMany(ctx context.Context, inputs []string) (*types.EmbeddingsResult, error) {
	m.mu.Lock()
	m.EmbedManyCalls = append(m.EmbedManyCalls, inputs)
	m.mu.Unlock()

	if m.DoEmbedManyFunc != nil {
		return m.DoEmbedManyFunc(ctx, inputs)
	}
	embeddings := make([][]float64, len(inputs))
	for i := range inputs {
		embeddings[i] = []float64{0.1 * float64(i+1), 0.2 * float64(i+1), 0.3 * float64(i+1)}
	}
	return &types.EmbeddingsResult{
		Embeddings: embeddings,
		Usage:      types.EmbeddingUsage{InputTokens: len(inputs) * 5, TotalTokens: len(inputs) * 5},
	}, nil
}

// MockRerankingModel is a mock implementation of provider.RerankingModel for testing.
type MockRerankingModel struct {
	DoRerankFunc func(ctx context.Context, opts *provider.RerankOptions) (*types.RerankResult, error)
	ProviderName string
	ModelName    string

	// Call tracking
	mu          sync.Mutex
	RerankCalls []*provider.RerankOptions
}

func (m *MockRerankingModel) SpecificationVersion() string { return "v3" }
func (m *MockRerankingModel) Provider() string {
	if m.ProviderName == "" {
		return "mock"
	}
	return m.ProviderName
}
func (m *MockRerankingModel) ModelID() string {
	if m.ModelName == "" {
		return "mock-reranking"
	}
	return m.ModelName
}

func (m *MockRerankingModel) DoRerank(ctx context.Context, opts *provider.RerankOptions) (*types.RerankResult, error) {
	m.mu.Lock()
	m.RerankCalls = append(m.RerankCalls, opts)
	m.mu.Unlock()

	if m.DoRerankFunc != nil {
		return m.DoRerankFunc(ctx, opts)
	}

	// Default: return documents in reverse order with decreasing scores
	var docs []interface{}
	switch d := opts.Documents.(type) {
	case []string:
		for _, s := range d {
			docs = append(docs, s)
		}
	case []interface{}:
		docs = d
	}

	ranking := make([]types.RerankItem, len(docs))
	for i := range docs {
		ranking[i] = types.RerankItem{
			Index:          len(docs) - 1 - i,
			RelevanceScore: 1.0 - float64(i)*0.1,
		}
	}

	return &types.RerankResult{
		Ranking: ranking,
		Response: types.RerankResponse{
			ModelID: m.ModelID(),
		},
	}, nil
}

// MockTextStream is a mock implementation of provider.TextStream for testing.
type MockTextStream struct {
	chunks []provider.StreamChunk
	index  int
	err    error
	closed bool
	mu     sync.Mutex
}

// NewMockTextStream creates a new MockTextStream with the given chunks.
func NewMockTextStream(chunks []provider.StreamChunk) *MockTextStream {
	return &MockTextStream{
		chunks: chunks,
		index:  0,
	}
}

// NewMockTextStreamWithError creates a MockTextStream that returns an error.
func NewMockTextStreamWithError(err error) *MockTextStream {
	return &MockTextStream{
		err: err,
	}
}

func (m *MockTextStream) Next() (*provider.StreamChunk, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.err != nil {
		return nil, m.err
	}

	if m.closed {
		return nil, io.EOF
	}

	if m.index >= len(m.chunks) {
		return nil, io.EOF
	}

	chunk := &m.chunks[m.index]
	m.index++
	return chunk, nil
}

func (m *MockTextStream) Read(p []byte) (n int, err error) {
	chunk, err := m.Next()
	if err != nil {
		return 0, err
	}
	if chunk.Type == provider.ChunkTypeText {
		copy(p, chunk.Text)
		return len(chunk.Text), nil
	}
	return 0, nil
}

func (m *MockTextStream) Close() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.closed = true
	return nil
}

func (m *MockTextStream) Err() error {
	return m.err
}

// MockImageModel is a mock implementation of provider.ImageModel for testing.
type MockImageModel struct {
	DoGenerateFunc func(ctx context.Context, opts *provider.ImageGenerateOptions) (*types.ImageResult, error)
	ProviderName   string
	ModelName      string
}

func (m *MockImageModel) SpecificationVersion() string { return "v3" }
func (m *MockImageModel) Provider() string {
	if m.ProviderName == "" {
		return "mock"
	}
	return m.ProviderName
}
func (m *MockImageModel) ModelID() string {
	if m.ModelName == "" {
		return "mock-image"
	}
	return m.ModelName
}

func (m *MockImageModel) DoGenerate(ctx context.Context, opts *provider.ImageGenerateOptions) (*types.ImageResult, error) {
	if m.DoGenerateFunc != nil {
		return m.DoGenerateFunc(ctx, opts)
	}
	return &types.ImageResult{
		Image:    []byte("mock-image-data"),
		MimeType: "image/png",
	}, nil
}

// MockSpeechModel is a mock implementation of provider.SpeechModel for testing.
type MockSpeechModel struct {
	DoGenerateFunc func(ctx context.Context, opts *provider.SpeechGenerateOptions) (*types.SpeechResult, error)
	ProviderName   string
	ModelName      string
}

func (m *MockSpeechModel) SpecificationVersion() string { return "v3" }
func (m *MockSpeechModel) Provider() string {
	if m.ProviderName == "" {
		return "mock"
	}
	return m.ProviderName
}
func (m *MockSpeechModel) ModelID() string {
	if m.ModelName == "" {
		return "mock-speech"
	}
	return m.ModelName
}

func (m *MockSpeechModel) DoGenerate(ctx context.Context, opts *provider.SpeechGenerateOptions) (*types.SpeechResult, error) {
	if m.DoGenerateFunc != nil {
		return m.DoGenerateFunc(ctx, opts)
	}
	return &types.SpeechResult{
		Audio: []byte("mock-audio-data"),
	}, nil
}

// MockTranscriptionModel is a mock implementation of provider.TranscriptionModel for testing.
type MockTranscriptionModel struct {
	DoTranscribeFunc func(ctx context.Context, opts *provider.TranscriptionOptions) (*types.TranscriptionResult, error)
	ProviderName     string
	ModelName        string
}

func (m *MockTranscriptionModel) SpecificationVersion() string { return "v3" }
func (m *MockTranscriptionModel) Provider() string {
	if m.ProviderName == "" {
		return "mock"
	}
	return m.ProviderName
}
func (m *MockTranscriptionModel) ModelID() string {
	if m.ModelName == "" {
		return "mock-transcription"
	}
	return m.ModelName
}

func (m *MockTranscriptionModel) DoTranscribe(ctx context.Context, opts *provider.TranscriptionOptions) (*types.TranscriptionResult, error) {
	if m.DoTranscribeFunc != nil {
		return m.DoTranscribeFunc(ctx, opts)
	}
	return &types.TranscriptionResult{
		Text: "mock transcription",
	}, nil
}

// MockProvider is a mock implementation of provider.Provider for testing.
type MockProvider struct {
	ProviderName           string
	LanguageModelFunc      func(modelID string) (provider.LanguageModel, error)
	EmbeddingModelFunc     func(modelID string) (provider.EmbeddingModel, error)
	ImageModelFunc         func(modelID string) (provider.ImageModel, error)
	SpeechModelFunc        func(modelID string) (provider.SpeechModel, error)
	TranscriptionModelFunc func(modelID string) (provider.TranscriptionModel, error)
	RerankingModelFunc     func(modelID string) (provider.RerankingModel, error)
}

func (m *MockProvider) Name() string {
	if m.ProviderName == "" {
		return "mock"
	}
	return m.ProviderName
}

func (m *MockProvider) LanguageModel(modelID string) (provider.LanguageModel, error) {
	if m.LanguageModelFunc != nil {
		return m.LanguageModelFunc(modelID)
	}
	return &MockLanguageModel{
		ProviderName: m.Name(),
		ModelName:    modelID,
	}, nil
}

func (m *MockProvider) EmbeddingModel(modelID string) (provider.EmbeddingModel, error) {
	if m.EmbeddingModelFunc != nil {
		return m.EmbeddingModelFunc(modelID)
	}
	return &MockEmbeddingModel{
		ProviderName: m.Name(),
		ModelName:    modelID,
	}, nil
}

func (m *MockProvider) ImageModel(modelID string) (provider.ImageModel, error) {
	if m.ImageModelFunc != nil {
		return m.ImageModelFunc(modelID)
	}
	return &MockImageModel{
		ProviderName: m.Name(),
		ModelName:    modelID,
	}, nil
}

func (m *MockProvider) SpeechModel(modelID string) (provider.SpeechModel, error) {
	if m.SpeechModelFunc != nil {
		return m.SpeechModelFunc(modelID)
	}
	return &MockSpeechModel{
		ProviderName: m.Name(),
		ModelName:    modelID,
	}, nil
}

func (m *MockProvider) TranscriptionModel(modelID string) (provider.TranscriptionModel, error) {
	if m.TranscriptionModelFunc != nil {
		return m.TranscriptionModelFunc(modelID)
	}
	return &MockTranscriptionModel{
		ProviderName: m.Name(),
		ModelName:    modelID,
	}, nil
}

func (m *MockProvider) RerankingModel(modelID string) (provider.RerankingModel, error) {
	if m.RerankingModelFunc != nil {
		return m.RerankingModelFunc(modelID)
	}
	return &MockRerankingModel{
		ProviderName: m.Name(),
		ModelName:    modelID,
	}, nil
}
