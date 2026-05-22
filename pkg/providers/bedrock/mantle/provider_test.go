package mantle

import (
	"net/http"
	"testing"
)

func TestCreateBedrockMantleDefaultsAndModels(t *testing.T) {
	p := CreateBedrockMantle(ProviderSettings{
		Region: "us-west-2",
		APIKey: "bearer",
	})
	if p.Name() != "bedrock-mantle" {
		t.Fatalf("Name = %q", p.Name())
	}
	chat, err := p.Chat(ModelOpenAIGPTOSS20B)
	if err != nil {
		t.Fatalf("Chat error = %v", err)
	}
	if chat.ModelID() != ModelOpenAIGPTOSS20B || chat.Provider() != "bedrock-mantle.chat" {
		t.Fatalf("chat metadata = %s/%s", chat.Provider(), chat.ModelID())
	}
	responses, err := p.Responses(ResponsesModelOpenAIGPTOSS120B)
	if err != nil {
		t.Fatalf("Responses error = %v", err)
	}
	if responses.ModelID() != ResponsesModelOpenAIGPTOSS120B {
		t.Fatalf("responses model = %q", responses.ModelID())
	}
}

func TestBedrockMantleBearerTokenSkipsSigV4Transport(t *testing.T) {
	base := roundTripperFunc(func(req *http.Request) (*http.Response, error) { return nil, nil })
	client := &http.Client{Transport: base}
	p := CreateBedrockMantle(ProviderSettings{Region: "us-east-1", APIKey: "bearer", HTTPClient: client})
	if _, ok := p.openai.Client().HTTPClient().Transport.(*sigV4Transport); ok {
		t.Fatal("bearer token auth must not install SigV4 transport")
	}
}

func TestBedrockMantleSigV4FallbackTransport(t *testing.T) {
	p := CreateBedrockMantle(ProviderSettings{
		Region:          "us-east-1",
		AccessKeyID:     "akid",
		SecretAccessKey: "secret",
	})
	if _, ok := p.openai.Client().HTTPClient().Transport.(*sigV4Transport); !ok {
		t.Fatal("expected SigV4 transport when bearer token is absent")
	}
}

type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
