package vm

import (
	"doggo/internal/vm/engine"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDocAndSourceHelpersReturnMetadataAndDefinitionForm(t *testing.T) {
	runner := MustNewRunner()

	src := `(ns my.app)
(def ^{:doc "double value"} my-fn (fn [x] (* x 2)))`
	if _, err := runner.RunWithOrigin(src, engine.SourceOrigin{File: "src/my_app.dog"}); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	docResult, err := runner.Run(`(doc* 'my-fn)`)
	if err != nil {
		t.Fatalf("doc* failed: %v", err)
	}
	docText := mustString(t, docResult)
	if !strings.Contains(docText, "my.app/my-fn") {
		t.Fatalf("expected fully-qualified symbol in doc output, got %s", docText)
	}
	if !strings.Contains(docText, "double value") {
		t.Fatalf("expected :doc content in doc output, got %s", docText)
	}
	if !strings.Contains(docText, "src/my_app.dog:2:1") {
		t.Fatalf("expected source location in doc output, got %s", docText)
	}

	sourceResult, err := runner.Run(`(source-str* 'my-fn)`)
	if err != nil {
		t.Fatalf("source-str* failed: %v", err)
	}
	sourceText := mustString(t, sourceResult)
	if !strings.Contains(sourceText, `(def ^{:doc "double value"} my-fn (fn [x] (* x 2)))`) {
		t.Fatalf("expected full definition form in source output, got %s", sourceText)
	}
}

func TestSourceHelperReportsUnavailableWhenSourceCannotBeResolved(t *testing.T) {
	runner := MustNewRunner()

	result, err := runner.Run(`(source-str* 'map)`)
	if err != nil {
		t.Fatalf("source-str* failed: %v", err)
	}
	if !strings.Contains(mustString(t, result), "unavailable") {
		t.Fatalf("expected unavailable message, got %s", mustString(t, result))
	}
}

func TestDocAndSourceHelpersSupportMacrosAndQualifiedBuiltins(t *testing.T) {
	runner := MustNewRunner()

	src := `(ns my.app)
(defmacro helper "macro docs" [] 1)`
	if _, err := runner.RunWithOrigin(src, engine.SourceOrigin{File: "src/my_macro.dog"}); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	docResult, err := runner.Run(`(doc* 'doggo.core/map)`)
	if err != nil {
		t.Fatalf("doc* for builtin failed: %v", err)
	}
	if !strings.Contains(mustString(t, docResult), "doggo.core/map") {
		t.Fatalf("expected qualified builtin name in doc output, got %s", mustString(t, docResult))
	}

	macroDoc, err := runner.Run(`(doc* 'my.app/helper)`)
	if err != nil {
		t.Fatalf("doc* for macro failed: %v", err)
	}
	if !strings.Contains(mustString(t, macroDoc), "my.app/helper") || !strings.Contains(mustString(t, macroDoc), "macro docs") {
		t.Fatalf("expected macro doc output, got %s", mustString(t, macroDoc))
	}

	macroSource, err := runner.Run(`(source-str* 'my.app/helper)`)
	if err != nil {
		t.Fatalf("source-str* for macro failed: %v", err)
	}
	if !strings.Contains(mustString(t, macroSource), `(defmacro helper "macro docs" [] 1)`) {
		t.Fatalf("expected macro source output, got %s", mustString(t, macroSource))
	}
}

func TestDocAndSourceHelpersPreferMacrosOverBuiltinsWithSameName(t *testing.T) {
	runner := MustNewRunner()

	src := `(ns my.app)
(defmacro map "macro docs" [] 1)`
	if _, err := runner.RunWithOrigin(src, engine.SourceOrigin{File: "src/my_map_macro.dog"}); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	docResult, err := runner.Run(`(doc* 'map)`)
	if err != nil {
		t.Fatalf("doc* failed: %v", err)
	}
	if !strings.Contains(mustString(t, docResult), "my.app/map") || !strings.Contains(mustString(t, docResult), "macro docs") {
		t.Fatalf("expected macro to override builtin in doc output, got %s", mustString(t, docResult))
	}
	if strings.Contains(mustString(t, docResult), "doggo.core/map") {
		t.Fatalf("expected builtin doc to be hidden by macro, got %s", mustString(t, docResult))
	}

	sourceResult, err := runner.Run(`(source-str* 'map)`)
	if err != nil {
		t.Fatalf("source-str* failed: %v", err)
	}
	if !strings.Contains(mustString(t, sourceResult), `(defmacro map "macro docs" [] 1)`) {
		t.Fatalf("expected macro source output, got %s", mustString(t, sourceResult))
	}
}

func TestDoggoReplNamespaceCanBeRequiredOutsideREPL(t *testing.T) {
	runner := MustNewRunner()

	if _, err := runner.Run(`(require '[doggo.repl :refer [doc source]])`); err != nil {
		t.Fatalf("require doggo.repl failed: %v", err)
	}
	if _, err := runner.Run(`(def foo 1)`); err != nil {
		t.Fatalf("def failed: %v", err)
	}

	docResult, err := runner.Run(`(doc foo)`)
	if err != nil {
		t.Fatalf("doc macro failed: %v", err)
	}
	if docResult.String() != "nil" {
		t.Fatalf("expected doc macro to return nil, got %s", docResult.String())
	}

	sourceResult, err := runner.Run(`(source foo)`)
	if err != nil {
		t.Fatalf("source macro failed: %v", err)
	}
	if sourceResult.String() != "nil" {
		t.Fatalf("expected source macro to return nil, got %s", sourceResult.String())
	}
}

func TestDocAndSourceHelpersPreferVarsOverMacrosWithSameName(t *testing.T) {
	runner := MustNewRunner()

	src := `(ns my.app)
(defmacro helper "macro docs" [] 1)
(def ^{:doc "var docs"} helper 2)`
	if _, err := runner.RunWithOrigin(src, engine.SourceOrigin{File: "src/my_macro_shadow.dog"}); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	docResult, err := runner.Run(`(doc* 'helper)`)
	if err != nil {
		t.Fatalf("doc* failed: %v", err)
	}
	if strings.Contains(mustString(t, docResult), "macro docs") {
		t.Fatalf("expected var to shadow macro in doc output, got %s", mustString(t, docResult))
	}
	if !strings.Contains(mustString(t, docResult), "var docs") {
		t.Fatalf("expected var doc output, got %s", mustString(t, docResult))
	}

	sourceResult, err := runner.Run(`(source-str* 'helper)`)
	if err != nil {
		t.Fatalf("source-str* failed: %v", err)
	}
	if strings.Contains(mustString(t, sourceResult), "defmacro helper") {
		t.Fatalf("expected var to shadow macro in source output, got %s", mustString(t, sourceResult))
	}
	if !strings.Contains(mustString(t, sourceResult), `(def ^{:doc "var docs"} helper 2)`) {
		t.Fatalf("expected var source output, got %s", mustString(t, sourceResult))
	}

	findDocResult, err := runner.Run(`(pr-str (find-doc* "var docs"))`)
	if err != nil {
		t.Fatalf("find-doc* failed: %v", err)
	}
	if !strings.Contains(mustString(t, findDocResult), "my.app/helper") {
		t.Fatalf("expected var doc entry, got %s", mustString(t, findDocResult))
	}
	if strings.Contains(mustString(t, findDocResult), "macro docs") {
		t.Fatalf("expected macro doc to be shadowed by var doc, got %s", mustString(t, findDocResult))
	}
}

func TestDocAndSourceHelpersPreferActiveMacroOverOlderVar(t *testing.T) {
	runner := MustNewRunner()

	src := `(ns my.app)
(def ^{:doc "var docs"} helper (fn [] 2))
(defmacro helper "macro docs" [] 1)`
	if _, err := runner.RunWithOrigin(src, engine.SourceOrigin{File: "src/my_macro_override.dog"}); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	docResult, err := runner.Run(`(doc* 'helper)`)
	if err != nil {
		t.Fatalf("doc* failed: %v", err)
	}
	if !strings.Contains(mustString(t, docResult), "macro docs") {
		t.Fatalf("expected active macro doc output, got %s", mustString(t, docResult))
	}
	if strings.Contains(mustString(t, docResult), "var docs") {
		t.Fatalf("expected older var docs to be replaced, got %s", mustString(t, docResult))
	}

	sourceResult, err := runner.Run(`(source-str* 'helper)`)
	if err != nil {
		t.Fatalf("source-str* failed: %v", err)
	}
	if !strings.Contains(mustString(t, sourceResult), `(defmacro helper "macro docs" [] 1)`) {
		t.Fatalf("expected active macro source output, got %s", mustString(t, sourceResult))
	}
}

func TestDocHelpersUseSourceNamespaceForReferredVars(t *testing.T) {
	projectRoot := t.TempDir()
	libPath := filepath.Join(projectRoot, "lib.dog")
	libSource := `(ns lib)
(def ^{:doc "helper docs"} helper (fn [] 1))`
	if err := os.WriteFile(libPath, []byte(libSource), 0644); err != nil {
		t.Fatalf("write lib: %v", err)
	}

	runner, err := NewRunnerWithConfig(RunnerConfig{
		ProjectRoot: projectRoot,
		LoadPaths:   []string{projectRoot},
	})
	if err != nil {
		t.Fatalf("new runner failed: %v", err)
	}

	result, err := runner.Run(`(ns user (:require [lib :refer [helper]] [doggo.repl :as repl]))
(doc* 'helper)`)
	if err != nil {
		t.Fatalf("doc* failed: %v", err)
	}
	if !strings.Contains(mustString(t, result), "lib/helper") {
		t.Fatalf("expected referred var to retain source namespace, got %s", mustString(t, result))
	}
	if strings.Contains(mustString(t, result), "user/helper") {
		t.Fatalf("expected doc output to avoid refer alias namespace, got %s", mustString(t, result))
	}
}

func TestDocHelpersUseLocalMetadataAfterShadowingReferredVar(t *testing.T) {
	projectRoot := t.TempDir()
	libPath := filepath.Join(projectRoot, "lib.dog")
	libSource := `(ns lib)
(def ^{:doc "helper docs"} helper (fn [] 1))`
	if err := os.WriteFile(libPath, []byte(libSource), 0644); err != nil {
		t.Fatalf("write lib: %v", err)
	}

	runner, err := NewRunnerWithConfig(RunnerConfig{
		ProjectRoot: projectRoot,
		LoadPaths:   []string{projectRoot},
	})
	if err != nil {
		t.Fatalf("new runner failed: %v", err)
	}

	result, err := runner.Run(`(ns user (:require [lib :refer [helper]] [doggo.repl :as repl]))
(def ^{:doc "local docs"} helper (fn [] 2))
(doc* 'helper)`)
	if err != nil {
		t.Fatalf("doc* failed: %v", err)
	}
	if !strings.Contains(mustString(t, result), "user/helper") {
		t.Fatalf("expected local def to replace refer provenance, got %s", mustString(t, result))
	}
	if !strings.Contains(mustString(t, result), "local docs") {
		t.Fatalf("expected local def metadata to replace refer metadata, got %s", mustString(t, result))
	}
	if strings.Contains(mustString(t, result), "lib/helper") {
		t.Fatalf("expected refer provenance to be cleared, got %s", mustString(t, result))
	}
}

func TestDocAndSourceHelpersResolveUnqualifiedSymbolsInFunctionNamespace(t *testing.T) {
	runner := MustNewRunner()

	src := `(ns my.docs)
(def ^{:doc "helper docs"} helper (fn [] 1))
(def probe-doc (fn [] (doc* 'helper)))
(def probe-source (fn [] (source-str* 'helper)))
(ns other.ns)
(my.docs/probe-doc)`
	result, err := runner.RunWithOrigin(src, engine.SourceOrigin{File: "src/docs_ns.dog"})
	if err != nil {
		t.Fatalf("probe-doc failed: %v", err)
	}
	if !strings.Contains(mustString(t, result), "my.docs/helper") {
		t.Fatalf("expected lexical namespace resolution for doc*, got %s", mustString(t, result))
	}

	sourceResult, err := runner.Run(`(my.docs/probe-source)`)
	if err != nil {
		t.Fatalf("probe-source failed: %v", err)
	}
	if !strings.Contains(mustString(t, sourceResult), `(def ^{:doc "helper docs"} helper (fn [] 1))`) {
		t.Fatalf("expected lexical namespace resolution for source-str*, got %s", mustString(t, sourceResult))
	}
}

func TestSourceHelperPrefersNamedDefinitionOnSharedLine(t *testing.T) {
	runner := MustNewRunner()

	src := `(ns my.docs) (defmacro helper "macro docs" [] 1)`
	if _, err := runner.RunWithOrigin(src, engine.SourceOrigin{File: "src/shared_line.dog"}); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	sourceResult, err := runner.Run(`(source-str* 'helper)`)
	if err != nil {
		t.Fatalf("source-str* failed: %v", err)
	}
	if !strings.Contains(mustString(t, sourceResult), `(defmacro helper "macro docs" [] 1)`) {
		t.Fatalf("expected exact defmacro form on shared line, got %s", mustString(t, sourceResult))
	}
	if strings.Contains(mustString(t, sourceResult), `(ns my.docs)`) {
		t.Fatalf("expected source-str* to avoid earlier same-line form, got %s", mustString(t, sourceResult))
	}
}

func TestSourceHelperFindsNamedDefinitionInsideTopLevelDo(t *testing.T) {
	runner := MustNewRunner()

	src := `(do (ns my.docs) (defmacro helper "macro docs" [] 1))`
	if _, err := runner.RunWithOrigin(src, engine.SourceOrigin{File: "src/top_level_do.dog"}); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	sourceResult, err := runner.Run(`(source-str* 'helper)`)
	if err != nil {
		t.Fatalf("source-str* failed: %v", err)
	}
	if !strings.Contains(mustString(t, sourceResult), `(defmacro helper "macro docs" [] 1)`) {
		t.Fatalf("expected source-str* to find nested top-level defmacro, got %s", mustString(t, sourceResult))
	}
	if strings.Contains(mustString(t, sourceResult), `(do (ns my.docs)`) {
		t.Fatalf("expected source-str* to return the definition, not the enclosing do, got %s", mustString(t, sourceResult))
	}
}

func TestSourceHelperIgnoresQuotedDefinitionsOnSameLine(t *testing.T) {
	runner := MustNewRunner()

	src := `(ns my.docs)
(defmacro helper "real" [] 1) (def quoted '(defmacro helper "quoted" [] 1))`
	if _, err := runner.RunWithOrigin(src, engine.SourceOrigin{File: "src/quoted_same_line.dog"}); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	sourceResult, err := runner.Run(`(source-str* 'helper)`)
	if err != nil {
		t.Fatalf("source-str* failed: %v", err)
	}
	if !strings.Contains(mustString(t, sourceResult), `(defmacro helper "real" [] 1)`) {
		t.Fatalf("expected source-str* to return the real definition, got %s", mustString(t, sourceResult))
	}
	if strings.Contains(mustString(t, sourceResult), `"quoted"`) {
		t.Fatalf("expected source-str* to ignore quoted definition data, got %s", mustString(t, sourceResult))
	}
}

func TestSourceHelperReturnsDefinitionInsideConditional(t *testing.T) {
	runner := MustNewRunner()

	src := `(ns my.docs)
(if true
  (def helper (fn [] 1))
  nil)`
	if _, err := runner.RunWithOrigin(src, engine.SourceOrigin{File: "src/conditional_def.dog"}); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	sourceResult, err := runner.Run(`(source-str* 'helper)`)
	if err != nil {
		t.Fatalf("source-str* failed: %v", err)
	}
	if !strings.Contains(mustString(t, sourceResult), `(def helper (fn [] 1))`) {
		t.Fatalf("expected source-str* to return the nested def, got %s", mustString(t, sourceResult))
	}
	if strings.Contains(mustString(t, sourceResult), `(if true`) {
		t.Fatalf("expected source-str* to avoid enclosing conditional, got %s", mustString(t, sourceResult))
	}
}

func TestSourceHelperReturnsDefinitionCreatedInsideFunctionBody(t *testing.T) {
	runner := MustNewRunner()

	src := `(ns my.docs)
(def trigger
  (fn []
    (def helper (fn [] 1))
    :ok))
(trigger)`
	if _, err := runner.RunWithOrigin(src, engine.SourceOrigin{File: "src/fn_body_def.dog"}); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	sourceResult, err := runner.Run(`(source-str* 'helper)`)
	if err != nil {
		t.Fatalf("source-str* failed: %v", err)
	}
	if !strings.Contains(mustString(t, sourceResult), `(def helper (fn [] 1))`) {
		t.Fatalf("expected source-str* to return the function-body def, got %s", mustString(t, sourceResult))
	}
	if strings.Contains(mustString(t, sourceResult), `(def trigger`) {
		t.Fatalf("expected source-str* to avoid enclosing fn definition, got %s", mustString(t, sourceResult))
	}
}

func TestSourceHelperReturnsOriginalDefnSource(t *testing.T) {
	runner := MustNewRunner()

	src := `(ns my.docs)
(defn alpha
  [x]
  x)`
	if _, err := runner.RunWithOrigin(src, engine.SourceOrigin{File: "src/defn_source.dog"}); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	sourceResult, err := runner.Run(`(source-str* 'alpha)`)
	if err != nil {
		t.Fatalf("source-str* failed: %v", err)
	}
	if !strings.Contains(mustString(t, sourceResult), "(defn alpha") {
		t.Fatalf("expected source-str* to preserve original defn form, got %s", mustString(t, sourceResult))
	}
	if strings.Contains(mustString(t, sourceResult), "(def alpha (fn") {
		t.Fatalf("expected source-str* to avoid macro-expanded def form, got %s", mustString(t, sourceResult))
	}
}

func TestSourceHelperReturnsOriginalMacroCallSource(t *testing.T) {
	runner := MustNewRunner()

	src := "(ns my.docs)\n" +
		"(defmacro defconst [name value]\n" +
		"  `(def ~name ~value))\n" +
		"(defconst answer 42)"
	if _, err := runner.RunWithOrigin(src, engine.SourceOrigin{File: "src/defconst_source.dog"}); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	sourceResult, err := runner.Run(`(source-str* 'answer)`)
	if err != nil {
		t.Fatalf("source-str* failed: %v", err)
	}
	if !strings.Contains(mustString(t, sourceResult), "(defconst answer 42)") {
		t.Fatalf("expected source-str* to return original macro call, got %s", mustString(t, sourceResult))
	}
	if strings.Contains(mustString(t, sourceResult), "(def answer 42)") {
		t.Fatalf("expected source-str* to avoid expanded def form, got %s", mustString(t, sourceResult))
	}

	docResult, err := runner.Run(`(doc* 'answer)`)
	if err != nil {
		t.Fatalf("doc* failed: %v", err)
	}
	if !strings.Contains(mustString(t, docResult), "src/defconst_source.dog:4:1") {
		t.Fatalf("expected macro-generated definition to keep its source span, got %s", mustString(t, docResult))
	}
}

func TestSourceHelperPreservesOutermostMacroCallAcrossNestedExpansion(t *testing.T) {
	runner := MustNewRunner()

	src := "(ns my.docs)\n" +
		"(defmacro inner [name value]\n" +
		"  `(def ~name ~value))\n" +
		"(defmacro outer [name value]\n" +
		"  `(inner ~name ~value))\n" +
		"(outer answer 42)"
	if _, err := runner.RunWithOrigin(src, engine.SourceOrigin{File: "src/nested_macro_source.dog"}); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	sourceResult, err := runner.Run(`(source-str* 'answer)`)
	if err != nil {
		t.Fatalf("source-str* failed: %v", err)
	}
	if !strings.Contains(mustString(t, sourceResult), "(outer answer 42)") {
		t.Fatalf("expected source-str* to keep outermost macro call, got %s", mustString(t, sourceResult))
	}
	if strings.Contains(mustString(t, sourceResult), "(inner answer 42)") || strings.Contains(mustString(t, sourceResult), "(def answer 42)") {
		t.Fatalf("expected source-str* to avoid synthetic nested expansion forms, got %s", mustString(t, sourceResult))
	}
}

func TestSourceHelperPreservesOutermostMacroCallForPassthroughMacro(t *testing.T) {
	runner := MustNewRunner()

	src := "(ns my.docs)\n" +
		"(defmacro passthrough [form] form)\n" +
		"(passthrough (def answer 42))"
	if _, err := runner.RunWithOrigin(src, engine.SourceOrigin{File: "src/passthrough_macro_source.dog"}); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	sourceResult, err := runner.Run(`(source-str* 'answer)`)
	if err != nil {
		t.Fatalf("source-str* failed: %v", err)
	}
	if !strings.Contains(mustString(t, sourceResult), "(passthrough (def answer 42))") {
		t.Fatalf("expected source-str* to preserve outermost macro call, got %s", mustString(t, sourceResult))
	}
	if mustString(t, sourceResult) == "(def answer 42)" || strings.Contains(mustString(t, sourceResult), "\n(def answer 42)") {
		t.Fatalf("expected source-str* to avoid extracted inner def form, got %s", mustString(t, sourceResult))
	}
}

func TestSourceHelperReturnsDefmultiSource(t *testing.T) {
	runner := MustNewRunner()

	src := `(ns my.docs)
(defmulti area :type)`
	if _, err := runner.RunWithOrigin(src, engine.SourceOrigin{File: "src/defmulti_source.dog"}); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	sourceResult, err := runner.Run(`(source-str* 'area)`)
	if err != nil {
		t.Fatalf("source-str* failed: %v", err)
	}
	if !strings.Contains(mustString(t, sourceResult), "(defmulti area :type)") {
		t.Fatalf("expected source-str* to return defmulti form, got %s", mustString(t, sourceResult))
	}
}

func TestSourceHelperReturnsDefrecordAndConstructorsSource(t *testing.T) {
	runner := MustNewRunner()

	src := `(ns my.docs)
(defrecord User [name])`
	if _, err := runner.RunWithOrigin(src, engine.SourceOrigin{File: "src/defrecord_source.dog"}); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	for _, target := range []string{"User", "->User", "map->User"} {
		sourceResult, err := runner.Run(`(source-str* '` + target + `)`)
		if err != nil {
			t.Fatalf("source-str* %s failed: %v", target, err)
		}
		if !strings.Contains(mustString(t, sourceResult), "(defrecord User [name])") {
			t.Fatalf("expected %s source to point at defrecord form, got %s", target, mustString(t, sourceResult))
		}
	}
}

func TestSourceHelperReturnsDeftypeAndConstructorSource(t *testing.T) {
	runner := MustNewRunner()

	src := `(ns my.docs)
(deftype Counter [value])`
	if _, err := runner.RunWithOrigin(src, engine.SourceOrigin{File: "src/deftype_source.dog"}); err != nil {
		t.Fatalf("setup failed: %v", err)
	}

	for _, target := range []string{"Counter", "->Counter"} {
		sourceResult, err := runner.Run(`(source-str* '` + target + `)`)
		if err != nil {
			t.Fatalf("source-str* %s failed: %v", target, err)
		}
		if !strings.Contains(mustString(t, sourceResult), "(deftype Counter [value])") {
			t.Fatalf("expected %s source to point at deftype form, got %s", target, mustString(t, sourceResult))
		}
	}
}

func TestSourceHelperWorksForPrecompiledSource(t *testing.T) {
	runner := MustNewRunner()

	code, err := runner.Compile("(ns my.docs)\n(defn alpha [x] x)\n")
	if err != nil {
		t.Fatalf("compile failed: %v", err)
	}
	if _, err := runner.RunPrecompiled(code); err != nil {
		t.Fatalf("RunPrecompiled failed: %v", err)
	}

	sourceResult, err := runner.Run(`(source-str* 'alpha)`)
	if err != nil {
		t.Fatalf("source-str* failed: %v", err)
	}
	if !strings.Contains(mustString(t, sourceResult), "(defn alpha [x] x)") {
		t.Fatalf("expected source-str* to work for precompiled code, got %s", mustString(t, sourceResult))
	}
}
