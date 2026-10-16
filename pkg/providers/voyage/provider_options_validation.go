package voyage

import "github.com/digitallysavvy/go-ai/pkg/providerutils"

// voyageEmbeddingOptionsSchema mirrors voyageEmbeddingModelOptions (TS
// voyage-embedding-model-options.ts).
var voyageEmbeddingOptionsSchema = providerutils.OptionSchema{
	"inputType":       providerutils.OptNullable(providerutils.OptEnum("query", "document")),
	"truncation":      providerutils.OptBool,
	"outputDimension": providerutils.OptNumber,
	"outputDtype":     providerutils.OptEnum("float", "int8", "uint8", "binary", "ubinary"),
}

func validateEmbeddingProviderOptions(providerOptions map[string]interface{}) error {
	_, err := providerutils.ParseProviderOptions("voyage", providerOptions, voyageEmbeddingOptionsSchema)
	return err
}

// voyageRerankingOptionsSchema mirrors voyageRerankingModelOptionsSchema (TS
// reranking/voyage-reranking-model-options.ts).
var voyageRerankingOptionsSchema = providerutils.OptionSchema{
	"returnDocuments": providerutils.OptBool,
	"truncation":      providerutils.OptBool,
}

func validateRerankProviderOptions(providerOptions map[string]interface{}) error {
	_, err := providerutils.ParseProviderOptions("voyage", providerOptions, voyageRerankingOptionsSchema)
	return err
}
