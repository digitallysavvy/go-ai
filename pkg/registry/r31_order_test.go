package registry

import (
	"reflect"
	"testing"
)

// TS reports Object.keys(providers) (registration order) in NoSuchProviderError.
func TestNoSuchProviderErrorListsProvidersInRegistrationOrder(t *testing.T) {
	r := NewRegistry()
	for _, name := range []string{"zeta", "alpha", "mid"} {
		r.RegisterProvider(name, nil)
	}
	r.RegisterProvider("alpha", nil) // re-registering keeps the original position
	if got := r.ListProviders(); !reflect.DeepEqual(got, []string{"zeta", "alpha", "mid"}) {
		t.Fatalf("ListProviders = %v", got)
	}
	_, err := r.GetProvider("nope")
	want := "No such provider: nope (available providers: zeta,alpha,mid)"
	if err == nil || err.Error() != want {
		t.Fatalf("err = %v, want %q", err, want)
	}
}

// With many providers a map-ordered listing practically never matches the
// registration order, so this catches an unordered implementation reliably.
func TestListProvidersKeepsRegistrationOrderForManyProviders(t *testing.T) {
	r := NewRegistry()
	names := []string{"p09", "p03", "p17", "p01", "p12", "p05", "p20", "p08", "p14", "p02", "p11", "p19", "p06", "p16", "p04", "p10"}
	for _, name := range names {
		r.RegisterProvider(name, nil)
	}
	for i := 0; i < 5; i++ {
		if got := r.ListProviders(); !reflect.DeepEqual(got, names) {
			t.Fatalf("ListProviders = %v, want %v", got, names)
		}
	}
}
