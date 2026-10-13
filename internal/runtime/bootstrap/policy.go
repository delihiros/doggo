package bootstrap

type Policy struct {
	Namespaces []string
}

func DefaultPolicy() Policy {
	return Policy{Namespaces: []string{"doggo.core"}}
}

func (p Policy) LoadsNamespace(name string) bool {
	for _, candidate := range p.Namespaces {
		if candidate == name {
			return true
		}
	}
	return false
}
