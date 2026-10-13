package cli

import (
	"os"
	"path/filepath"
	"testing"
)

func TestApp_Run_TestCommand_UsesExplicitPathProjectRoot(t *testing.T) {
	projectRoot := t.TempDir()
	writeModAndLock(t, projectRoot)

	testPath := filepath.Join(projectRoot, "examples", "sample_test.dog")
	if err := os.MkdirAll(filepath.Dir(testPath), 0755); err != nil {
		t.Fatalf("mkdir examples dir: %v", err)
	}
	source := `(ns examples.sample)
(doggo.test/deftest explicit-path-test
  (doggo.test/is (= (+ 1 2) 3)))
`
	if err := os.WriteFile(testPath, []byte(source), 0644); err != nil {
		t.Fatalf("write sample_test.dog: %v", err)
	}

	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer os.Chdir(oldWD)
	outsideDir := t.TempDir()
	if err := os.Chdir(outsideDir); err != nil {
		t.Fatalf("chdir outside project: %v", err)
	}

	app := NewApp("1.0.0", []string{"test", testPath})
	if err := app.Run(); err != nil {
		t.Fatalf("test command should resolve project root from explicit path: %v", err)
	}
}

func TestExampleTestFileRuns(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping example command e2e in short mode")
	}

	repoRoot := findRepoRootForCLITest(t)
	projectRoot := t.TempDir()
	writeModAndLock(t, projectRoot)
	examplePath := filepath.Join(projectRoot, "examples", "sample_test.dog")
	if err := os.MkdirAll(filepath.Dir(examplePath), 0755); err != nil {
		t.Fatal(err)
	}
	source, err := os.ReadFile(filepath.Join(repoRoot, "examples", "sample_test.dog"))
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(examplePath, source, 0644); err != nil {
		t.Fatal(err)
	}

	oldWD, err := os.Getwd()
	if err != nil {
		t.Fatalf("getwd: %v", err)
	}
	defer os.Chdir(oldWD)
	outsideDir := t.TempDir()
	if err := os.Chdir(outsideDir); err != nil {
		t.Fatalf("chdir outside project: %v", err)
	}

	if err := NewApp("1.0.0", []string{"test", examplePath}).Run(); err != nil {
		t.Fatalf("example test failed: %v", err)
	}
}
