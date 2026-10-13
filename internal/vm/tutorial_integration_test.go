package vm

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTutorialRuns(t *testing.T) {
	tutorialPath := filepath.Join("..", "..", "examples", "tutorial.dog")
	srcBytes, err := os.ReadFile(tutorialPath)
	if err != nil {
		t.Fatal(err)
	}

	runner := mustNewRunnerWithLoadPaths(t, filepath.Dir(tutorialPath))
	if _, err := runner.Run(string(srcBytes)); err != nil {
		t.Fatalf("run tutorial: %v", err)
	}
}
