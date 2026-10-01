package providers_test

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/providers/anthropic"
	"github.com/digitallysavvy/go-ai/pkg/providers/azure"
	"github.com/digitallysavvy/go-ai/pkg/providers/bedrock"
	"github.com/digitallysavvy/go-ai/pkg/providers/cohere"
	"github.com/digitallysavvy/go-ai/pkg/providers/deepseek"
	"github.com/digitallysavvy/go-ai/pkg/providers/fireworks"
	"github.com/digitallysavvy/go-ai/pkg/providers/gateway"
	"github.com/digitallysavvy/go-ai/pkg/providers/google"
	"github.com/digitallysavvy/go-ai/pkg/providers/googlevertex"
	"github.com/digitallysavvy/go-ai/pkg/providers/groq"
	"github.com/digitallysavvy/go-ai/pkg/providers/mistral"
	"github.com/digitallysavvy/go-ai/pkg/providers/ollama"
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
	"github.com/digitallysavvy/go-ai/pkg/providers/perplexity"
	"github.com/digitallysavvy/go-ai/pkg/providers/quiverai"
	"github.com/digitallysavvy/go-ai/pkg/providers/together"
	"github.com/digitallysavvy/go-ai/pkg/providers/xai"
)

func TestProviderConfigHeadersAreOptionalForWorkflowSerialization(t *testing.T) {
	tests := []struct {
		name   string
		config any
	}{
		{name: "anthropic", config: anthropic.Config{}},
		{name: "openai", config: openai.Config{}},
		{name: "google", config: google.Config{}},
		{name: "googlevertex", config: googlevertex.Config{}},
		{name: "bedrock", config: bedrock.Config{}},
		{name: "xai", config: xai.Config{}},
		{name: "mistral", config: mistral.Config{}},
		{name: "groq", config: groq.Config{}},
		{name: "cohere", config: cohere.Config{}},
		{name: "fireworks", config: fireworks.Config{}},
		{name: "together", config: together.Config{}},
		{name: "deepseek", config: deepseek.Config{}},
		{name: "perplexity", config: perplexity.Config{}},
		{name: "azure", config: azure.Config{}},
		{name: "ollama", config: ollama.Config{}},
		{name: "gateway", config: gateway.Config{}},
		{name: "quiverai", config: quiverai.Config{}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			field, ok := reflect.TypeOf(tt.config).FieldByName("Headers")
			if !ok {
				t.Fatalf("Config.Headers field missing")
			}
			if field.Type.Kind() != reflect.Map || field.Type.Key().Kind() != reflect.String || field.Type.Elem().Kind() != reflect.String {
				t.Fatalf("Config.Headers type = %v, want map[string]string", field.Type)
			}
			if !strings.Contains(field.Tag.Get("json"), "omitempty") {
				t.Fatalf("Config.Headers json tag = %q, want omitempty", field.Tag.Get("json"))
			}

			data, err := json.Marshal(tt.config)
			if err != nil {
				t.Fatalf("json marshal Config: %v", err)
			}
			if strings.Contains(string(data), "headers") || strings.Contains(string(data), "Headers") {
				t.Fatalf("nil Headers should be omitted from JSON, got %s", data)
			}
		})
	}
}
