package profile

type Feature string

const (
	FeaturePureCore    Feature = "pure-core"
	FeatureFileRead    Feature = "file-read"
	FeatureHostEffects Feature = "host-effects"
)

type Profile struct {
	name     string
	features []Feature
}

func New(name string) *Profile {
	return &Profile{name: name}
}

func Default() *Profile {
	return New("default").SetFeatures([]Feature{FeaturePureCore})
}

func Host() *Profile {
	return New("host").SetFeatures([]Feature{FeaturePureCore, FeatureHostEffects})
}

func (p *Profile) Name() string {
	if p == nil {
		return ""
	}
	return p.name
}

func (p *Profile) SetFeatures(features []Feature) *Profile {
	if p == nil {
		p = New("")
	}
	p.features = append([]Feature(nil), features...)
	return p
}

func (p *Profile) Features() []Feature {
	if p == nil {
		return nil
	}
	return append([]Feature(nil), p.features...)
}
