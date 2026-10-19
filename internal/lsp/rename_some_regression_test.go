package lsp

import (
	"strings"
	"testing"
)

func TestRenameSomeMacroRejectsCapture(t *testing.T) {
	for _, macro := range []string{"if-some", "when-some"} {
		for _, spelling := range []struct{ name, declaration, head string }{
			{"plain", "(ns audit)", macro},
			{"qualified", "(ns audit)", "doggo.core/" + macro},
			{"namespace alias", "(ns audit (:require [doggo.core :as c]))", "c/" + macro},
			{"core rename", "(ns audit (:refer-clojure :rename {" + macro + " present}))", "present"},
			{"require rename", "(ns audit (:require [doggo.core :refer [" + macro + "] :rename {" + macro + " present}]))", "present"},
		} {
			t.Run(macro+"/"+spelling.name, func(t *testing.T) {
				body := "(" + spelling.head + " [renamed false] target)"
				if macro == "if-some" {
					body = "(" + spelling.head + " [renamed false] target 9)"
				}
				source := spelling.declaration + "\n(def target 7)\n" + body + "\n"
				if before := runRenameTestSource(t, source); before != "7" {
					t.Fatalf("before=%s", before)
				}
				actual, response := renameTestSource(t, source, 1, 5)
				if after := runRenameTestSource(t, actual); after != "7" {
					t.Errorf("unsafe WorkspaceEdit changed value to %s", after)
				}
				assertRenameV6Rejected(t, source, actual, response)
				localSource := strings.ReplaceAll(source, "renamed", "target")
				for _, offset := range []int{strings.LastIndex(localSource, "[target") + 1, strings.LastIndex(localSource, "target")} {
					position := byteOffsetToLSPPosition(localSource, offset)
					actual, response := renameTestSource(t, localSource, position.Line, position.Character)
					if actual != localSource {
						t.Errorf("local cursor selected global: %q; response=%v", actual, response)
					}
				}
			})
		}
	}
}

func TestRenameSomeMacroLocalCursorsDoNotTargetGlobal(t *testing.T) {
	for _, body := range []string{"(if-some [target false] target 9)", "(when-some [target false] nil target)"} {
		source := "(ns audit)\n(def target 7)\n" + body + "\n"
		for _, column := range []int{strings.Index(body, "target"), strings.LastIndex(body, "target")} {
			actual, response := renameTestSource(t, source, 2, column)
			if actual != source {
				t.Errorf("local cursor changed global: %q; response=%v", actual, response)
			}
		}
		if got := v7References(t, source, 1, 5); len(got) != 1 {
			t.Errorf("local references leaked: %v", got)
		}
		if got := runRenameTestSource(t, source); got != "false" {
			t.Errorf("value=%s", got)
		}
	}
}

func TestRenameSomeMacroScopesPreserveEvaluation(t *testing.T) {
	for _, tc := range []struct{ name, body, wantBody, value string }{
		{"if else outer", "(if-some [renamed nil] renamed target)", "(if-some [renamed nil] renamed renamed)", "7"},
		{"if initializer outer", "(if-some [renamed target] renamed 9)", "(if-some [renamed renamed] renamed 9)", "7"},
		{"when initializer outer", "(when-some [renamed target] nil renamed)", "(when-some [renamed renamed] nil renamed)", "7"},
		{"when body locals", "(when-some [target false] nil target)", "(when-some [target false] nil target)", "false"},
		{"if else same name global", "(if-some [target nil] target target)", "(if-some [target nil] target renamed)", "7"},
		{"if qualified global", "(if-some [renamed false] audit/target 9)", "(if-some [renamed false] audit/renamed 9)", "7"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := "(ns audit)\n(def target 7)\n" + tc.body + "\n"
			actual, response := renameTestSource(t, source, 1, 5)
			want := "(ns audit)\n(def renamed 7)\n" + tc.wantBody + "\n"
			if actual != want {
				t.Fatalf("rename=%q want=%q response=%v", actual, want, response)
			}
			if before, after := runRenameTestSource(t, source), runRenameTestSource(t, actual); before != tc.value || after != before {
				t.Fatalf("VM=%s/%s want=%s", before, after, tc.value)
			}
		})
	}
}

func TestRenameSomeUserShadowedHeadsEvaluateArguments(t *testing.T) {
	for _, macro := range []string{"if-some", "when-some"} {
		for _, binder := range []string{"(let [" + macro + " (fn [a b] a)] (" + macro + " [target] nil))", "(def " + macro + " (fn [a b] a))\n(" + macro + " [target] nil)"} {
			source := "(ns audit)\n(def target 7)\n" + binder + "\n"
			actual, response := renameTestSource(t, source, 1, 5)
			want := strings.ReplaceAll(source, "target", "renamed")
			if actual != want {
				t.Fatalf("rename=%q want=%q response=%v", actual, want, response)
			}
			if before, after := runRenameTestSource(t, source), runRenameTestSource(t, actual); before != "[7]" || after != before {
				t.Fatalf("VM=%s/%s", before, after)
			}
		}
	}
}
