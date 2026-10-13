package services

type ServiceSet struct {
	graph      *Graph
	Namespaces NamespaceServices
	Sources    SourceRegistry
	Execution  ExecutionServices
	Basis      BasisServices
}

func (s *ServiceSet) Graph() *Graph {
	if s == nil {
		return nil
	}
	return s.graph
}
