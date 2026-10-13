package lsp

import (
	"strings"
	"testing"

	"doggo/internal/vm"
)

func TestRenameMetadataBinderContainersDoNotChangeGlobal(t *testing.T) {
	for _, tc := range []struct{ name, form, suffix string }{
		{"let", "(let ^:probe [target 2] target)", ""},
		{"loop", "(loop ^:probe [target 2] target)", ""},
		{"for", "(first (for ^:probe [target [2]] target))", ""},
		{"doseq", "(let [result (atom 0)] (doseq ^:probe [target [2]] (reset! result target)) @result)", ""},
		{"if-let", "(if-let ^:probe [target 2] target 0)", ""},
		{"when-let", "(when-let ^:probe [target 2] target)", ""},
		{"defn parameters", "(defn read-value ^:probe [target] target)", "\n(read-value 2)"},
		{"defmacro parameters", "(defmacro read-value ^:probe [target] target)", "\n(read-value 2)"},
		{"defn multi-arity parameters", "(defn read-value (^:probe [target] target))", "\n(read-value 2)"},
		{"fn parameters control", "((fn ^:probe [target] target) 2)", ""},
		{"named fn parameters", "((fn read-value ^:probe [target] target) 2)", ""},
		{"fn multi-arity parameters", "((fn (^:probe [target] target)) 2)", ""},
		{"fn decorated arity", "((fn ([] 0) ^:probe ([target] target)) 2)", ""},
		{"defn decorated arity", "(defn read-value ([] 0) ^:probe ([target] target))", "\n(read-value 2)"},
		{"letfn bindings", "(letfn ^:probe [(read-value [target] target)] (read-value 2))", ""},
		{"letfn parameters", "(letfn [(read-value ^:probe [target] target)] (read-value 2))", ""},
		{"letfn multi-arity parameters", "(letfn [(read-value (^:probe [target] target))] (read-value 2))", ""},
		{"letfn decorated arity", "(letfn [(read-value ([] 0) ^:probe ([target] target))] (read-value 2))", ""},
		{"letfn decorated function", "(letfn [^:probe (read-value [target] target)] (read-value 2))", ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := "(ns demo)\n(def target 7)\n" + tc.form + tc.suffix + "\n"
			if before := runRenameTestSource(t, source); before != "2" {
				t.Fatalf("VM local oracle = %s, want 2", before)
			}
			actual, response := renameTestSource(t, source, 2, strings.LastIndex(tc.form, "target"))
			if actual != source {
				t.Fatalf("metadata binder container cursor changed unrelated global: %q; response = %v", actual, response)
			}
		})
	}
}

func TestRenameMetadataBindingCursorDoesNotChangeGlobal(t *testing.T) {
	for _, tc := range []struct{ name, body string }{
		{"let symbol", "(let [^:probe target 2] target)"},
		{"fn parameter", "((fn [^:probe target] target) 2)"},
		{"let vector", "(let [^:probe [target] [2]] target)"},
		{"map keys symbol", "(let [{:keys [^:probe target]} {:target 2}] target)"},
		{"map strs symbol", "(let [{:strs [^:probe target]} {\"target\" 2}] target)"},
		{"map pattern", "(let [^:probe {:keys [target]} {:target 2}] target)"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := "(ns demo)\n(def target 7)\n" + tc.body + "\n"
			if before := runRenameTestSource(t, source); before != "2" {
				t.Fatalf("VM local oracle = %s, want 2", before)
			}
			actual, response := renameTestSource(t, source, 2, strings.LastIndex(tc.body, "target"))
			if actual != source {
				t.Fatalf("metadata local cursor changed unrelated global: %q; response = %v", actual, response)
			}
		})
	}
}

func TestRenameMapLiteralValuesDoNotBindGlobalReaderReference(t *testing.T) {
	for _, tc := range []struct{ name, body, want string }{
		{"default", "(let [{:keys [x] :or {x target}} {}] (deref #'target))", "(let [{:keys [x] :or {x target}} {}] (deref #'renamed))"},
		{"lookup key", "(let [{x target} {'target 2}] (deref #'target))", "(let [{x target} {'target 2}] (deref #'renamed))"},
		{"ignored directive", "(let [{:unused target} {}] (deref #'target))", "(let [{:unused target} {}] (deref #'renamed))"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := "(ns demo)\n(def target 7)\n" + tc.body + "\n"
			if before := runRenameTestSource(t, source); before != "7" {
				t.Fatalf("VM global Var oracle = %s, want 7", before)
			}
			actual, response := renameTestSource(t, source, 1, 5)
			want := "(ns demo)\n(def renamed 7)\n" + tc.want + "\n"
			if actual != want {
				t.Fatalf("literal map value suppressed evaluated Var: %q, want %q; response = %v", actual, want, response)
			}
			if after := runRenameTestSource(t, actual); after != "7" {
				t.Fatalf("fresh VM after = %s, want 7", after)
			}
		})
	}
}

func TestRenameMapDefaultPreservesLiteralData(t *testing.T) {
	body := "(let [{:keys [x] :or {x (deref #'target)}} {}] x)"
	source := "(ns demo)\n(def target 7)\n" + body + "\n"
	before := runRenameTestSource(t, source)
	if before != "(deref (var target))" {
		t.Fatalf("VM default oracle = %s, want literal Var form", before)
	}
	actual, response := renameTestSource(t, source, 1, 5)
	if actual != "(ns demo)\n(def renamed 7)\n"+body+"\n" {
		t.Fatalf("literal default changed: %q; response = %v", actual, response)
	}
	if after := runRenameTestSource(t, actual); after != before {
		t.Fatalf("fresh VM after = %s, want %s", after, before)
	}
}

func TestRenameVarCannotReferenceLocalShadow(t *testing.T) {
	source := "(ns demo)\n(def target 7)\n(let [target 2] (deref #'target))\n"
	runner, err := vm.NewRunner()
	if err != nil {
		t.Fatal(err)
	}
	_, err = runner.Run(source)
	if err == nil || !strings.Contains(err.Error(), "var can only reference vars") {
		t.Fatalf("VM accepted Var of a local binding: %v", err)
	}
	actual, response := renameTestSource(t, source, 2, 24)
	if actual != source {
		t.Fatalf("unsupported local Var cursor changed global: %q; response = %v", actual, response)
	}
}

func TestRenameMetadataLiteralCursorDoesNotChangeGlobal(t *testing.T) {
	body := "(meta ^{:probe target} [])"
	source := "(ns demo)\n(def target 7)\n" + body + "\n"
	if before := runRenameTestSource(t, source); before != "{:probe target}" {
		t.Fatalf("VM metadata oracle = %s, want literal target", before)
	}
	actual, response := renameTestSource(t, source, 2, strings.Index(body, "target"))
	if actual != source {
		t.Fatalf("metadata data cursor changed unrelated global: %q; response = %v", actual, response)
	}
}

func TestRenameMetadataFollowsReaderEvaluation(t *testing.T) {
	for _, tc := range []struct{ name, body, want string }{
		{"literal", "(meta ^{:probe target} [])", "(meta ^{:probe target} [])"},
		{"quasiquote literal", "(meta `^{:probe target} [])", "(meta `^{:probe target} [])"},
		{"quasiquote active unquote", "(meta `^{:probe ~target} [])", "(meta `^{:probe ~renamed} [])"},
		{"quasiquote active explicit unquote", "(meta (quasiquote ^{:probe (unquote target)} []))", "(meta (quasiquote ^{:probe (unquote renamed)} []))"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := "(ns demo)\n(def target 7)\n" + tc.body + "\n"
			before := runRenameTestSource(t, source)
			actual, response := renameTestSource(t, source, 1, 5)
			want := "(ns demo)\n(def renamed 7)\n" + tc.want + "\n"
			if actual != want {
				t.Fatalf("metadata rename = %q, want %q; response = %v", actual, want, response)
			}
			if after := runRenameTestSource(t, actual); after != before {
				t.Fatalf("fresh VM after = %s, want %s", after, before)
			}
			t.Logf("fresh VM metadata before/after = %s", before)
		})
	}
}
