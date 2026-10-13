package release

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestReleaseAssetPublisher(t *testing.T) {
	bash := releaseBashForTest(t)
	script := filepath.ToSlash(filepath.Join("..", "..", ".github", "scripts", "publish-release-assets.sh"))
	for _, tc := range []struct {
		name        string
		mode        string
		wantError   bool
		wantCreate  bool
		wantStale   bool
		wantUpload  bool
		wantPublish bool
	}{
		{name: "new release", mode: "new", wantCreate: true, wantUpload: true, wantPublish: true},
		{name: "draft retry", mode: "exact", wantUpload: true, wantPublish: true},
		{name: "partial draft retry", mode: "partial", wantUpload: true, wantPublish: true},
		{name: "published release", mode: "published", wantError: true},
		{name: "stale asset", mode: "stale", wantError: true, wantStale: true},
		{name: "broken gh query", mode: "broken", wantError: true},
		{name: "invalid draft state", mode: "invalid", wantError: true},
		{name: "upload failure", mode: "upload-fail", wantCreate: true, wantUpload: true, wantError: true},
		{name: "download failure", mode: "download-fail", wantCreate: true, wantUpload: true, wantError: true},
		{name: "downloaded bytes changed", mode: "tampered", wantCreate: true, wantUpload: true, wantError: true},
		{name: "unexpected uploaded asset", mode: "extra", wantCreate: true, wantUpload: true, wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			bundle := filepath.Join(root, "bundle")
			if err := os.Mkdir(bundle, 0755); err != nil {
				t.Fatal(err)
			}
			for _, name := range []string{"doggo", "SHA256SUMS"} {
				if err := os.WriteFile(filepath.Join(bundle, name), []byte(name), 0644); err != nil {
					t.Fatal(err)
				}
			}
			remote := filepath.Join(root, "assets")
			if err := os.Mkdir(remote, 0755); err != nil {
				t.Fatal(err)
			}
			statePath := filepath.Join(root, "draft")
			switch tc.mode {
			case "exact", "partial", "stale", "broken", "invalid", "published":
				state := "true"
				if tc.mode == "published" {
					state = "false"
				}
				if err := os.WriteFile(statePath, []byte(state), 0644); err != nil {
					t.Fatal(err)
				}
				for _, name := range []string{"doggo", "SHA256SUMS"} {
					if tc.mode == "partial" && name == "doggo" {
						continue
					}
					if err := os.WriteFile(filepath.Join(remote, name), []byte("old draft bytes"), 0644); err != nil {
						t.Fatal(err)
					}
				}
				if tc.mode == "stale" {
					if err := os.WriteFile(filepath.Join(remote, "stale.bin"), []byte("stale"), 0644); err != nil {
						t.Fatal(err)
					}
				}
			}
			fakeBin := filepath.Join(root, "bin")
			if err := os.Mkdir(fakeBin, 0755); err != nil {
				t.Fatal(err)
			}
			fakeGH := `#!/usr/bin/env bash
set -euo pipefail
printf '%s\n' "$*" >> "$FAKE_GH_LOG"
if [[ "$1 $2" == "release view" ]]; then
  [[ -f "$FAKE_GH_STATE" ]] || exit 1
  if [[ "$FAKE_GH_MODE" == broken && "$*" == *"--json"* ]]; then exit 23; fi
  if [[ "$*" == *"--json isDraft"* ]]; then
    if [[ "$FAKE_GH_MODE" == invalid ]]; then printf maybe; else cat "$FAKE_GH_STATE"; fi
  elif [[ "$*" == *"--json assets"* ]]; then
    find "$FAKE_GH_ASSETS" -maxdepth 1 -type f -exec basename {} \; | LC_ALL=C sort
  fi
elif [[ "$1 $2" == "release create" ]]; then
  if [[ " $* " == *" --draft "* ]]; then printf true; else printf false; fi > "$FAKE_GH_STATE"
elif [[ "$1 $2" == "release upload" ]]; then
  printf 'upload-state=%s\n' "$(cat "$FAKE_GH_STATE")" >> "$FAKE_GH_LOG"
  shift 3
  for source in "$@"; do
    [[ "$source" != --clobber ]] || continue
    cp "$source" "$FAKE_GH_ASSETS/$(basename "$source")"
    if [[ "$FAKE_GH_MODE" == upload-fail ]]; then exit 23; fi
  done
  if [[ "$FAKE_GH_MODE" == extra ]]; then printf extra > "$FAKE_GH_ASSETS/unexpected.bin"; fi
elif [[ "$1 $2" == "release download" ]]; then
  [[ "$FAKE_GH_MODE" != download-fail ]] || exit 23
  shift 3
  [[ "$1" == --dir ]] || exit 24
  destination="$2"
  cp "$FAKE_GH_ASSETS/"* "$destination/"
  if [[ "$FAKE_GH_MODE" == tampered ]]; then printf corrupt > "$destination/doggo"; fi
elif [[ "$1 $2" == "release edit" ]]; then
  [[ "$*" == *"--draft=false"* ]] || exit 24
  printf false > "$FAKE_GH_STATE"
else
  exit 25
fi
`
			if err := os.WriteFile(filepath.Join(fakeBin, "gh"), []byte(fakeGH), 0755); err != nil {
				t.Fatal(err)
			}
			logPath := filepath.Join(root, "gh.log")
			cmd := exec.Command(bash, script, "v1.2.3", filepath.ToSlash(bundle))
			cmd.Env = append(os.Environ(),
				"PATH="+fakeBin+string(os.PathListSeparator)+os.Getenv("PATH"),
				"FAKE_GH_LOG="+filepath.ToSlash(logPath),
				"FAKE_GH_MODE="+tc.mode,
				"FAKE_GH_STATE="+filepath.ToSlash(statePath),
				"FAKE_GH_ASSETS="+filepath.ToSlash(remote),
			)
			output, err := cmd.CombinedOutput()
			if tc.wantError != (err != nil) {
				t.Fatalf("publisher error = %v, output=%s", err, output)
			}
			logContent, readErr := os.ReadFile(logPath)
			if readErr != nil {
				t.Fatal(readErr)
			}
			log := string(logContent)
			if got := strings.Contains(log, "release create v1.2.3"); got != tc.wantCreate {
				t.Fatalf("create logged=%v want=%v; log=%s", got, tc.wantCreate, log)
			}
			if got := strings.Contains(log, "release upload v1.2.3"); got != tc.wantUpload {
				t.Fatalf("upload logged=%v want=%v; log=%s", got, tc.wantUpload, log)
			}
			if strings.Contains(log, "upload-state=false") {
				t.Fatalf("uploaded to published release: %s", log)
			}
			if got := strings.Contains(log, "release edit v1.2.3"); got != tc.wantPublish {
				t.Fatalf("publish logged=%v want=%v; log=%s", got, tc.wantPublish, log)
			}
			if tc.wantCreate || tc.mode == "exact" || tc.mode == "partial" {
				state, err := os.ReadFile(statePath)
				if err != nil {
					t.Fatal(err)
				}
				want := "true"
				if tc.wantPublish {
					want = "false"
				}
				if string(state) != want {
					t.Fatalf("draft state=%s want=%s; log=%s", state, want, log)
				}
			}
			if tc.wantStale && (!strings.Contains(string(output), "stale.bin") || !strings.Contains(string(output), "stale assets")) {
				t.Fatalf("stale failure output = %s", output)
			}
		})
	}
}

func releaseBashForTest(t *testing.T) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		// The Windows system bash is a WSL launcher; run the fixture in Git Bash
		// so it uses the same Windows files and environment as this test process.
		git, err := exec.LookPath("git")
		if err == nil {
			for _, candidate := range []string{
				filepath.Join(filepath.Dir(git), "bash.exe"),
				filepath.Join(filepath.Dir(git), "..", "bin", "bash.exe"),
			} {
				if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
					return candidate
				}
			}
		}
		t.Skip("publisher shell fixture requires Git Bash on Windows")
	}
	bash, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("publisher shell fixture requires Bash")
	}
	return bash
}

func TestReleaseWorkflowUsesTestedPublisherAndDistributesPublicKey(t *testing.T) {
	workflowPath := filepath.Join("..", "..", ".github", "workflows", "release.yml")
	content, err := os.ReadFile(workflowPath)
	if err != nil {
		t.Fatal(err)
	}
	workflow := string(content)
	for _, fragment := range []string{
		`.github/scripts/publish-release-assets.sh "$VERSION" release-assets/release-bundle`,
		`public_key="release-assets/doggo-release-public.pem"`,
		`doggo-release-public.pem.sha256`,
		`-name '*.pem'`,
		`-name '*.pem.sha256'`,
		`--public-key "$public_key"`,
		`GITHUB_STEP_SUMMARY`,
	} {
		if !strings.Contains(workflow, fragment) {
			t.Errorf("release workflow does not contain %q", fragment)
		}
	}
	if strings.Contains(workflow, `gh release upload "$VERSION"`) {
		t.Fatal("release workflow duplicates the tested publisher inline")
	}
}

func TestReleaseQualityVerifiesRootLockBeforeTests(t *testing.T) {
	workflowPath := filepath.Join("..", "..", ".github", "workflows", "release.yml")
	content, err := os.ReadFile(workflowPath)
	if err != nil {
		t.Fatal(err)
	}
	workflow := string(content)
	syncAt := strings.Index(workflow, "go run ./cmd/doggo mod sync")
	diffAt := strings.Index(workflow, "git diff --exit-code -- doggo.lock")
	testsAt := strings.Index(workflow, "go test -count=1 ./...")
	if syncAt < 0 || diffAt < 0 {
		t.Fatal("release quality job does not materialize and verify the tracked root lock")
	}
	if testsAt < 0 || syncAt > diffAt || diffAt > testsAt {
		t.Fatal("release quality job does not verify the tracked root lock before tests")
	}
}

func TestCIRequiresActionlintWorkflowValidation(t *testing.T) {
	ciPath := filepath.Join("..", "..", ".github", "workflows", "ci.yml")
	content, err := os.ReadFile(ciPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(content), "github.com/rhysd/actionlint/cmd/actionlint@") {
		t.Fatal("CI does not require actionlint workflow validation")
	}
}

func TestReleaseMakeTargetsReuseImmutableBundleDirectory(t *testing.T) {
	makefilePath := filepath.Join("..", "..", "Makefile")
	content, err := os.ReadFile(makefilePath)
	if err != nil {
		t.Fatal(err)
	}
	makefile := string(content)
	if !strings.Contains(makefile, "BIN_DIR := dist/bin") {
		t.Fatal("Makefile does not place build binaries under dist/bin")
	}
	if strings.Contains(makefile, "BIN_DIR := bin\n") {
		t.Fatal("Makefile still places build binaries in the project-root bin directory")
	}
	if strings.Contains(makefile, "release-gate: release-bundle") {
		t.Fatal("release-gate regenerates an immutable release bundle")
	}
	for _, command := range []string{
		"release bundle --out-dir $(RELEASE_OUT_DIR)",
		"release verify --out-dir $(RELEASE_OUT_DIR)",
		"release gate --out-dir $(RELEASE_OUT_DIR)",
	} {
		if !strings.Contains(makefile, command) {
			t.Errorf("Makefile does not contain %q", command)
		}
	}
}
