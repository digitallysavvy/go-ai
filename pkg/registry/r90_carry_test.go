package registry

import "testing"

// TS provider-registry.ts 477/490 interpolate the id into a template
// literal: `The provider "${id}" ...`, with no escaping (Go %q escaped it).
func TestR90carryUnsupportedAPIQuotesIDLikeTS(t *testing.T) {
	r := NewRegistry()
	r.RegisterProvider("my\tprov", NewCustomProvider(CustomProviderOptions{}))
	if _, err := r.ResolveFilesAPI("my\tprov"); err == nil || err.Error() != "The provider \"my\tprov\" does not support file uploads. Make sure it exposes a Files() method." {
		t.Errorf("files err = %v", err)
	}
	if _, err := r.ResolveSkillsAPI("my\tprov"); err == nil || err.Error() != "The provider \"my\tprov\" does not support skills. Make sure it exposes a Skills() method." {
		t.Errorf("skills err = %v", err)
	}
}
