package opt

type Config struct {
	EnableLoopFusion bool
}

func DefaultOptimizationConfig() *Config {
	return &Config{
		EnableLoopFusion: true,
	}
}

func NoLoopFusionConfig() *Config {
	return &Config{
		EnableLoopFusion: false,
	}
}

type Binding struct {
	Scope string
}

type Resolver interface {
	ResolveOptimizationSymbol(name string) (Binding, bool)
}

const (
	ScopeGlobal          = "GLOBAL"
	ScopeProtectedGlobal = "PROTECTED_GLOBAL"
	ScopeBuiltin         = "BUILTIN"
)

func canUseIndexBasedFusion(resolver Resolver) bool {
	requiredBuiltins := []string{"count", "nth", "<", "inc"}
	for _, name := range requiredBuiltins {
		binding, resolved := resolver.ResolveOptimizationSymbol(name)
		if !resolved {
			continue
		}
		if !isGlobalLikeScope(binding.Scope) && binding.Scope != ScopeBuiltin {
			return false
		}
	}
	return true
}

func isGlobalLikeScope(scope string) bool {
	return scope == ScopeGlobal || scope == ScopeProtectedGlobal || scope == ScopeBuiltin
}
