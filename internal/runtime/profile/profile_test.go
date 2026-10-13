package profile

import "testing"

func TestDefaultIncludesPureCore(t *testing.T) {
	p := Default()
	if p == nil {
		t.Fatal("default profile is nil")
	}
	if got := p.Features(); len(got) != 1 || got[0] != FeaturePureCore {
		t.Fatalf("unexpected default features: %#v", got)
	}
}

func TestHostIncludesPureCoreAndHostEffects(t *testing.T) {
	p := Host()
	if p == nil {
		t.Fatal("host profile is nil")
	}
	got := p.Features()
	if len(got) != 2 || got[0] != FeaturePureCore || got[1] != FeatureHostEffects {
		t.Fatalf("unexpected host features: %#v", got)
	}
}
