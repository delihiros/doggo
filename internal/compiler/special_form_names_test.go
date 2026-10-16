package compiler

import "testing"

func TestUnshadowableSpecialFormNames(t *testing.T) {
	getResolvedSpecialFormCompiler("if") // initialize the compiler's actual dispatch table
	for name := range resolvedSpecialForms {
		want := !isOptimizedBuiltinForm(name)
		if actual := IsUnshadowableSpecialForm(name); actual != want {
			t.Errorf("IsUnshadowableSpecialForm(%q) = %t, want %t", name, actual, want)
		}
	}
	for _, name := range []string{"renamed", "count", "unknown", "audit/if"} {
		if IsUnshadowableSpecialForm(name) {
			t.Errorf("ordinary name %q classified as compiler syntax", name)
		}
	}
}
