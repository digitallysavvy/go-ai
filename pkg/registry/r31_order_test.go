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
