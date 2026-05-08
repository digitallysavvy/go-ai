package providers_test

import (
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/providers/alibaba"
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
	"github.com/digitallysavvy/go-ai/pkg/providers/openai"
	"github.com/digitallysavvy/go-ai/pkg/providers/perplexity"
	"github.com/digitallysavvy/go-ai/pkg/providers/together"
	"github.com/digitallysavvy/go-ai/pkg/providers/xai"
)

func TestProviderFactoryAliasesMirrorTypeScriptCreateExports(t *testing.T) {
	if anthropic.CreateAnthropic(anthropic.Config{}) == nil {
		t.Fatal("CreateAnthropic returned nil")
	}
	if openai.CreateOpenAI(openai.Config{}) == nil {
		t.Fatal("CreateOpenAI returned nil")
	}
	if google.CreateGoogle(google.Config{}) == nil {
		t.Fatal("CreateGoogle returned nil")
	}
	if google.CreateGoogleGenerativeAI(google.Config{}) == nil {
		t.Fatal("CreateGoogleGenerativeAI returned nil")
	}
	if bedrock.CreateAmazonBedrock(bedrock.Config{Region: "us-east-1"}) == nil {
		t.Fatal("CreateAmazonBedrock returned nil")
	}
	if xai.CreateXai(xai.Config{}) == nil {
		t.Fatal("CreateXai returned nil")
	}
	if mistral.CreateMistral(mistral.Config{}) == nil {
		t.Fatal("CreateMistral returned nil")
	}
	if groq.CreateGroq(groq.Config{}) == nil {
		t.Fatal("CreateGroq returned nil")
	}
	if cohere.CreateCohere(cohere.Config{}) == nil {
		t.Fatal("CreateCohere returned nil")
	}
	if fireworks.CreateFireworks(fireworks.Config{}) == nil {
		t.Fatal("CreateFireworks returned nil")
	}
	if together.CreateTogetherAI(together.Config{}) == nil {
		t.Fatal("CreateTogetherAI returned nil")
	}
	if deepseek.CreateDeepSeek(deepseek.Config{}) == nil {
		t.Fatal("CreateDeepSeek returned nil")
	}
	if perplexity.CreatePerplexity(perplexity.Config{}) == nil {
		t.Fatal("CreatePerplexity returned nil")
	}
	if azure.CreateAzure(azure.Config{}) == nil {
		t.Fatal("CreateAzure returned nil")
	}
	if alibaba.CreateAlibaba(alibaba.Config{}) == nil {
		t.Fatal("CreateAlibaba returned nil")
	}

	vertexCfg := googlevertex.Config{
		Project:     "test-project",
		Location:    "us-central1",
		AccessToken: "test-token",
	}
	if p, err := googlevertex.CreateGoogleVertex(vertexCfg); err != nil || p == nil {
		t.Fatalf("CreateGoogleVertex returned provider=%v err=%v", p, err)
	}
	if p, err := googlevertex.CreateVertex(vertexCfg); err != nil || p == nil {
		t.Fatalf("CreateVertex returned provider=%v err=%v", p, err)
	}
	if p, err := gateway.CreateGateway(gateway.Config{APIKey: "test-key"}); err != nil || p == nil {
		t.Fatalf("CreateGateway returned provider=%v err=%v", p, err)
	}
	if p, err := gateway.CreateGatewayProvider(gateway.Config{APIKey: "test-key"}); err != nil || p == nil {
		t.Fatalf("CreateGatewayProvider returned provider=%v err=%v", p, err)
	}
}
