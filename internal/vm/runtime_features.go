package vm

import "fmt"

type RuntimeFeature string

const (
	RuntimeFeatureMacroExpand       RuntimeFeature = "macro-expand"
	RuntimeFeatureFileLoad          RuntimeFeature = "file-load"
	RuntimeFeatureREPLIntrospection RuntimeFeature = "repl-introspection"
	RuntimeFeatureLazySeq           RuntimeFeature = "lazy-seq"
	RuntimeFeatureObservability     RuntimeFeature = "observability"
	RuntimeFeatureBasis             RuntimeFeature = "basis"
	RuntimeFeatureCLI               RuntimeFeature = "cli"
)

func DefaultRuntimeFeatures() []RuntimeFeature {
	return []RuntimeFeature{
		RuntimeFeatureMacroExpand,
		RuntimeFeatureFileLoad,
		RuntimeFeatureREPLIntrospection,
		RuntimeFeatureLazySeq,
		RuntimeFeatureObservability,
		RuntimeFeatureBasis,
		RuntimeFeatureCLI,
	}
}

func copyRuntimeFeatures(features []RuntimeFeature) []RuntimeFeature {
	if features == nil {
		return nil
	}
	out := make([]RuntimeFeature, len(features))
	copy(out, features)
	return out
}

func installRuntimeFeatures(r *Runner, features []RuntimeFeature) error {
	for i, feature := range features {
		if err := installRuntimeFeature(r, feature); err != nil {
			name := string(feature)
			if name == "" {
				name = fmt.Sprintf("feature[%d]", i)
			}
			return fmt.Errorf("runtime feature %q: %w", name, err)
		}
	}
	return nil
}

func installRuntimeFeature(r *Runner, feature RuntimeFeature) error {
	switch feature {
	case RuntimeFeatureMacroExpand:
		return r.installRuntimeBindingGroup(runtimeBindingGroupMacroExpand)
	case RuntimeFeatureFileLoad:
		return r.installRuntimeBindingGroup(runtimeBindingGroupFileLoad)
	case RuntimeFeatureREPLIntrospection:
		return r.installRuntimeBindingGroup(runtimeBindingGroupREPLIntrospection)
	case RuntimeFeatureLazySeq:
		return r.installRuntimeBindingGroup(runtimeBindingGroupLazySeq)
	case RuntimeFeatureObservability:
		return r.installRuntimeBindingGroup(runtimeBindingGroupObservability)
	case RuntimeFeatureBasis:
		return r.installRuntimeBindingGroup(runtimeBindingGroupBasis)
	case RuntimeFeatureCLI:
		return r.installRuntimeBindingGroup(runtimeBindingGroupCLI)
	default:
		return fmt.Errorf("unknown runtime feature %q", feature)
	}
}
