package vm

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestSlurpReadsTextAndBinary(t *testing.T) {
	runner := MustNewRunner()
	textPath := filepath.Join(t.TempDir(), "text.txt")
	binaryPath := filepath.Join(t.TempDir(), "binary.bin")
	if err := os.WriteFile(textPath, []byte("Hello, World!"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(binaryPath, []byte{0, 127, 255}, 0o644); err != nil {
		t.Fatal(err)
	}

	assertRunResult(t, runner, `(slurp "`+filepath.ToSlash(textPath)+`")`, `"Hello, World!"`)
	assertRunResult(t, runner, `(slurp "`+filepath.ToSlash(binaryPath)+`" {:binary true})`, `[0 127 255]`)
}

func TestSpitWritesTextAppendAndBinary(t *testing.T) {
	runner := MustNewRunner()
	textPath := filepath.Join(t.TempDir(), "text.txt")
	binaryPath := filepath.Join(t.TempDir(), "binary.bin")

	_, err := runner.Run(`
		(do
		  (spit "` + filepath.ToSlash(textPath) + `" {:a 1})
		  (spit "` + filepath.ToSlash(textPath) + `" [2 3] {:append true})
		  (spit "` + filepath.ToSlash(binaryPath) + `" [0 127 255] {:binary true}))`)
	if err != nil {
		t.Fatal(err)
	}

	if got, err := os.ReadFile(textPath); err != nil || string(got) != "{:a 1}[2 3]" {
		t.Fatalf("text output = %q, %v", got, err)
	}
	if got, err := os.ReadFile(binaryPath); err != nil || !bytes.Equal(got, []byte{0, 127, 255}) {
		t.Fatalf("binary output = %v, %v", got, err)
	}
}

func TestIOOptionsRejectInvalidMaps(t *testing.T) {
	runner := MustNewRunner()
	path := filepath.ToSlash(filepath.Join(t.TempDir(), "options.txt"))
	tests := []string{
		`(spit "` + path + `" "x" {:append :yes})`,
		`(spit "` + path + `" "x" {:unknown true})`,
		`(slurp "` + path + `" {:binary :yes})`,
		`(slurp "` + path + `" {:append true})`,
	}
	for _, source := range tests {
		if _, err := runner.Run(source); err == nil {
			t.Errorf("expected strict option error for %s", source)
		}
	}
}
func TestReadStringReadsOnlyFirstFormLikeClojure(t *testing.T) {
	runner := MustNewRunner()

	result, err := runner.Run(`(read-string "(+ 1 2) 3")`)
	if err != nil {
		t.Fatalf("read-string failed: %v", err)
	}
	if got := result.String(); got != "(+ 1 2)" {
		t.Fatalf("unexpected first form: %s", got)
	}
}

func TestReadStringSkipsTopLevelDiscardLikeClojure(t *testing.T) {
	runner := MustNewRunner()

	result, err := runner.Run(`(read-string "#_ 1 2")`)
	if err != nil {
		t.Fatalf("read-string failed: %v", err)
	}
	if got := result.String(); got != "2" {
		t.Fatalf("unexpected first form: %s", got)
	}
}

func TestReadStringNestedDiscardMatchesClojure(t *testing.T) {
	runner := MustNewRunner()

	assertRunErrorContains(t, runner, `(read-string "#_ #_ 1 2")`, "unexpected EOF")
}

func TestSpitRejectsInvalidBinaryValues(t *testing.T) {
	runner := MustNewRunner()
	path := filepath.ToSlash(filepath.Join(t.TempDir(), "invalid.bin"))
	for _, source := range []string{
		`(spit "` + path + `" "bytes" {:binary true})`,
		`(spit "` + path + `" [:not-a-byte] {:binary true})`,
		`(spit "` + path + `" [256] {:binary true})`,
	} {
		if _, err := runner.Run(source); err == nil {
			t.Errorf("expected binary validation error for %s", source)
		}
	}
}

func TestIONamespaceTempAndPathHelpers(t *testing.T) {
	baseDir := t.TempDir()

	runner := MustNewRunner()
	value, err := runner.Run(`
		(require '[doggo.io :as io])
		(let [tmp-file (io/create-temp-file {:prefix "doggo-io-" :suffix ".txt" :dir "` + filepath.ToSlash(baseDir) + `"})
		      tmp-dir (io/create-temp-dir {:prefix "doggo-dir-" :dir "` + filepath.ToSlash(baseDir) + `"})
		      list-dir (io/join-path "` + filepath.ToSlash(baseDir) + `" "listing")
		      file-a (io/join-path list-dir "a.txt")
		      file-b (io/join-path list-dir "b.txt")
		      dir-c (io/join-path list-dir "c")]
		  (io/make-parents file-a)
		  (io/spit file-a "a")
		  (io/spit file-b "b")
		  (io/make-parents (io/join-path dir-c "placeholder.txt"))
		  [(io/exists? tmp-file)
		   (io/file? tmp-file)
		   (io/directory? tmp-file)
		   (io/exists? tmp-dir)
		   (io/directory? tmp-dir)
		   (io/file? tmp-dir)
		   (io/list-entries list-dir)
		   (io/exists? (io/join-path "` + filepath.ToSlash(baseDir) + `" "missing"))])
	`)
	if err != nil {
		t.Fatalf("runner error: %v", err)
	}

	got := value.String()
	want := `[true true false true true false ["` + filepath.Join(baseDir, "listing", "a.txt") + `" "` + filepath.Join(baseDir, "listing", "b.txt") + `" "` + filepath.Join(baseDir, "listing", "c") + `"] false]`
	if got != want {
		t.Fatalf("unexpected io helper result:\nwant %s\ngot  %s", want, got)
	}
}

func TestIOPathHelpers(t *testing.T) {
	baseDir := t.TempDir()
	nestedDir := filepath.Join(baseDir, "nested")
	filePath := filepath.Join(nestedDir, "archive.tar.gz")
	plainPath := filepath.Join(nestedDir, "README")
	rawNormalized := baseDir + string(filepath.Separator) + "nested" + string(filepath.Separator) + ".." + string(filepath.Separator) + "nested" + string(filepath.Separator) + "." + string(filepath.Separator) + "child.txt"
	dirWithTrailingSeparator := filepath.Join(nestedDir, "folder") + string(filepath.Separator)
	relativeAbs, err := filepath.Abs("relative.txt")
	if err != nil {
		t.Fatalf("filepath.Abs failed: %v", err)
	}

	runner := MustNewRunner()
	value, err := runner.Run(`
		(require '[doggo.io :as io])
		(let [base "` + filepath.ToSlash(nestedDir) + `"
		      file "` + filepath.ToSlash(filePath) + `"
		      plain "` + filepath.ToSlash(plainPath) + `"
		      raw "` + filepath.ToSlash(rawNormalized) + `"
		      dir-with-slash "` + filepath.ToSlash(dirWithTrailingSeparator) + `"]
		  [(io/basename file)
		   (io/basename dir-with-slash)
		   (io/dirname file)
		   (io/extension file)
		   (io/extension plain)
		   (io/normalize raw)
		   (io/absolute? file)
		   (io/absolute? "relative.txt")
		   (io/absolute "relative.txt")
		   (io/relative-to base (io/join-path base "deep" "child.txt"))
		   (io/relative-to base base)])
	`)
	if err != nil {
		t.Fatalf("runner error: %v", err)
	}

	want := `["archive.tar.gz" "folder" "` + nestedDir + `" "gz" nil "` + filepath.Join(nestedDir, "child.txt") + `" true false "` + relativeAbs + `" "` + filepath.Join("deep", "child.txt") + `" "."]`
	if got := value.String(); got != want {
		t.Fatalf("unexpected io path helper result:\nwant %s\ngot  %s", want, got)
	}

	tests := []struct {
		input       string
		wantMessage string
	}{
		{`(require '[doggo.io :as io]) (io/basename :bad)`, `doggo.io/basename: path must be a string`},
		{`(require '[doggo.io :as io]) (io/dirname :bad)`, `doggo.io/dirname: path must be a string`},
		{`(require '[doggo.io :as io]) (io/extension :bad)`, `doggo.io/extension: path must be a string`},
		{`(require '[doggo.io :as io]) (io/normalize :bad)`, `doggo.io/normalize: path must be a string`},
		{`(require '[doggo.io :as io]) (io/absolute? :bad)`, `doggo.io/absolute?: path must be a string`},
		{`(require '[doggo.io :as io]) (io/absolute :bad)`, `doggo.io/absolute: path must be a string`},
		{`(require '[doggo.io :as io]) (io/relative-to :bad "x")`, `doggo.io/relative-to: base must be a string`},
		{fmt.Sprintf(`(require '[doggo.io :as io]) (io/relative-to %q "relative.txt")`, baseDir), `base and path must both be absolute or both be relative`},
	}
	for _, tt := range tests {
		if _, err := runner.Run(tt.input); err == nil {
			t.Fatalf("expected error for %s", tt.input)
		} else if !strings.Contains(err.Error(), tt.wantMessage) {
			t.Fatalf("expected error containing %q for %s, got %v", tt.wantMessage, tt.input, err)
		}
	}

	if runtime.GOOS == "windows" {
		if _, err := runner.Run(`(require '[doggo.io :as io]) (io/relative-to "C:\\tmp" "D:\\tmp\\child.txt")`); err == nil {
			t.Fatal("expected cross-volume relative-to to fail on windows")
		}
	}
}

func TestIONamespaceFileSeqWalksDepthFirst(t *testing.T) {
	baseDir := t.TempDir()

	runner := MustNewRunner()
	value, err := runner.Run(`
		(require '[doggo.io :as io])
		(let [root (io/join-path "` + filepath.ToSlash(baseDir) + `" "tree")
		      child-a (io/join-path root "a.txt")
		      child-dir (io/join-path root "sub")
		      child-b (io/join-path child-dir "b.txt")
		      missing (io/join-path root "missing")]
		  (io/make-parents child-b)
		  (io/spit child-a "a")
		  (io/spit child-b "b")
		  [(vec (io/file-seq root))
		   (vec (io/file-seq child-a))
		   (vec (io/file-seq missing))])
	`)
	if err != nil {
		t.Fatalf("runner error: %v", err)
	}

	want := `[["` + filepath.Join(baseDir, "tree") + `" "` + filepath.Join(baseDir, "tree", "a.txt") + `" "` + filepath.Join(baseDir, "tree", "sub") + `" "` + filepath.Join(baseDir, "tree", "sub", "b.txt") + `"] ["` + filepath.Join(baseDir, "tree", "a.txt") + `"] ["` + filepath.Join(baseDir, "tree", "missing") + `"]]`
	if got := value.String(); got != want {
		t.Fatalf("unexpected io/file-seq result:\nwant %s\ngot  %s", want, got)
	}
}

func symlinkOrSkip(t *testing.T, oldname, newname string) {
	t.Helper()
	if err := os.Symlink(oldname, newname); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
}

func TestIOWalkSharesSequenceContract(t *testing.T) {
	root := t.TempDir()
	runner := MustNewRunner()
	result, err := runner.Run(fmt.Sprintf(`
		(require '[doggo.io :as io])
		(let [walk (io/walk %q {:max-depth 0})
		      paths (list %q)]
		  [(= walk paths) (= paths walk)
		   (get (hash-map walk :walk) paths :missing)
		   (get (hash-map paths :list) walk :missing)])`, root, root))
	if err != nil {
		t.Fatalf("runner error: %v", err)
	}
	if got := result.String(); got != "[true true :walk :list]" {
		t.Fatalf("Run() = %s, want io/walk sequence contract", got)
	}
}

func TestIOWalkAndGlob(t *testing.T) {
	t.Run("walk and glob surfaces", func(t *testing.T) {
		baseDir := t.TempDir()
		root := filepath.Join(baseDir, "tree")
		childA := filepath.Join(root, "a.txt")
		emptyDir := filepath.Join(root, "empty")
		subDir := filepath.Join(root, "sub")
		childB := filepath.Join(subDir, "b.txt")
		childC := filepath.Join(subDir, "c.log")
		rawPattern := filepath.ToSlash(root) + "/sub/../*.txt"

		for _, path := range []string{childA, childB, childC} {
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatalf("mkdir %s: %v", path, err)
			}
			if err := os.WriteFile(path, []byte(filepath.Base(path)), 0o644); err != nil {
				t.Fatalf("write %s: %v", path, err)
			}
		}
		if err := os.MkdirAll(emptyDir, 0o755); err != nil {
			t.Fatalf("mkdir %s: %v", emptyDir, err)
		}

		runner := MustNewRunner()
		value, err := runner.Run(`
			(require '[doggo.io :as io])
			(let [root "` + filepath.ToSlash(root) + `"
			      raw-pattern "` + rawPattern + `"]
			  [(vec (io/walk root))
			   (vec (io/walk root {:order :post}))
			   (vec (io/walk root {:type :files}))
			   (vec (io/walk root {:type :dirs}))
			   (vec (io/walk root {:include-root false}))
			   (vec (io/walk root {:max-depth 1}))
			   (io/glob (io/join-path root "*.txt"))
			   (io/glob raw-pattern)
			   (io/glob (io/join-path root "sub" "*"))
			   (io/glob (io/join-path root "*.missing"))])
		`)
		if err != nil {
			t.Fatalf("runner error: %v", err)
		}

		want := `[["` + root + `" "` + childA + `" "` + emptyDir + `" "` + subDir + `" "` + childB + `" "` + childC + `"] [` +
			`"` + childA + `" "` + emptyDir + `" "` + childB + `" "` + childC + `" "` + subDir + `" "` + root + `"] [` +
			`"` + childA + `" "` + childB + `" "` + childC + `"] [` +
			`"` + root + `" "` + emptyDir + `" "` + subDir + `"] [` +
			`"` + childA + `" "` + emptyDir + `" "` + subDir + `" "` + childB + `" "` + childC + `"] [` +
			`"` + root + `" "` + childA + `" "` + emptyDir + `" "` + subDir + `"] [` +
			`"` + childA + `"] ["` + childA + `"] ["` + childB + `" "` + childC + `"] []]`
		if got := value.String(); got != want {
			t.Fatalf("unexpected io/walk or io/glob result:\nwant %s\ngot  %s", want, got)
		}
	})

	t.Run("walk follows symlinked directories only when requested", func(t *testing.T) {
		baseDir := t.TempDir()
		root := filepath.Join(baseDir, "tree")
		realDir := filepath.Join(root, "real")
		child := filepath.Join(realDir, "child.txt")
		link := filepath.Join(root, "link")
		linkChild := filepath.Join(link, "child.txt")

		if err := os.MkdirAll(realDir, 0o755); err != nil {
			t.Fatalf("mkdir real dir: %v", err)
		}
		if err := os.WriteFile(child, []byte("a"), 0o644); err != nil {
			t.Fatalf("write child: %v", err)
		}
		symlinkOrSkip(t, realDir, link)

		runner := MustNewRunner()
		value, err := runner.Run(`
			(require '[doggo.io :as io])
			[(vec (io/walk "` + filepath.ToSlash(root) + `"))
			 (vec (io/walk "` + filepath.ToSlash(root) + `" {:follow-symlinks true}))
			 (vec (io/walk "` + filepath.ToSlash(root) + `" {:follow-symlinks true :type :dirs}))
			 (vec (io/walk "` + filepath.ToSlash(root) + `" {:follow-symlinks true :type :files}))]
		`)
		if err != nil {
			t.Fatalf("runner error: %v", err)
		}

		want := `[["` + root + `" "` + link + `" "` + realDir + `" "` + child + `"] [` +
			`"` + root + `" "` + link + `" "` + linkChild + `" "` + realDir + `" "` + child + `"] [` +
			`"` + root + `" "` + link + `" "` + realDir + `"] [` +
			`"` + linkChild + `" "` + child + `"]]`
		if got := value.String(); got != want {
			t.Fatalf("unexpected symlink walk result:\nwant %s\ngot  %s", want, got)
		}
	})

	t.Run("walk follow-symlinks stops directory cycles", func(t *testing.T) {
		baseDir := t.TempDir()
		root := filepath.Join(baseDir, "tree")
		realDir := filepath.Join(root, "real")
		child := filepath.Join(realDir, "a.txt")
		loop := filepath.Join(realDir, "loop")

		if err := os.MkdirAll(realDir, 0o755); err != nil {
			t.Fatalf("mkdir real dir: %v", err)
		}
		if err := os.WriteFile(child, []byte("a"), 0o644); err != nil {
			t.Fatalf("write child: %v", err)
		}
		symlinkOrSkip(t, root, loop)

		runner := MustNewRunner()
		value, err := runner.Run(`
			(require '[doggo.io :as io])
			(vec (io/walk "` + filepath.ToSlash(root) + `" {:follow-symlinks true :max-depth 6}))
		`)
		if err != nil {
			t.Fatalf("runner error: %v", err)
		}

		want := `["` + root + `" "` + realDir + `" "` + child + `" "` + loop + `"]`
		if got := value.String(); got != want {
			t.Fatalf("unexpected cycle-safe symlink walk result:\nwant %s\ngot  %s", want, got)
		}
	})

	t.Run("walk follow-symlinks tolerates broken symlinks", func(t *testing.T) {
		baseDir := t.TempDir()
		root := filepath.Join(baseDir, "tree")
		broken := filepath.Join(root, "broken")
		child := filepath.Join(root, "file.txt")

		if err := os.MkdirAll(root, 0o755); err != nil {
			t.Fatalf("mkdir root: %v", err)
		}
		if err := os.WriteFile(child, []byte("a"), 0o644); err != nil {
			t.Fatalf("write child: %v", err)
		}
		symlinkOrSkip(t, filepath.Join(root, "missing"), broken)

		runner := MustNewRunner()
		value, err := runner.Run(`
			(require '[doggo.io :as io])
			[(vec (io/walk "` + filepath.ToSlash(root) + `" {:follow-symlinks true}))
			 (vec (io/walk "` + filepath.ToSlash(root) + `" {:follow-symlinks true :type :dirs}))
			 (vec (io/walk "` + filepath.ToSlash(root) + `" {:follow-symlinks true :type :files}))]
		`)
		if err != nil {
			t.Fatalf("runner error: %v", err)
		}

		want := `[["` + root + `" "` + broken + `" "` + child + `"] [` +
			`"` + root + `"] [` +
			`"` + broken + `" "` + child + `"]]`
		if got := value.String(); got != want {
			t.Fatalf("unexpected broken symlink walk result:\nwant %s\ngot  %s", want, got)
		}
	})

	t.Run("walk follow-symlinks tolerates symlinks through non-directories", func(t *testing.T) {
		baseDir := t.TempDir()
		root := filepath.Join(baseDir, "tree")
		link := filepath.Join(root, "link")
		targetFile := filepath.Join(root, "target-file")

		if err := os.MkdirAll(root, 0o755); err != nil {
			t.Fatalf("mkdir root: %v", err)
		}
		if err := os.WriteFile(targetFile, []byte("a"), 0o644); err != nil {
			t.Fatalf("write target file: %v", err)
		}
		symlinkOrSkip(t, filepath.Join(targetFile, "child"), link)

		runner := MustNewRunner()
		value, err := runner.Run(`
			(require '[doggo.io :as io])
			[(vec (io/walk "` + filepath.ToSlash(root) + `" {:follow-symlinks true}))
			 (vec (io/walk "` + filepath.ToSlash(root) + `" {:follow-symlinks true :type :dirs}))
			 (vec (io/walk "` + filepath.ToSlash(root) + `" {:follow-symlinks true :type :files}))
			 (vec (io/walk "` + filepath.ToSlash(root) + `" {:follow-symlinks true :max-depth 1}))]
		`)
		if err != nil {
			t.Fatalf("runner error: %v", err)
		}

		want := `[["` + root + `" "` + link + `" "` + targetFile + `"] [` +
			`"` + root + `"] [` +
			`"` + link + `" "` + targetFile + `"] [` +
			`"` + root + `" "` + link + `" "` + targetFile + `"]]`
		if got := value.String(); got != want {
			t.Fatalf("unexpected non-directory symlink walk result:\nwant %s\ngot  %s", want, got)
		}
	})

	t.Run("walk follow-symlinks tolerates self-referential symlink loops", func(t *testing.T) {
		baseDir := t.TempDir()
		root := filepath.Join(baseDir, "tree")
		loop := filepath.Join(root, "loop")
		child := filepath.Join(root, "file.txt")

		if err := os.MkdirAll(root, 0o755); err != nil {
			t.Fatalf("mkdir root: %v", err)
		}
		if err := os.WriteFile(child, []byte("a"), 0o644); err != nil {
			t.Fatalf("write child: %v", err)
		}
		symlinkOrSkip(t, loop, loop)

		runner := MustNewRunner()
		value, err := runner.Run(`
			(require '[doggo.io :as io])
			[(vec (io/walk "` + filepath.ToSlash(root) + `" {:follow-symlinks true}))
			 (vec (io/walk "` + filepath.ToSlash(root) + `" {:follow-symlinks true :type :dirs}))
			 (vec (io/walk "` + filepath.ToSlash(root) + `" {:follow-symlinks true :type :files}))]
		`)
		if err != nil {
			t.Fatalf("runner error: %v", err)
		}

		want := `[["` + root + `" "` + child + `" "` + loop + `"] [` +
			`"` + root + `"] [` +
			`"` + child + `" "` + loop + `"]]`
		if got := value.String(); got != want {
			t.Fatalf("unexpected self-referential symlink walk result:\nwant %s\ngot  %s", want, got)
		}
	})

	t.Run("walk follow-symlinks still fails on inaccessible symlink targets", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("unix permission semantics required")
		}

		baseDir := t.TempDir()
		root := filepath.Join(baseDir, "tree")
		blockedParent := filepath.Join(baseDir, "blocked")
		target := filepath.Join(blockedParent, "target")
		link := filepath.Join(root, "link")

		if err := os.MkdirAll(root, 0o755); err != nil {
			t.Fatalf("mkdir root: %v", err)
		}
		if err := os.MkdirAll(target, 0o755); err != nil {
			t.Fatalf("mkdir target: %v", err)
		}
		symlinkOrSkip(t, target, link)
		if err := os.Chmod(blockedParent, 0); err != nil {
			t.Fatalf("chmod blocked parent: %v", err)
		}
		t.Cleanup(func() {
			_ = os.Chmod(blockedParent, 0o755)
		})
		if _, err := os.Stat(link); err == nil {
			t.Skip("current environment can still stat target through chmod 000 parent")
		}

		runner := MustNewRunner()
		inputs := []string{
			`(require '[doggo.io :as io]) (vec (io/walk "` + filepath.ToSlash(root) + `" {:follow-symlinks true}))`,
			`(require '[doggo.io :as io]) (vec (io/walk "` + filepath.ToSlash(root) + `" {:follow-symlinks true :max-depth 1}))`,
		}
		for _, input := range inputs {
			if _, err := runner.Run(input); err == nil {
				t.Fatalf("expected walk to fail on inaccessible symlink target for %s", input)
			}
		}
	})

	t.Run("walk and glob reject invalid input", func(t *testing.T) {
		runner := MustNewRunner()
		tests := []struct {
			input       string
			wantMessage string
		}{
			{`(require '[doggo.io :as io]) (io/walk :bad)`, `doggo.io/walk: path must be a string`},
			{`(require '[doggo.io :as io]) (io/walk "/tmp" {:order :sideways})`, `doggo.io/walk: option :order must be :pre or :post`},
			{`(require '[doggo.io :as io]) (io/walk "/tmp" {:type :weird})`, `doggo.io/walk: option :type must be :all, :files, or :dirs`},
			{`(require '[doggo.io :as io]) (io/walk "/tmp" {:include-root :yes})`, `doggo.io/walk: option :include-root must be boolean`},
			{`(require '[doggo.io :as io]) (io/walk "/tmp" {:max-depth "x"})`, `doggo.io/walk: option :max-depth must be a non-negative integer or nil`},
			{`(require '[doggo.io :as io]) (io/walk "/tmp" {:follow-symlinks :yes})`, `doggo.io/walk: option :follow-symlinks must be boolean`},
			{`(require '[doggo.io :as io]) (io/glob :bad)`, `doggo.io/glob: pattern must be a string`},
			{`(require '[doggo.io :as io]) (io/glob "[")`, `io-glob*:`},
		}
		for _, tt := range tests {
			if _, err := runner.Run(tt.input); err == nil {
				t.Fatalf("expected error for %s", tt.input)
			} else if !strings.Contains(err.Error(), tt.wantMessage) {
				t.Fatalf("expected error containing %q for %s, got %v", tt.wantMessage, tt.input, err)
			}
		}
	})

	t.Run("walk fails fast on unreadable roots and when forced into unreadable children", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("unix permission semantics required")
		}

		baseDir := t.TempDir()
		root := filepath.Join(baseDir, "root")
		childFile := filepath.Join(root, "a.txt")
		blockedChild := filepath.Join(root, "blocked")
		blockedGrandchild := filepath.Join(blockedChild, "hidden.txt")
		blockedRoot := filepath.Join(baseDir, "blocked-root")
		blockedRootChild := filepath.Join(blockedRoot, "hidden.txt")

		for _, dir := range []string{blockedChild, blockedRoot} {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatalf("mkdir %s: %v", dir, err)
			}
		}
		for _, file := range []string{childFile, blockedGrandchild, blockedRootChild} {
			if err := os.MkdirAll(filepath.Dir(file), 0o755); err != nil {
				t.Fatalf("mkdir parent %s: %v", file, err)
			}
			if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
				t.Fatalf("write %s: %v", file, err)
			}
		}
		if err := os.Chmod(blockedChild, 0); err != nil {
			t.Fatalf("chmod blocked child: %v", err)
		}
		if err := os.Chmod(blockedRoot, 0); err != nil {
			_ = os.Chmod(blockedChild, 0o755)
			t.Fatalf("chmod blocked root: %v", err)
		}
		t.Cleanup(func() {
			_ = os.Chmod(blockedChild, 0o755)
			_ = os.Chmod(blockedRoot, 0o755)
		})
		if _, err := os.ReadDir(blockedChild); err == nil {
			t.Skip("current environment can still read chmod 000 child directory")
		}
		if _, err := os.ReadDir(blockedRoot); err == nil {
			t.Skip("current environment can still read chmod 000 root directory")
		}

		runner := MustNewRunner()

		prefixValue, err := runner.Run(`(require '[doggo.io :as io]) (vec (take 2 (io/walk "` + filepath.ToSlash(root) + `")))`)
		if err != nil {
			t.Fatalf("expected prefix walk to succeed before unreadable child, got %v", err)
		}
		wantPrefix := `["` + root + `" "` + childFile + `"]`
		if got := prefixValue.String(); got != wantPrefix {
			t.Fatalf("unexpected prefix walk result:\nwant %s\ngot  %s", wantPrefix, got)
		}

		if _, err := runner.Run(`(require '[doggo.io :as io]) (vec (io/walk "` + filepath.ToSlash(root) + `"))`); err == nil {
			t.Fatalf("expected full walk to fail once unreadable child is forced")
		}
		if _, err := runner.Run(`(require '[doggo.io :as io]) (vec (io/walk "` + filepath.ToSlash(blockedRoot) + `"))`); err == nil {
			t.Fatalf("expected blocked root walk to fail immediately")
		}
	})
}

func TestIONamespaceFileSeqIncludesUnreadableDirectoriesWithoutDescending(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("unix permission semantics required")
	}

	baseDir := t.TempDir()
	readableRoot := filepath.Join(baseDir, "readable")
	readableFile := filepath.Join(readableRoot, "a.txt")
	unreadableChild := filepath.Join(readableRoot, "secret")
	hiddenChild := filepath.Join(unreadableChild, "hidden.txt")
	unreadableRoot := filepath.Join(baseDir, "blocked")
	unreadableRootChild := filepath.Join(unreadableRoot, "hidden.txt")

	if err := os.MkdirAll(unreadableChild, 0o755); err != nil {
		t.Fatalf("mkdir unreadable child: %v", err)
	}
	if err := os.MkdirAll(unreadableRoot, 0o755); err != nil {
		t.Fatalf("mkdir unreadable root: %v", err)
	}
	if err := os.WriteFile(readableFile, []byte("a"), 0o644); err != nil {
		t.Fatalf("write readable file: %v", err)
	}
	if err := os.WriteFile(hiddenChild, []byte("hidden"), 0o644); err != nil {
		t.Fatalf("write hidden child: %v", err)
	}
	if err := os.WriteFile(unreadableRootChild, []byte("hidden"), 0o644); err != nil {
		t.Fatalf("write unreadable root child: %v", err)
	}
	if err := os.Chmod(unreadableChild, 0); err != nil {
		t.Fatalf("chmod unreadable child: %v", err)
	}
	if err := os.Chmod(unreadableRoot, 0); err != nil {
		_ = os.Chmod(unreadableChild, 0o755)
		t.Fatalf("chmod unreadable root: %v", err)
	}
	t.Cleanup(func() {
		_ = os.Chmod(unreadableChild, 0o755)
		_ = os.Chmod(unreadableRoot, 0o755)
	})
	if _, err := os.ReadDir(unreadableChild); err == nil {
		t.Skip("current environment can still read chmod 000 child directory")
	}
	if _, err := os.ReadDir(unreadableRoot); err == nil {
		t.Skip("current environment can still read chmod 000 root directory")
	}

	runner := MustNewRunner()
	value, err := runner.Run(`
		(require '[doggo.io :as io])
		(let [readable-root "` + filepath.ToSlash(readableRoot) + `"
		      unreadable-root "` + filepath.ToSlash(unreadableRoot) + `"]
		  [(vec (io/file-seq readable-root))
		   (vec (io/file-seq unreadable-root))])
	`)
	if err != nil {
		t.Fatalf("runner error: %v", err)
	}

	want := `[["` + readableRoot + `" "` + readableFile + `" "` + unreadableChild + `"] ["` + unreadableRoot + `"]]`
	if got := value.String(); got != want {
		t.Fatalf("unexpected io/file-seq unreadable result:\nwant %s\ngot  %s", want, got)
	}
}

func TestIONamespaceTempAndPathHelpersRejectInvalidInput(t *testing.T) {
	runner := MustNewRunner()
	tests := []struct {
		input       string
		wantMessage string
	}{
		{`(require '[doggo.io :as io]) (io/exists? :bad)`, `doggo.io/exists?: path must be a string`},
		{`(require '[doggo.io :as io]) (io/list-entries :bad)`, `doggo.io/list-entries: path must be a string`},
		{`(require '[doggo.io :as io]) (io/file-seq :bad)`, `doggo.io/file-seq: path must be a string`},
		{`(require '[doggo.io :as io]) (io/create-temp-file {:prefix :bad})`, `doggo.io/create-temp-file: prefix must be a string`},
		{`(require '[doggo.io :as io]) (io/create-temp-dir {:dir :bad})`, `doggo.io/create-temp-dir: dir must be a string`},
	}
	for _, tt := range tests {
		if _, err := runner.Run(tt.input); err == nil {
			t.Fatalf("expected error for %s", tt.input)
		} else if !strings.Contains(err.Error(), tt.wantMessage) {
			t.Fatalf("expected error containing %q for %s, got %v", tt.wantMessage, tt.input, err)
		}
	}
}

func TestIONamespacePreservesLiteralBackslashesOnUnix(t *testing.T) {
	if filepath.Separator == '\\' {
		t.Skip("Windows uses backslash as a path separator")
	}

	baseDir := t.TempDir()

	runner := MustNewRunner()
	value, err := runner.Run(`
		(require '[doggo.io :as io])
		(require '[doggo.string :as str])
		(let [literal-dir (io/join-path "` + filepath.ToSlash(baseDir) + `" "listing\\literal")
		      file-path (io/join-path literal-dir "child.txt")
		      _ (io/make-parents file-path)
		      _ (io/spit file-path "x")
		      listed (io/list-entries literal-dir)
		      tmp-file (io/create-temp-file {:prefix "doggo-io-" :dir literal-dir})
		      tmp-dir (io/create-temp-dir {:prefix "doggo-dir-" :dir literal-dir})]
		  [(str/includes? literal-dir "\\")
		   (str/includes? tmp-file "\\")
		   (str/includes? tmp-dir "\\")
		   (= [file-path] listed)
		   (io/exists? file-path)
		   (io/exists? tmp-file)
		   (io/exists? tmp-dir)])
	`)
	if err != nil {
		t.Fatalf("runner error: %v", err)
	}

	if got := value.String(); got != `[true true true true true true true]` {
		t.Fatalf("expected literal backslashes to survive io helpers, got %s", got)
	}
}

func TestIOTreeMutation(t *testing.T) {
	t.Run("mkdirs is idempotent", func(t *testing.T) {
		baseDir := t.TempDir()
		target := filepath.Join(baseDir, "a", "b", "c")

		runner := MustNewRunner()
		value, err := runner.Run(`
			(require '[doggo.io :as io])
			(let [target "` + filepath.ToSlash(target) + `"]
			  [(io/mkdirs target)
			   (io/mkdirs target)])
		`)
		if err != nil {
			t.Fatalf("runner error: %v", err)
		}

		want := `["` + target + `" "` + target + `"]`
		if got := value.String(); got != want {
			t.Fatalf("unexpected mkdirs result:\nwant %s\ngot  %s", want, got)
		}
		if info, err := os.Stat(target); err != nil || !info.IsDir() {
			t.Fatalf("expected mkdirs target to exist as directory, info=%v err=%v", info, err)
		}
	})

	t.Run("delete-tree handles files directories and missing policy", func(t *testing.T) {
		baseDir := t.TempDir()
		fileTarget := filepath.Join(baseDir, "file.txt")
		dirTarget := filepath.Join(baseDir, "tree")
		dirChild := filepath.Join(dirTarget, "sub", "child.txt")
		missingTarget := filepath.Join(baseDir, "missing")

		if err := os.WriteFile(fileTarget, []byte("x"), 0o644); err != nil {
			t.Fatalf("write file target: %v", err)
		}
		if err := os.MkdirAll(filepath.Dir(dirChild), 0o755); err != nil {
			t.Fatalf("mkdir dir target: %v", err)
		}
		if err := os.WriteFile(dirChild, []byte("x"), 0o644); err != nil {
			t.Fatalf("write dir child: %v", err)
		}

		runner := MustNewRunner()
		value, err := runner.Run(`
			(require '[doggo.io :as io])
			(let [file-target "` + filepath.ToSlash(fileTarget) + `"
			      dir-target "` + filepath.ToSlash(dirTarget) + `"
			      missing-target "` + filepath.ToSlash(missingTarget) + `"]
			  [(nil? (io/delete-tree file-target))
			   (nil? (io/delete-tree dir-target))
			   (nil? (io/delete-tree missing-target {:missing :ignore}))
			   (try
			     (io/delete-tree missing-target)
			     :unexpected
			     (catch [e] :error))])
		`)
		if err != nil {
			t.Fatalf("runner error: %v", err)
		}

		want := `[true true true :error]`
		if got := value.String(); got != want {
			t.Fatalf("unexpected delete-tree result:\nwant %s\ngot  %s", want, got)
		}
		if _, err := os.Stat(fileTarget); !os.IsNotExist(err) {
			t.Fatalf("expected file target to be removed, got err=%v", err)
		}
		if _, err := os.Stat(dirTarget); !os.IsNotExist(err) {
			t.Fatalf("expected dir target to be removed, got err=%v", err)
		}
	})

	t.Run("copy-tree handles destination shape policy", func(t *testing.T) {
		baseDir := t.TempDir()
		fileSrc := filepath.Join(baseDir, "file-src.txt")
		fileDst := filepath.Join(baseDir, "file-dst.txt")
		fileDirDst := filepath.Join(baseDir, "file-dst-dir")
		fileDirCollisionRoot := filepath.Join(baseDir, "file-dst-dir-collision")
		fileDirCollision := filepath.Join(fileDirCollisionRoot, filepath.Base(fileSrc))
		dirSrc := filepath.Join(baseDir, "dir-src")
		dirSrcA := filepath.Join(dirSrc, "a.txt")
		dirSrcB := filepath.Join(dirSrc, "sub", "b.txt")
		dirDstMissing := filepath.Join(baseDir, "dir-dst-missing")
		dirDstMerge := filepath.Join(baseDir, "dir-dst-merge")
		dirDstMergeKeep := filepath.Join(dirDstMerge, "keep.txt")
		dirDstMergeCollide := filepath.Join(dirDstMerge, "a.txt")
		dirDstFile := filepath.Join(baseDir, "dir-dst-file")

		for _, file := range []struct {
			path string
			body string
		}{
			{fileSrc, "src-file"},
			{fileDst, "dst-file"},
			{dirSrcA, "dir-a"},
			{dirSrcB, "dir-b"},
			{dirDstMergeKeep, "keep"},
			{dirDstMergeCollide, "old-a"},
			{dirDstFile, "target-file"},
		} {
			if err := os.MkdirAll(filepath.Dir(file.path), 0o755); err != nil {
				t.Fatalf("mkdir %s: %v", file.path, err)
			}
			if err := os.WriteFile(file.path, []byte(file.body), 0o644); err != nil {
				t.Fatalf("write %s: %v", file.path, err)
			}
		}
		if err := os.MkdirAll(fileDirDst, 0o755); err != nil {
			t.Fatalf("mkdir file dst dir: %v", err)
		}
		if err := os.MkdirAll(fileDirCollision, 0o755); err != nil {
			t.Fatalf("mkdir file dir collision: %v", err)
		}

		runner := MustNewRunner()
		value, err := runner.Run(`
			(require '[doggo.io :as io])
			(let [file-src "` + filepath.ToSlash(fileSrc) + `"
			      file-dst "` + filepath.ToSlash(fileDst) + `"
			      file-dst-dir "` + filepath.ToSlash(fileDirDst) + `"
			      file-dst-dir-collision "` + filepath.ToSlash(fileDirCollisionRoot) + `"
			      dir-src "` + filepath.ToSlash(dirSrc) + `"
			      dir-dst-missing "` + filepath.ToSlash(dirDstMissing) + `"
			      dir-dst-merge "` + filepath.ToSlash(dirDstMerge) + `"
			      dir-dst-file "` + filepath.ToSlash(dirDstFile) + `"]
			  [(try
			     (io/copy-tree file-src file-dst)
			     :unexpected
			     (catch [e] :overwrite-error))
			   (nil? (io/copy-tree file-src file-dst {:overwrite true}))
			   (nil? (io/copy-tree file-src file-dst-dir))
			   (try
			     (io/copy-tree file-src file-dst-dir-collision)
			     :unexpected
			     (catch [e] :dir-collision))
			   (nil? (io/copy-tree dir-src dir-dst-missing))
			   (try
			     (io/copy-tree dir-src dir-dst-merge)
			     :unexpected
			     (catch [e] :merge-collision))
			   (nil? (io/copy-tree dir-src dir-dst-merge {:overwrite true}))
			   (try
			     (io/copy-tree dir-src dir-dst-file)
			     :unexpected
			     (catch [e] :dir-into-file))
			   (nil? (io/copy-tree "` + filepath.ToSlash(filepath.Join(baseDir, "missing-src")) + `" "` + filepath.ToSlash(filepath.Join(baseDir, "ignored")) + `" {:missing :ignore}))])
		`)
		if err != nil {
			t.Fatalf("runner error: %v", err)
		}

		want := `[:overwrite-error true true :dir-collision true :merge-collision true :dir-into-file true]`
		if got := value.String(); got != want {
			t.Fatalf("unexpected copy-tree result:\nwant %s\ngot  %s", want, got)
		}

		if body, err := os.ReadFile(fileDst); err != nil || string(body) != "src-file" {
			t.Fatalf("expected file overwrite copy, body=%q err=%v", string(body), err)
		}
		if body, err := os.ReadFile(filepath.Join(fileDirDst, filepath.Base(fileSrc))); err != nil || string(body) != "src-file" {
			t.Fatalf("expected file copied into existing directory, body=%q err=%v", string(body), err)
		}
		if body, err := os.ReadFile(filepath.Join(dirDstMissing, "a.txt")); err != nil || string(body) != "dir-a" {
			t.Fatalf("expected dir copied into missing destination, body=%q err=%v", string(body), err)
		}
		if body, err := os.ReadFile(filepath.Join(dirDstMissing, "sub", "b.txt")); err != nil || string(body) != "dir-b" {
			t.Fatalf("expected nested dir copied into missing destination, body=%q err=%v", string(body), err)
		}
		if body, err := os.ReadFile(dirDstMergeKeep); err != nil || string(body) != "keep" {
			t.Fatalf("expected merge to keep existing non-colliding file, body=%q err=%v", string(body), err)
		}
		if body, err := os.ReadFile(dirDstMergeCollide); err != nil || string(body) != "dir-a" {
			t.Fatalf("expected merge overwrite to replace colliding file, body=%q err=%v", string(body), err)
		}
	})

	t.Run("move mirrors collision and overwrite semantics", func(t *testing.T) {
		baseDir := t.TempDir()
		fileSrc := filepath.Join(baseDir, "move-src.txt")
		fileDst := filepath.Join(baseDir, "move-dst.txt")
		fileDirDst := filepath.Join(baseDir, "move-dst-dir")
		dirSrc := filepath.Join(baseDir, "move-dir-src")
		dirSrcA := filepath.Join(dirSrc, "a.txt")
		dirDst := filepath.Join(baseDir, "move-dir-dst")
		dirDstKeep := filepath.Join(dirDst, "keep.txt")
		dirDstCollide := filepath.Join(dirDst, "a.txt")

		for _, file := range []struct {
			path string
			body string
		}{
			{fileSrc, "move-file"},
			{fileDst, "old-dst"},
			{dirSrcA, "move-a"},
			{dirDstKeep, "keep"},
			{dirDstCollide, "old-a"},
		} {
			if err := os.MkdirAll(filepath.Dir(file.path), 0o755); err != nil {
				t.Fatalf("mkdir %s: %v", file.path, err)
			}
			if err := os.WriteFile(file.path, []byte(file.body), 0o644); err != nil {
				t.Fatalf("write %s: %v", file.path, err)
			}
		}
		if err := os.MkdirAll(fileDirDst, 0o755); err != nil {
			t.Fatalf("mkdir move dst dir: %v", err)
		}

		runner := MustNewRunner()
		value, err := runner.Run(`
			(require '[doggo.io :as io])
			(let [file-src "` + filepath.ToSlash(fileSrc) + `"
			      file-dst "` + filepath.ToSlash(fileDst) + `"
			      file-dst-dir "` + filepath.ToSlash(fileDirDst) + `"
			      dir-src "` + filepath.ToSlash(dirSrc) + `"
			      dir-dst "` + filepath.ToSlash(dirDst) + `"]
			  [(try
			     (io/move file-src file-dst)
			     :unexpected
			     (catch [e] :overwrite-error))
			   (nil? (io/move file-src file-dst {:overwrite true}))
			   (nil? (io/move file-dst file-dst-dir))
			   (try
			     (io/move dir-src dir-dst)
			     :unexpected
			     (catch [e] :merge-collision))
			   (nil? (io/move dir-src dir-dst {:overwrite true}))
			   (nil? (io/move "` + filepath.ToSlash(filepath.Join(baseDir, "missing-move")) + `" "` + filepath.ToSlash(filepath.Join(baseDir, "ignored")) + `" {:missing :ignore}))])
		`)
		if err != nil {
			t.Fatalf("runner error: %v", err)
		}

		want := `[:overwrite-error true true :merge-collision true true]`
		if got := value.String(); got != want {
			t.Fatalf("unexpected move result:\nwant %s\ngot  %s", want, got)
		}

		movedIntoDir := filepath.Join(fileDirDst, filepath.Base(fileDst))
		if body, err := os.ReadFile(movedIntoDir); err != nil || string(body) != "move-file" {
			t.Fatalf("expected file moved into existing directory, body=%q err=%v", string(body), err)
		}
		if _, err := os.Stat(fileDst); !os.IsNotExist(err) {
			t.Fatalf("expected moved file source to disappear, got err=%v", err)
		}
		if body, err := os.ReadFile(filepath.Join(dirDst, "a.txt")); err != nil || string(body) != "move-a" {
			t.Fatalf("expected directory move overwrite to replace colliding file, body=%q err=%v", string(body), err)
		}
		if _, err := os.Stat(dirSrc); !os.IsNotExist(err) {
			t.Fatalf("expected moved directory source to disappear, got err=%v", err)
		}
	})

	t.Run("tree mutation rejects invalid options", func(t *testing.T) {
		runner := MustNewRunner()
		tests := []struct {
			input       string
			wantMessage string
		}{
			{`(require '[doggo.io :as io]) (io/mkdirs :bad)`, `doggo.io/mkdirs: path must be a string`},
			{`(require '[doggo.io :as io]) (io/delete-tree "/tmp" {:missing :weird})`, `doggo.io/delete-tree: option :missing must be :error or :ignore`},
			{`(require '[doggo.io :as io]) (io/copy-tree "/tmp/a" "/tmp/b" {:overwrite :yes})`, `doggo.io/copy-tree: option :overwrite must be boolean`},
			{`(require '[doggo.io :as io]) (io/copy-tree "/tmp/a" "/tmp/b" {:missing :weird})`, `doggo.io/copy-tree: option :missing must be :error or :ignore`},
			{`(require '[doggo.io :as io]) (io/move "/tmp/a" "/tmp/b" {:overwrite :yes})`, `doggo.io/move: option :overwrite must be boolean`},
			{`(require '[doggo.io :as io]) (io/move "/tmp/a" "/tmp/b" {:missing :weird})`, `doggo.io/move: option :missing must be :error or :ignore`},
		}
		for _, tt := range tests {
			if _, err := runner.Run(tt.input); err == nil {
				t.Fatalf("expected error for %s", tt.input)
			} else if !strings.Contains(err.Error(), tt.wantMessage) {
				t.Fatalf("expected error containing %q for %s, got %v", tt.wantMessage, tt.input, err)
			}
		}
	})

	t.Run("copy-tree and move reject self and descendant destinations", func(t *testing.T) {
		baseDir := t.TempDir()
		fileSrc := filepath.Join(baseDir, "self.txt")
		dirSrc := filepath.Join(baseDir, "tree")
		dirSrcChild := filepath.Join(dirSrc, "child.txt")
		dirNestedDst := filepath.Join(dirSrc, "nested", "dst")

		if err := os.WriteFile(fileSrc, []byte("self"), 0o644); err != nil {
			t.Fatalf("write self file: %v", err)
		}
		if err := os.MkdirAll(filepath.Dir(dirSrcChild), 0o755); err != nil {
			t.Fatalf("mkdir tree: %v", err)
		}
		if err := os.WriteFile(dirSrcChild, []byte("child"), 0o644); err != nil {
			t.Fatalf("write tree child: %v", err)
		}

		runner := MustNewRunner()
		tests := []struct {
			name        string
			input       string
			wantMessage string
		}{
			{
				name:        "copy file to itself",
				input:       `(require '[doggo.io :as io]) (io/copy-tree "` + filepath.ToSlash(fileSrc) + `" "` + filepath.ToSlash(fileSrc) + `" {:overwrite true})`,
				wantMessage: `source and destination must be different`,
			},
			{
				name:        "move file to itself",
				input:       `(require '[doggo.io :as io]) (io/move "` + filepath.ToSlash(fileSrc) + `" "` + filepath.ToSlash(fileSrc) + `" {:overwrite true})`,
				wantMessage: `source and destination must be different`,
			},
			{
				name:        "copy dir into descendant",
				input:       `(require '[doggo.io :as io]) (io/copy-tree "` + filepath.ToSlash(dirSrc) + `" "` + filepath.ToSlash(dirNestedDst) + `")`,
				wantMessage: `destination must not be the source or a descendant of the source`,
			},
			{
				name:        "move dir into descendant",
				input:       `(require '[doggo.io :as io]) (io/move "` + filepath.ToSlash(dirSrc) + `" "` + filepath.ToSlash(dirNestedDst) + `")`,
				wantMessage: `destination must not be the source or a descendant of the source`,
			},
		}
		for _, tt := range tests {
			if _, err := runner.Run(tt.input); err == nil {
				t.Fatalf("expected error for %s", tt.name)
			} else if !strings.Contains(err.Error(), tt.wantMessage) {
				t.Fatalf("expected error containing %q for %s, got %v", tt.wantMessage, tt.name, err)
			}
		}
	})
}
