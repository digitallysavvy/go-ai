package registry

import (
	"errors"
	"strings"
	"testing"

	"github.com/digitallysavvy/go-ai/pkg/ai"
	"github.com/digitallysavvy/go-ai/pkg/provider"
	providererrors "github.com/digitallysavvy/go-ai/pkg/provider/errors"
	"github.com/digitallysavvy/go-ai/pkg/testutil"
)

// Go twins of TS packages/ai/src/registry/extraction-model.test.ts (10ff0b1a1d).

type port2genExtractionProvider struct {
	*testutil.MockProvider
	model   provider.ExtractionModel
	err     error
	asked   []string
	nilWant bool
}

func (p *port2genExtractionProvider) ExtractionModel(modelID string) (provider.ExtractionModel, error) {
	p.asked = append(p.asked, modelID)
	if p.err != nil {
		return nil, p.err
	}
	if p.nilWant {
		return nil, nil
	}
	return p.model, nil
}

type port2genExtractionModelV3 struct{ *testutil.MockExtractionModel }

func (port2genExtractionModelV3) SpecificationVersion() string { return "v3" }

func port2genNoSuchModel(t *testing.T, err error, id, modelType string) {
	t.Helper()
	var target *providererrors.NoSuchModelError
	if !errors.As(err, &target) || target.ModelID != id || target.ModelType != modelType {
		t.Fatalf("err = %v (%T), want NoSuchModelError{%s, %s}", err, err, id, modelType)
	}
}

func port2genExtractionOf(t *testing.T, p provider.Provider) provider.ExtractionModelProvider {
	t.Helper()
	ep, ok := p.(provider.ExtractionModelProvider)
	if !ok {
		t.Fatalf("%T does not implement ExtractionModelProvider", p)
	}
	return ep
}

func TestPort2genCustomProviderExtractionResolvesAliasBeforeFallback(t *testing.T) {
	model := &testutil.MockExtractionModel{}
	fallback := &port2genExtractionProvider{MockProvider: &testutil.MockProvider{ProviderName: "fb"}, model: model}
	p := NewCustomProvider(CustomProviderOptions{ExtractionModels: map[string]interface{}{"alias": model}, Fallback: fallback})
	got, err := port2genExtractionOf(t, p).ExtractionModel("alias")
	if err != nil || got != provider.ExtractionModel(model) {
		t.Fatalf("got = %v, err = %v", got, err)
	}
	if len(fallback.asked) != 0 {
		t.Fatalf("fallback consulted: %v", fallback.asked)
	}
}

func TestPort2genCustomProviderExtractionResolvesStringAliasesThroughDefaultProvider(t *testing.T) {
	model := &testutil.MockExtractionModel{}
	def := &port2genExtractionProvider{MockProvider: &testutil.MockProvider{ProviderName: "def"}, model: model}
	ai.SetDefaultProvider(def)
	t.Cleanup(func() { ai.SetDefaultProvider(nil) })
	p := NewCustomProvider(CustomProviderOptions{ExtractionModels: map[string]interface{}{"alias": "model:version"}})
	got, err := port2genExtractionOf(t, p).ExtractionModel("alias")
	if err != nil || got != provider.ExtractionModel(model) {
		t.Fatalf("got = %v, err = %v", got, err)
	}
	if len(def.asked) != 1 || def.asked[0] != "model:version" {
		t.Fatalf("asked = %v", def.asked)
	}
}

func TestPort2genCustomProviderExtractionUsesFallback(t *testing.T) {
	model := &testutil.MockExtractionModel{}
	fallback := &port2genExtractionProvider{MockProvider: &testutil.MockProvider{ProviderName: "fb"}, model: model}
	p := NewCustomProvider(CustomProviderOptions{Fallback: fallback})
	got, err := port2genExtractionOf(t, p).ExtractionModel("model:version")
	if err != nil || got != provider.ExtractionModel(model) {
		t.Fatalf("got = %v, err = %v", got, err)
	}
	if len(fallback.asked) != 1 || fallback.asked[0] != "model:version" {
		t.Fatalf("asked = %v", fallback.asked)
	}
}

func TestPort2genCustomProviderExtractionReportsMissingAlias(t *testing.T) {
	for _, id := range []string{"missing", "toString", "constructor", "__proto__"} {
		p := NewCustomProvider(CustomProviderOptions{ExtractionModels: map[string]interface{}{"known": &testutil.MockExtractionModel{}}})
		_, err := port2genExtractionOf(t, p).ExtractionModel(id)
		port2genNoSuchModel(t, err, id, "extractionModel")
	}
}

func TestPort2genCustomProviderExtractionReportsUnavailableFallbackModel(t *testing.T) {
	fallback := &port2genExtractionProvider{MockProvider: &testutil.MockProvider{ProviderName: "fb"}, nilWant: true}
	p := NewCustomProvider(CustomProviderOptions{Fallback: fallback})
	_, err := port2genExtractionOf(t, p).ExtractionModel("missing")
	port2genNoSuchModel(t, err, "missing", "extractionModel")
	// A fallback without extraction models is the same error.
	p = NewCustomProvider(CustomProviderOptions{Fallback: &testutil.MockProvider{ProviderName: "plain"}})
	_, err = port2genExtractionOf(t, p).ExtractionModel("missing")
	port2genNoSuchModel(t, err, "missing", "extractionModel")
}

func TestPort2genRegistryExtractionPreservesIdentityAndColonsInModelIDs(t *testing.T) {
	model := &testutil.MockExtractionModel{}
	ep := &port2genExtractionProvider{MockProvider: &testutil.MockProvider{ProviderName: "p"}, model: model}
	r := NewRegistry()
	r.RegisterProvider("provider", ep)
	got, err := r.ResolveExtractionModel("provider:model:version")
	if err != nil || got != provider.ExtractionModel(model) {
		t.Fatalf("got = %v, err = %v", got, err)
	}
	if len(ep.asked) != 1 || ep.asked[0] != "model:version" {
		t.Fatalf("asked = %v", ep.asked)
	}
}

func TestPort2genRegistryExtractionCustomSeparator(t *testing.T) {
	model := &testutil.MockExtractionModel{}
	ep := &port2genExtractionProvider{MockProvider: &testutil.MockProvider{ProviderName: "p"}, model: model}
	r := NewRegistry(WithSeparator("::"))
	r.RegisterProvider("provider", ep)
	got, err := r.ResolveExtractionModel("provider::model::version")
	if err != nil || got != provider.ExtractionModel(model) {
		t.Fatalf("got = %v, err = %v", got, err)
	}
	if len(ep.asked) != 1 || ep.asked[0] != "model::version" {
		t.Fatalf("asked = %v", ep.asked)
	}
}

func TestPort2genRegistryExtractionUnknownProvider(t *testing.T) {
	r := NewRegistry()
	r.RegisterProvider("known", &testutil.MockProvider{ProviderName: "known"})
	_, err := r.ResolveExtractionModel("missing:model")
	var target *NoSuchProviderError
	if !errors.As(err, &target) || target.ProviderID != "missing" || target.ModelType != "extractionModel" ||
		len(target.AvailableProviders) != 1 || target.AvailableProviders[0] != "known" {
		t.Fatalf("err = %v (%T)", err, err)
	}
}

func TestPort2genRegistryExtractionMalformedID(t *testing.T) {
	_, err := NewRegistry().ResolveExtractionModel("model")
	if err == nil || !strings.Contains(err.Error(), `must be in the format "providerId:modelId"`) {
		t.Fatalf("err = %v", err)
	}
}

func TestPort2genRegistryExtractionReportsMissingCapabilityOrModel(t *testing.T) {
	for name, p := range map[string]provider.Provider{
		"no capability": &testutil.MockProvider{ProviderName: "plain"},
		"nil model":     &port2genExtractionProvider{MockProvider: &testutil.MockProvider{ProviderName: "p"}, nilWant: true},
	} {
		r := NewRegistry()
		r.RegisterProvider("provider", p)
		_, err := r.ResolveExtractionModel("provider:model")
		_ = name
		port2genNoSuchModel(t, err, "provider:model", "extractionModel")
	}
}

func TestPort2genResolveExtractionModelReturnsInstancesUnchanged(t *testing.T) {
	model := &testutil.MockExtractionModel{}
	got, err := ai.ResolveExtractionModel(model)
	if err != nil || got != provider.ExtractionModel(model) {
		t.Fatalf("got = %v, err = %v", got, err)
	}
}

func TestPort2genResolveExtractionModelRequiresDefaultProviderSupport(t *testing.T) {
	ai.SetDefaultProvider(&testutil.MockProvider{ProviderName: "plain"})
	t.Cleanup(func() { ai.SetDefaultProvider(nil) })
	_, err := ai.ResolveExtractionModel("model")
	if err == nil || !strings.Contains(err.Error(), "The default provider does not support extraction models.") {
		t.Fatalf("err = %v", err)
	}
	port2genNoSuchModel(t, err, "model", "extractionModel")
}

func TestPort2genResolveExtractionModelValidatesVersions(t *testing.T) {
	invalid := port2genExtractionModelV3{&testutil.MockExtractionModel{}}
	p := NewCustomProvider(CustomProviderOptions{ExtractionModels: map[string]interface{}{"invalid": invalid}})
	r := NewRegistry()
	r.RegisterProvider("provider", p)
	ai.SetDefaultProvider(r.providers["provider"])
	t.Cleanup(func() { ai.SetDefaultProvider(nil) })
	for name, resolve := range map[string]func() error{
		"instance": func() error { _, err := ai.ResolveExtractionModel(invalid); return err },
		"alias":    func() error { _, err := port2genExtractionOf(t, p).ExtractionModel("invalid"); return err },
		"registry": func() error { _, err := r.ResolveExtractionModel("provider:invalid"); return err },
		"default":  func() error { _, err := ai.ResolveExtractionModel("invalid"); return err },
	} {
		var target *ai.UnsupportedModelVersionError
		if err := resolve(); !errors.As(err, &target) || target.Version != "v3" {
			t.Fatalf("%s: err = %v", name, err)
		}
	}
}

func TestPort2genResolveExtractionModelPreservesFactoryErrors(t *testing.T) {
	boom := providererrors.NewNoSuchModelError("missing", "extractionModel")
	ai.SetDefaultProvider(&port2genExtractionProvider{MockProvider: &testutil.MockProvider{ProviderName: "p"}, err: boom})
	t.Cleanup(func() { ai.SetDefaultProvider(nil) })
	if _, err := ai.ResolveExtractionModel("missing"); err != boom {
		t.Fatalf("err = %v", err)
	}
}

func TestPort2genResolveExtractionModelReportsMissingModelFromDefaultProvider(t *testing.T) {
	ai.SetDefaultProvider(&port2genExtractionProvider{MockProvider: &testutil.MockProvider{ProviderName: "p"}, nilWant: true})
	t.Cleanup(func() { ai.SetDefaultProvider(nil) })
	_, err := ai.ResolveExtractionModel("missing")
	port2genNoSuchModel(t, err, "missing", "extractionModel")
}
