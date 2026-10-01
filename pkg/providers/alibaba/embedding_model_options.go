package alibaba

const (
	AlibabaEmbeddingTextV4 = "text-embedding-v4"
	AlibabaEmbeddingTextV3 = "text-embedding-v3"
)

// Embedding output types supported by DashScope text embeddings.
const (
	AlibabaEmbeddingOutputDense       = "dense"
	AlibabaEmbeddingOutputSparse      = "sparse"
	AlibabaEmbeddingOutputDenseSparse = "dense&sparse"
)

// AlibabaEmbeddingModelOptions contains Alibaba-specific embedding options.
type AlibabaEmbeddingModelOptions struct {
	// TextType differentiates query text from document text. Valid values are
	// "query" and "document"; DashScope defaults to "document".
	TextType string `json:"textType,omitempty"`

	// Dimension is the output embedding dimension. DashScope defaults to 1024.
	// text-embedding-v4 also supports 1536 and 2048. A nil value means omitted.
	Dimension *float64 `json:"dimension,omitempty"`

	// OutputType controls dense/sparse output. Valid values are "dense",
	// "sparse", and "dense&sparse"; DashScope defaults to "dense".
	OutputType string `json:"outputType,omitempty"`
}
