package release

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"debug/buildinfo"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"sort"
	"strings"
	"testing"

	"doggo/internal/project"
	"doggo/internal/testutil"
)

func TestGenerateReleaseBundleUsesSnapshotAfterProjectPathReplacement(t *testing.T) {
	projectRoot := t.TempDir()
	binaryPath, binaryDigest := writeGenerationBinaryFixture(t, projectRoot, 0755)
	binary, err := os.ReadFile(binaryPath)
	if err != nil {
		t.Fatal(err)
	}
	extra := "editors/vscode/dist/doggo-vscode.vsix"
	writeReleaseBundleFixture(t, projectRoot, extra)
	binaryRel, err := filepath.Rel(projectRoot, binaryPath)
	if err != nil {
		t.Fatal(err)
	}
	snapshot := project.GenerationBinarySnapshot{ProjectPath: filepath.ToSlash(binaryRel), SHA256: binaryDigest, Content: binary}
	if err := os.Rename(binaryPath, binaryPath+".retained"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(binaryPath, []byte("replacement without Go build info"), 0755); err != nil {
		t.Fatal(err)
	}

	out, err := GenerateReleaseBundle(projectRoot, snapshot, []string{extra}, DefaultReleaseBundleDir, "")
	if err != nil {
		t.Fatal(err)
	}
	logicalPath := filepath.Join(projectRoot, DefaultReleaseBundleDir, "doggo")
	got, err := os.ReadFile(logicalPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != string(binary) {
		t.Fatalf("logical doggo = %q, want %q", got, binary)
	}
	paths := make([]string, 0, len(out.Artifacts))
	for _, artifact := range out.Artifacts {
		paths = append(paths, artifact.Path)
	}
	if strings.Contains(strings.Join(paths, "\n"), ".doggo/generations/") {
		t.Fatalf("generation-qualified source leaked into artifacts: %#v", paths)
	}
	if !containsString(paths, "doggo") || !containsString(paths, "doggo-vscode.vsix") {
		t.Fatalf("artifact paths = %#v, want self-contained logical names", paths)
	}
	copiedExtra, err := os.ReadFile(filepath.Join(projectRoot, DefaultReleaseBundleDir, "doggo-vscode.vsix"))
	if err != nil || string(copiedExtra) != "extra:"+extra {
		t.Fatalf("copied extra = %q, err=%v", copiedExtra, err)
	}
	sums, err := os.ReadFile(out.SHA256SumsPath)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(sums), "  doggo\n") || strings.Contains(string(sums), "release-assets/") || strings.Contains(string(sums), ".doggo/") {
		t.Fatalf("SHA256SUMS does not name logical doggo: %s", sums)
	}
	for _, name := range []string{"doggo-vscode.vsix", goSBOMFileName, vsixSBOMFileName, manifestFileName} {
		if !strings.Contains(string(sums), "  "+name+"\n") {
			t.Fatalf("SHA256SUMS does not cover %s: %s", name, sums)
		}
	}
}

func TestGenerateReleaseBundleRejectsExistingOutputDirectoryWithoutMutation(t *testing.T) {
	projectRoot := t.TempDir()
	binaryPath, binaryDigest := writeGenerationBinaryFixture(t, projectRoot, 0755)
	writeReleaseBundleFixture(t, projectRoot)
	outputDir := filepath.Join(projectRoot, DefaultReleaseBundleDir)
	if err := os.MkdirAll(outputDir, 0755); err != nil {
		t.Fatal(err)
	}

	if _, err := GenerateReleaseBundle(projectRoot, generationBinarySnapshot(t, projectRoot, binaryPath, binaryDigest), nil, DefaultReleaseBundleDir, ""); err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("GenerateReleaseBundle error = %v, want existing-directory rejection", err)
	}
	entries, err := os.ReadDir(outputDir)
	if err != nil || len(entries) != 0 {
		t.Fatalf("existing output changed: entries=%v err=%v", entries, err)
	}
}

func TestGenerateReleaseBundleDoesNotAdoptOutputDirectoryAppearingAtPublish(t *testing.T) {
	projectRoot := t.TempDir()
	binaryPath, binaryDigest := writeGenerationBinaryFixture(t, projectRoot, 0755)
	writeReleaseBundleFixture(t, projectRoot)
	outputDir := filepath.Join(projectRoot, DefaultReleaseBundleDir)
	marker := filepath.Join(outputDir, "doggo")
	beforeReleasePublishForTest = func(step string) error {
		if step != "directory" {
			return nil
		}
		if err := os.MkdirAll(outputDir, 0755); err != nil {
			t.Fatal(err)
		}
		return os.WriteFile(marker, []byte("racer"), 0644)
	}
	t.Cleanup(func() { beforeReleasePublishForTest = nil })

	if _, err := GenerateReleaseBundle(projectRoot, generationBinarySnapshot(t, projectRoot, binaryPath, binaryDigest), nil, DefaultReleaseBundleDir, ""); err == nil {
		t.Fatal("GenerateReleaseBundle adopted output directory that appeared at publish")
	}
	content, err := os.ReadFile(marker)
	if err != nil || string(content) != "racer" {
		t.Fatalf("racing output changed: content=%q err=%v", content, err)
	}
	entries, err := os.ReadDir(outputDir)
	if err != nil || len(entries) != 1 || entries[0].Name() != "doggo" {
		t.Fatalf("racing output entries = %v, err=%v", entries, err)
	}
}

func TestGenerateReleaseBundleRejectsHardLinkedExtrasWithDifferentNames(t *testing.T) {
	projectRoot := t.TempDir()
	binaryPath, binaryDigest := writeGenerationBinaryFixture(t, projectRoot, 0755)
	first := filepath.Join(projectRoot, "artifacts", "first.zip")
	second := filepath.Join(projectRoot, "artifacts", "second.zip")
	writeReleaseBundleFixture(t, projectRoot, filepath.ToSlash(strings.TrimPrefix(first, projectRoot+string(filepath.Separator))))
	if err := os.Link(first, second); err != nil {
		t.Skipf("create hard link: %v", err)
	}

	if _, err := GenerateReleaseBundle(projectRoot, generationBinarySnapshot(t, projectRoot, binaryPath, binaryDigest), []string{first, second}, DefaultReleaseBundleDir, ""); err == nil || !strings.Contains(err.Error(), "same file") {
		t.Fatalf("GenerateReleaseBundle error = %v, want duplicate source rejection", err)
	}
	if _, err := os.Lstat(filepath.Join(projectRoot, DefaultReleaseBundleDir)); !os.IsNotExist(err) {
		t.Fatalf("release output created after duplicate source rejection: %v", err)
	}
}

func TestCheckReleaseIntegrityRejectsMutatedSignedMetadata(t *testing.T) {
	for _, name := range []string{goSBOMFileName, vsixSBOMFileName, manifestFileName} {
		t.Run(name, func(t *testing.T) {
			projectRoot := t.TempDir()
			binaryPath, digest := writeGenerationBinaryFixture(t, projectRoot, 0755)
			writeReleaseBundleFixture(t, projectRoot)
			keyDir := filepath.Join(projectRoot, "keys")
			privateKeyPath := filepath.Join(keyDir, PrivateKeyFileName)
			publicKeyPath := filepath.Join(keyDir, PublicKeyFileName)
			if err := GenerateKeyPair(keyDir); err != nil {
				t.Fatal(err)
			}
			if _, err := GenerateReleaseBundle(projectRoot, generationBinarySnapshot(t, projectRoot, binaryPath, digest), nil, DefaultReleaseBundleDir, privateKeyPath); err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(projectRoot, DefaultReleaseBundleDir, name)
			if err := os.WriteFile(path, []byte("tampered\n"), 0644); err != nil {
				t.Fatal(err)
			}
			if err := CheckReleaseIntegrityFiles(projectRoot, DefaultReleaseBundleDir, publicKeyPath, true); err == nil {
				t.Fatalf("verification accepted mutated %s", name)
			}
		})
	}
}

func TestCheckReleaseIntegrityRejectsResignedInvalidManifestContract(t *testing.T) {
	projectRoot := t.TempDir()
	binaryPath, digest := writeGenerationBinaryFixture(t, projectRoot, 0755)
	writeReleaseBundleFixture(t, projectRoot)
	keyDir := filepath.Join(projectRoot, "keys")
	privateKeyPath := filepath.Join(keyDir, PrivateKeyFileName)
	publicKeyPath := filepath.Join(keyDir, PublicKeyFileName)
	if err := GenerateKeyPair(keyDir); err != nil {
		t.Fatal(err)
	}
	if _, err := GenerateReleaseBundle(projectRoot, generationBinarySnapshot(t, projectRoot, binaryPath, digest), nil, DefaultReleaseBundleDir, privateKeyPath); err != nil {
		t.Fatal(err)
	}

	outputDir := filepath.Join(projectRoot, DefaultReleaseBundleDir)
	manifestPath := filepath.Join(outputDir, manifestFileName)
	manifestContent, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var manifest releaseBundleManifest
	if err := json.Unmarshal(manifestContent, &manifest); err != nil {
		t.Fatal(err)
	}
	manifest.GoSBOM = "attacker-sbom.json"
	manifestContent, err = marshalJSON(manifest, "attacker manifest")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, manifestContent, 0644); err != nil {
		t.Fatal(err)
	}

	shaPath := filepath.Join(outputDir, sha256SumsFileName)
	shaContent, err := os.ReadFile(shaPath)
	if err != nil {
		t.Fatal(err)
	}
	sums, err := parseSHA256Sums(shaContent)
	if err != nil {
		t.Fatal(err)
	}
	sums[manifestFileName] = digestFor(manifestFileName, manifestContent).SHA256
	entries := make([]ArtifactDigest, 0, len(sums))
	for name, sum := range sums {
		entries = append(entries, ArtifactDigest{Path: name, SHA256: sum})
	}
	stringsContent := marshalSHA256Sums(entries)
	if err := os.WriteFile(shaPath, stringsContent, 0644); err != nil {
		t.Fatal(err)
	}
	privateKey, err := loadEd25519PrivateKey(privateKeyPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outputDir, sha256SigFileName), ed25519.Sign(privateKey, stringsContent), 0644); err != nil {
		t.Fatal(err)
	}

	if err := CheckReleaseIntegrityFiles(projectRoot, DefaultReleaseBundleDir, publicKeyPath, true); err == nil || !strings.Contains(err.Error(), "manifest contract") {
		t.Fatalf("CheckReleaseIntegrityFiles error = %v", err)
	}
}

func TestCheckReleaseIntegrityRejectsUntrackedBundleFile(t *testing.T) {
	projectRoot := t.TempDir()
	binaryPath, digest := writeGenerationBinaryFixture(t, projectRoot, 0755)
	writeReleaseBundleFixture(t, projectRoot)
	keyDir := filepath.Join(projectRoot, "keys")
	privateKeyPath := filepath.Join(keyDir, PrivateKeyFileName)
	publicKeyPath := filepath.Join(keyDir, PublicKeyFileName)
	if err := GenerateKeyPair(keyDir); err != nil {
		t.Fatal(err)
	}
	if _, err := GenerateReleaseBundle(projectRoot, generationBinarySnapshot(t, projectRoot, binaryPath, digest), nil, DefaultReleaseBundleDir, privateKeyPath); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(projectRoot, DefaultReleaseBundleDir, "stale.zip"), []byte("stale"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := CheckReleaseIntegrityFiles(projectRoot, DefaultReleaseBundleDir, publicKeyPath, true); err == nil || !strings.Contains(err.Error(), "untracked bundle entry") {
		t.Fatalf("CheckReleaseIntegrityFiles error = %v", err)
	}
}

func TestCheckReleaseIntegrityRequiresPublicKeyWhenSignatureIsRequired(t *testing.T) {
	projectRoot := t.TempDir()
	binaryPath, digest := writeGenerationBinaryFixture(t, projectRoot, 0755)
	writeReleaseBundleFixture(t, projectRoot)
	keyDir := filepath.Join(projectRoot, "keys")
	privateKeyPath := filepath.Join(keyDir, PrivateKeyFileName)
	if err := GenerateKeyPair(keyDir); err != nil {
		t.Fatal(err)
	}
	if _, err := GenerateReleaseBundle(projectRoot, generationBinarySnapshot(t, projectRoot, binaryPath, digest), nil, DefaultReleaseBundleDir, privateKeyPath); err != nil {
		t.Fatal(err)
	}

	if err := CheckReleaseIntegrityFiles(projectRoot, DefaultReleaseBundleDir, "", true); err == nil || !strings.Contains(err.Error(), "public key") {
		t.Fatalf("CheckReleaseIntegrityFiles error = %v", err)
	}
}

func TestCheckReleaseIntegrityRejectsSignedBundleWithoutVerificationKey(t *testing.T) {
	projectRoot := t.TempDir()
	binaryPath, digest := writeGenerationBinaryFixture(t, projectRoot, 0755)
	writeReleaseBundleFixture(t, projectRoot)
	keyDir := filepath.Join(projectRoot, "keys")
	privateKeyPath := filepath.Join(keyDir, PrivateKeyFileName)
	if err := GenerateKeyPair(keyDir); err != nil {
		t.Fatal(err)
	}
	if _, err := GenerateReleaseBundle(projectRoot, generationBinarySnapshot(t, projectRoot, binaryPath, digest), nil, DefaultReleaseBundleDir, privateKeyPath); err != nil {
		t.Fatal(err)
	}

	if err := CheckReleaseIntegrityFiles(projectRoot, DefaultReleaseBundleDir, "", false); err == nil || !strings.Contains(err.Error(), "public key") {
		t.Fatalf("CheckReleaseIntegrityFiles error = %v", err)
	}
}

func TestCheckReleaseIntegrityRejectsSignedUppercaseDoggoOnlyManifest(t *testing.T) {
	projectRoot := t.TempDir()
	binaryPath, digest := writeGenerationBinaryFixture(t, projectRoot, 0755)
	writeReleaseBundleFixture(t, projectRoot)
	keyDir := filepath.Join(projectRoot, "keys")
	privateKeyPath := filepath.Join(keyDir, PrivateKeyFileName)
	publicKeyPath := filepath.Join(keyDir, PublicKeyFileName)
	if err := GenerateKeyPair(keyDir); err != nil {
		t.Fatal(err)
	}
	if _, err := GenerateReleaseBundle(projectRoot, generationBinarySnapshot(t, projectRoot, binaryPath, digest), nil, DefaultReleaseBundleDir, privateKeyPath); err != nil {
		t.Fatal(err)
	}

	outputDir := filepath.Join(projectRoot, DefaultReleaseBundleDir)
	if err := os.Rename(filepath.Join(outputDir, "doggo"), filepath.Join(outputDir, "DOGGO")); err != nil {
		t.Fatal(err)
	}
	manifestPath := filepath.Join(outputDir, manifestFileName)
	manifestContent, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatal(err)
	}
	var manifest releaseBundleManifest
	if err := json.Unmarshal(manifestContent, &manifest); err != nil {
		t.Fatal(err)
	}
	manifest.Artifacts[0].Path = "DOGGO"
	manifestContent, err = marshalJSON(manifest, "uppercase doggo manifest")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(manifestPath, manifestContent, 0644); err != nil {
		t.Fatal(err)
	}
	checksummed := []ArtifactDigest{digestFor(manifestFileName, manifestContent)}
	for _, name := range []string{"DOGGO", goSBOMFileName, vsixSBOMFileName} {
		content, err := os.ReadFile(filepath.Join(outputDir, name))
		if err != nil {
			t.Fatal(err)
		}
		checksummed = append(checksummed, digestFor(name, content))
	}
	sort.Slice(checksummed, func(i, j int) bool { return checksummed[i].Path < checksummed[j].Path })
	shaContent := marshalSHA256Sums(checksummed)
	if err := os.WriteFile(filepath.Join(outputDir, sha256SumsFileName), shaContent, 0644); err != nil {
		t.Fatal(err)
	}
	privateKey, err := loadEd25519PrivateKey(privateKeyPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outputDir, sha256SigFileName), ed25519.Sign(privateKey, shaContent), 0644); err != nil {
		t.Fatal(err)
	}

	if err := CheckReleaseIntegrityFiles(projectRoot, DefaultReleaseBundleDir, publicKeyPath, true); err == nil || !strings.Contains(err.Error(), "doggo") {
		t.Fatalf("CheckReleaseIntegrityFiles error = %v", err)
	}
}

func TestCheckReleaseIntegrityRejectsNonExecutableDoggoOnUnix(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not represent Unix executable mode bits")
	}
	projectRoot := t.TempDir()
	binaryPath, digest := writeGenerationBinaryFixture(t, projectRoot, 0755)
	writeReleaseBundleFixture(t, projectRoot)
	keyDir := filepath.Join(projectRoot, "keys")
	privateKeyPath := filepath.Join(keyDir, PrivateKeyFileName)
	publicKeyPath := filepath.Join(keyDir, PublicKeyFileName)
	if err := GenerateKeyPair(keyDir); err != nil {
		t.Fatal(err)
	}
	if _, err := GenerateReleaseBundle(projectRoot, generationBinarySnapshot(t, projectRoot, binaryPath, digest), nil, DefaultReleaseBundleDir, privateKeyPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(filepath.Join(projectRoot, DefaultReleaseBundleDir, "doggo"), 0644); err != nil {
		t.Fatal(err)
	}

	if err := CheckReleaseIntegrityFiles(projectRoot, DefaultReleaseBundleDir, publicKeyPath, true); err == nil || !strings.Contains(err.Error(), "mode") {
		t.Fatalf("CheckReleaseIntegrityFiles error = %v", err)
	}
}

func TestParseSHA256SumsRejectsNonCanonicalBundlePath(t *testing.T) {
	line := strings.Repeat("0", sha256.Size*2) + "  .doggo/../doggo\n"
	if _, err := parseSHA256Sums([]byte(line)); err == nil {
		t.Fatal("parseSHA256Sums accepted a non-canonical generation-qualified path")
	}
}

func TestGenerateReleaseBundleRejectsReservedAndDuplicateExtraBasenames(t *testing.T) {
	for _, tc := range []struct {
		name   string
		extras []string
	}{
		{name: "doggo", extras: []string{"extras/doggo"}},
		{name: "checksums", extras: []string{"extras/" + sha256SumsFileName}},
		{name: "signature", extras: []string{"extras/" + sha256SigFileName}},
		{name: "Go SBOM", extras: []string{"extras/" + goSBOMFileName}},
		{name: "VS Code SBOM", extras: []string{"extras/" + vsixSBOMFileName}},
		{name: "manifest", extras: []string{"extras/" + manifestFileName}},
		{name: "duplicate basename", extras: []string{"one/archive.zip", "two/archive.zip"}},
		{name: "portable reserved name", extras: []string{"extras/sha256sums"}},
		{name: "portable duplicate basename", extras: []string{"one/Archive.zip", "two/archive.zip"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			projectRoot := t.TempDir()
			binaryPath, digest := writeGenerationBinaryFixture(t, projectRoot, 0755)
			writeReleaseBundleFixture(t, projectRoot, tc.extras...)
			_, err := GenerateReleaseBundle(projectRoot, generationBinarySnapshot(t, projectRoot, binaryPath, digest), tc.extras, DefaultReleaseBundleDir, "")
			if err == nil || !strings.Contains(err.Error(), "reserved or duplicate bundle name") {
				t.Fatalf("GenerateReleaseBundle error = %v", err)
			}
			if _, statErr := os.Lstat(filepath.Join(projectRoot, DefaultReleaseBundleDir)); !os.IsNotExist(statErr) {
				t.Fatalf("release output created: %v", statErr)
			}
		})
	}
}

func TestGenerateReleaseBundleRejectsNonPortableExtraBasenameBeforeMutation(t *testing.T) {
	for _, tc := range []struct {
		name      string
		basenames []string
	}{
		{name: "control", basenames: []string{"bad\nname.zip"}},
		{name: "colon", basenames: []string{"bad:name.zip"}},
		{name: "trailing dot", basenames: []string{"trailing."}},
		{name: "Windows NUL", basenames: []string{"NUL"}},
		{name: "Windows COM", basenames: []string{"com1.txt"}},
		{name: "Windows superscript device", basenames: []string{"COM¹.zip"}},
		{name: "NFC and NFD", basenames: []string{"é.zip", "e\u0301.zip"}},
		{name: "invalid UTF-8", basenames: []string{"bad-\xff.zip"}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			projectRoot := t.TempDir()
			binaryPath, digest := writeGenerationBinaryFixture(t, projectRoot, 0755)
			extras := make([]string, 0, len(tc.basenames))
			for _, basename := range tc.basenames {
				extras = append(extras, filepath.Join("extras", basename))
			}
			// Rejection happens before reading extras; invalid Windows names cannot
			// be represented by filesystem fixtures on every supported platform.
			writeReleaseBundleFixture(t, projectRoot)

			_, err := GenerateReleaseBundle(projectRoot, generationBinarySnapshot(t, projectRoot, binaryPath, digest), extras, DefaultReleaseBundleDir, "")
			if err == nil || !strings.Contains(err.Error(), "portable bundle name") {
				t.Fatalf("GenerateReleaseBundle error = %v", err)
			}
			if _, statErr := os.Lstat(filepath.Join(projectRoot, DefaultReleaseBundleDir)); !os.IsNotExist(statErr) {
				t.Fatalf("release output created: %v", statErr)
			}
		})
	}
}

func TestGenerateReleaseBundleRejectsOutputAppearingAfterPreflight(t *testing.T) {
	projectRoot := t.TempDir()
	binaryPath, digest := writeGenerationBinaryFixture(t, projectRoot, 0755)
	writeReleaseBundleFixture(t, projectRoot)
	outputDir := filepath.Join(projectRoot, DefaultReleaseBundleDir)
	marker := filepath.Join(outputDir, "doggo")
	beforeReleaseOutputParentOpenForTest = func() {
		if err := os.MkdirAll(outputDir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(marker, []byte("racer"), 0755); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { beforeReleaseOutputParentOpenForTest = nil })

	_, err := GenerateReleaseBundle(projectRoot, generationBinarySnapshot(t, projectRoot, binaryPath, digest), nil, DefaultReleaseBundleDir, "")
	if err == nil || !errors.Is(err, project.ErrOwnershipChanged) || !strings.Contains(err.Error(), "appeared") {
		t.Fatalf("GenerateReleaseBundle error = %v, want output appeared ownership error", err)
	}
	if content, err := os.ReadFile(marker); err != nil || string(content) != "racer" {
		t.Fatalf("racing output changed: content=%q err=%v", content, err)
	}
	entries, err := os.ReadDir(outputDir)
	if err != nil || len(entries) != 1 || entries[0].Name() != "doggo" {
		t.Fatalf("racing output entries = %v, err=%v", entries, err)
	}
}

func TestGenerateReleaseBundleRejectsReplacedStageAndPreservesRacer(t *testing.T) {
	projectRoot := t.TempDir()
	binaryPath, digest := writeGenerationBinaryFixture(t, projectRoot, 0755)
	writeReleaseBundleFixture(t, projectRoot)
	var ownedStage string
	var racerStage string
	afterReleaseDirectoryCreateForTest = func(kind, name string) {
		if kind != "stage" {
			return
		}
		racerStage = filepath.Join(projectRoot, filepath.Dir(DefaultReleaseBundleDir), name)
		ownedStage = racerStage + "-owned"
		if err := os.Rename(racerStage, ownedStage); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(racerStage, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(racerStage, "racer"), []byte("preserve"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { afterReleaseDirectoryCreateForTest = nil })

	if _, err := GenerateReleaseBundle(projectRoot, generationBinarySnapshot(t, projectRoot, binaryPath, digest), nil, DefaultReleaseBundleDir, ""); err == nil || !strings.Contains(err.Error(), "changed") {
		t.Fatalf("GenerateReleaseBundle error = %v", err)
	}
	if content, err := os.ReadFile(filepath.Join(racerStage, "racer")); err != nil || string(content) != "preserve" {
		t.Fatalf("racer stage changed: content=%q err=%v", content, err)
	}
	if _, err := os.Stat(ownedStage); !os.IsNotExist(err) {
		t.Fatalf("owned original stage was not cleaned: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(projectRoot, DefaultReleaseBundleDir)); !os.IsNotExist(err) {
		t.Fatalf("bundle output created after stage race: %v", err)
	}
}

func TestGenerateReleaseBundleRejectsGenerationOutputOverlap(t *testing.T) {
	for _, tc := range []struct {
		name      string
		outDir    func(t *testing.T, root string) string
		wantError string
	}{
		{name: "project root", outDir: func(_ *testing.T, _ string) string { return "." }, wantError: "project root"},
		{name: "direct", outDir: func(_ *testing.T, _ string) string { return ".doggo/release" }, wantError: "generation state"},
		{name: "symlink alias", outDir: func(t *testing.T, root string) string {
			alias := filepath.Join(root, "generation-output")
			if err := os.Symlink(filepath.Join(root, ".doggo"), alias); err != nil {
				t.Skipf("create symlink: %v", err)
			}
			return "generation-output/release"
		}, wantError: "not ordinary"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			projectRoot := t.TempDir()
			binaryPath, digest := writeGenerationBinaryFixture(t, projectRoot, 0755)
			writeReleaseBundleFixture(t, projectRoot)
			before, err := os.ReadFile(binaryPath)
			if err != nil {
				t.Fatal(err)
			}
			_, err = GenerateReleaseBundle(projectRoot, generationBinarySnapshot(t, projectRoot, binaryPath, digest), nil, tc.outDir(t, projectRoot), "")
			if err == nil || !strings.Contains(err.Error(), tc.wantError) {
				t.Fatalf("GenerateReleaseBundle error = %v", err)
			}
			after, readErr := os.ReadFile(binaryPath)
			if readErr != nil || !bytes.Equal(after, before) {
				t.Fatalf("generation binary changed: err=%v", readErr)
			}
		})
	}
}

func TestReleaseBinaryModeValidForPlatform(t *testing.T) {
	if !releaseBinaryModeValid("windows", 0666) {
		t.Fatal("Windows mode validation rejected regular-file permissions")
	}
	if releaseBinaryModeValid("linux", 0666) || !releaseBinaryModeValid("linux", 0755) {
		t.Fatal("Unix mode validation did not require 0755")
	}
}

func TestSameProjectRelativePathUsesWindowsCaseRules(t *testing.T) {
	const lower = ".doggo/generations/abc/bin/doggo.exe"
	const upper = ".DOGGO/GENERATIONS/ABC/BIN/DOGGO.EXE"
	if !sameProjectRelativePath("windows", lower, upper) {
		t.Fatal("Windows project paths were compared case-sensitively")
	}
	if sameProjectRelativePath("linux", lower, upper) {
		t.Fatal("Unix project paths were compared case-insensitively")
	}
}

func TestGenerateReleaseBundleRejectsActiveBinaryAsExtraBeforeMutation(t *testing.T) {
	for _, tc := range []struct {
		name      string
		wantError string
	}{
		{name: "relative path", wantError: "active generation binary cannot be listed in --artifacts"},
		{name: "absolute path", wantError: "active generation binary cannot be listed in --artifacts"},
		{name: "symbolic link", wantError: "is not an ordinary file"},
		{name: "logical output", wantError: "reserved or duplicate bundle name"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			projectRoot := t.TempDir()
			binaryPath, binaryDigest := writeGenerationBinaryFixture(t, projectRoot, 0755)
			writeReleaseBundleFixture(t, projectRoot)
			extra := binaryPath
			switch tc.name {
			case "relative path":
				var err error
				extra, err = filepath.Rel(projectRoot, binaryPath)
				if err != nil {
					t.Fatal(err)
				}
			case "symbolic link":
				extra = filepath.Join(projectRoot, "artifacts", "doggo-symlink")
				if err := os.MkdirAll(filepath.Dir(extra), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(binaryPath, extra); err != nil {
					t.Skipf("create symlink: %v", err)
				}
			case "logical output":
				extra = filepath.Join(DefaultReleaseBundleDir, "doggo")
			}
			_, err := GenerateReleaseBundle(projectRoot, generationBinarySnapshot(t, projectRoot, binaryPath, binaryDigest), []string{extra}, DefaultReleaseBundleDir, "")
			if err == nil || !strings.Contains(err.Error(), tc.wantError) {
				t.Fatalf("GenerateReleaseBundle error = %v, want extras-only rejection", err)
			}
			if _, statErr := os.Lstat(filepath.Join(projectRoot, DefaultReleaseBundleDir)); !os.IsNotExist(statErr) {
				t.Fatalf("bundle output created: %v", statErr)
			}
		})
	}
}

func TestGenerateReleaseBundleRejectsGenerationBinaryDigestMismatchBeforeReplacingLogicalArtifact(t *testing.T) {
	projectRoot := t.TempDir()
	writeReleaseBundleFixture(t, projectRoot)
	snapshot := project.GenerationBinarySnapshot{
		ProjectPath: "missing-generation-doggo",
		SHA256:      strings.Repeat("0", 64),
		Content:     []byte("tampered"),
	}
	_, err := GenerateReleaseBundle(projectRoot, snapshot, nil, DefaultReleaseBundleDir, "")
	if err == nil || !strings.Contains(err.Error(), "binary digest mismatch") {
		t.Fatalf("GenerateReleaseBundle error = %v, want digest mismatch", err)
	}
	if _, statErr := os.Lstat(filepath.Join(projectRoot, DefaultReleaseBundleDir)); !os.IsNotExist(statErr) {
		t.Fatalf("bundle output created on digest mismatch: %v", statErr)
	}
}

func TestGenerateReleaseBundlePublicationFailureLeavesNoPartialBundle(t *testing.T) {
	projectRoot := t.TempDir()
	binaryPath, digest := writeGenerationBinaryFixture(t, projectRoot, 0755)
	writeReleaseBundleFixture(t, projectRoot)
	beforeReleasePublishForTest = func(name string) error {
		if name == "directory" {
			return errors.New("injected publication failure")
		}
		return nil
	}
	t.Cleanup(func() { beforeReleasePublishForTest = nil })

	if _, err := GenerateReleaseBundle(projectRoot, generationBinarySnapshot(t, projectRoot, binaryPath, digest), nil, DefaultReleaseBundleDir, ""); err == nil || !strings.Contains(err.Error(), "injected publication failure") {
		t.Fatalf("GenerateReleaseBundle error = %v", err)
	}
	if _, err := os.Lstat(filepath.Join(projectRoot, DefaultReleaseBundleDir)); !os.IsNotExist(err) {
		t.Fatalf("partial bundle published: %v", err)
	}
	entries, err := os.ReadDir(filepath.Join(projectRoot, filepath.Dir(DefaultReleaseBundleDir)))
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".release-stage-") {
			t.Fatalf("release stage leaked after publication failure: %s", entry.Name())
		}
	}
}

func TestGenerateReleaseBundleRejectsStageMutationBeforeDirectoryPublication(t *testing.T) {
	tests := []struct {
		name   string
		mutate func(t *testing.T, stage string)
	}{
		{
			name: "doggo replaced",
			mutate: func(t *testing.T, stage string) {
				replacement := filepath.Join(stage, "replacement")
				if err := os.WriteFile(replacement, []byte("attacker"), 0755); err != nil {
					t.Fatal(err)
				}
				if err := os.Rename(replacement, filepath.Join(stage, "doggo")); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "doggo deleted",
			mutate: func(t *testing.T, stage string) {
				if err := os.Remove(filepath.Join(stage, "doggo")); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "metadata replaced",
			mutate: func(t *testing.T, stage string) {
				replacement := filepath.Join(stage, "replacement")
				if err := os.WriteFile(replacement, []byte("attacker"), 0644); err != nil {
					t.Fatal(err)
				}
				if err := os.Rename(replacement, filepath.Join(stage, manifestFileName)); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "entry added",
			mutate: func(t *testing.T, stage string) {
				if err := os.WriteFile(filepath.Join(stage, "attacker"), []byte("attacker"), 0644); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name: "mode changed",
			mutate: func(t *testing.T, stage string) {
				if err := os.Chmod(filepath.Join(stage, "doggo"), 0644); err != nil {
					t.Fatal(err)
				}
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if tc.name == "mode changed" && runtime.GOOS == "windows" {
				t.Skip("Windows does not expose Unix executable permission bits")
			}
			projectRoot := t.TempDir()
			binaryPath, digest := writeGenerationBinaryFixture(t, projectRoot, 0755)
			writeReleaseBundleFixture(t, projectRoot)
			beforeReleasePublishForTest = func(name string) error {
				if name == "directory" {
					tc.mutate(t, findStage(t, filepath.Join(projectRoot, "dist"), ".release-stage-"))
				}
				return nil
			}
			t.Cleanup(func() { beforeReleasePublishForTest = nil })

			if _, err := GenerateReleaseBundle(projectRoot, generationBinarySnapshot(t, projectRoot, binaryPath, digest), nil, DefaultReleaseBundleDir, ""); err == nil {
				t.Fatal("GenerateReleaseBundle accepted a mutated stage")
			}
			if _, err := os.Lstat(filepath.Join(projectRoot, DefaultReleaseBundleDir)); !os.IsNotExist(err) {
				t.Fatalf("mutated bundle was published: %v", err)
			}
		})
	}
}

func TestGenerateReleaseBundlePublishesDoggoMode0755(t *testing.T) {
	projectRoot := t.TempDir()
	binaryPath, binaryDigest := writeGenerationBinaryFixture(t, projectRoot, 0666)
	writeReleaseBundleFixture(t, projectRoot)
	originalSync := syncReleaseStage
	syncCalls := 0
	syncReleaseStage = func(root *os.Root) error {
		syncCalls++
		return project.SyncRootTree(root)
	}
	t.Cleanup(func() { syncReleaseStage = originalSync })

	if _, err := GenerateReleaseBundle(projectRoot, generationBinarySnapshot(t, projectRoot, binaryPath, binaryDigest), nil, DefaultReleaseBundleDir, ""); err != nil {
		t.Fatal(err)
	}
	if syncCalls != 3 {
		t.Fatalf("release stage tree sync calls = %d, want initial, post-hook, and post-chmod sync", syncCalls)
	}
	info, err := os.Stat(filepath.Join(projectRoot, DefaultReleaseBundleDir, "doggo"))
	if err != nil {
		t.Fatal(err)
	}
	wantMode := os.FileMode(0755)
	if runtime.GOOS == "windows" {
		wantMode = 0666
	}
	if got := info.Mode().Perm(); got != wantMode {
		t.Fatalf("logical doggo mode = %o, want %o", got, wantMode)
	}
	if runtime.GOOS != "windows" {
		directoryInfo, err := os.Stat(filepath.Join(projectRoot, DefaultReleaseBundleDir))
		if err != nil {
			t.Fatal(err)
		}
		if got := directoryInfo.Mode().Perm(); got != 0755 {
			t.Fatalf("release bundle directory mode = %o, want 755", got)
		}
	}
}

func findStage(t *testing.T, parent, prefix string) string {
	t.Helper()
	entries, err := os.ReadDir(parent)
	if err != nil {
		t.Fatal(err)
	}
	var stage string
	for _, entry := range entries {
		if !strings.HasPrefix(entry.Name(), prefix) {
			continue
		}
		if stage != "" {
			t.Fatalf("multiple release stages found in %s", parent)
		}
		stage = filepath.Join(parent, entry.Name())
	}
	if stage == "" {
		t.Fatalf("release stage not found in %s", parent)
	}
	return stage
}

func TestGenerateReleaseBundleRejectsRacingExtraAncestorWithoutOutsideReadOrWrite(t *testing.T) {
	projectRoot := t.TempDir()
	binaryPath, binaryDigest := writeGenerationBinaryFixture(t, projectRoot, 0755)
	extra := "extras/doggo.vsix"
	writeReleaseBundleFixture(t, projectRoot, extra)
	extraDir := filepath.Join(projectRoot, "extras")
	ownedExtraDir := extraDir + "-owned"
	called := false
	afterReleaseInputDirectoryOpenForTest = func(path string) {
		if filepath.ToSlash(path) != extra {
			return
		}
		called = true
		if err := os.Rename(extraDir, ownedExtraDir); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(extraDir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(extraDir, "doggo.vsix"), []byte("attacker"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { afterReleaseInputDirectoryOpenForTest = nil })

	_, err := GenerateReleaseBundle(projectRoot, generationBinarySnapshot(t, projectRoot, binaryPath, binaryDigest), []string{extra}, DefaultReleaseBundleDir, "")
	if err == nil {
		t.Fatal("GenerateReleaseBundle accepted a racing extra ancestor")
	}
	if !strings.Contains(err.Error(), "identity changed") {
		t.Fatalf("GenerateReleaseBundle did not detect the racing ancestor: %v", err)
	}
	if !called {
		t.Fatal("input-directory race hook was not called")
	}
	if content, err := os.ReadFile(filepath.Join(extraDir, "doggo.vsix")); err != nil || string(content) != "attacker" {
		t.Fatalf("attacker artifact changed: content=%q err=%v", content, err)
	}
	if _, err := os.Lstat(filepath.Join(projectRoot, DefaultReleaseBundleDir)); !os.IsNotExist(err) {
		t.Fatalf("bundle output created after source race: %v", err)
	}
}

func TestGenerateReleaseBundleRejectsRacingPackageLockAncestorBeforeSigning(t *testing.T) {
	projectRoot := t.TempDir()
	binaryPath, binaryDigest := writeGenerationBinaryFixture(t, projectRoot, 0755)
	writeReleaseBundleFixture(t, projectRoot)
	vscodeDir := filepath.Join(projectRoot, "editors", "vscode")
	ownedVSCodeDir := vscodeDir + "-owned"
	lockRel := filepath.ToSlash(filepath.Join("editors", "vscode", "package-lock.json"))
	afterReleaseInputDirectoryOpenForTest = func(path string) {
		if filepath.ToSlash(path) != lockRel {
			return
		}
		if err := os.Rename(vscodeDir, ownedVSCodeDir); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(vscodeDir, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(vscodeDir, "package-lock.json"), []byte(`{"lockfileVersion":3,"packages":{"node_modules/attacker":{"version":"9.9.9"}}}`), 0644); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { afterReleaseInputDirectoryOpenForTest = nil })

	if _, err := GenerateReleaseBundle(projectRoot, generationBinarySnapshot(t, projectRoot, binaryPath, binaryDigest), nil, DefaultReleaseBundleDir, ""); err == nil || !strings.Contains(err.Error(), "identity changed") {
		t.Fatalf("GenerateReleaseBundle error = %v", err)
	}
	if _, err := os.Lstat(filepath.Join(projectRoot, DefaultReleaseBundleDir)); !os.IsNotExist(err) {
		t.Fatalf("bundle output created after package-lock race: %v", err)
	}
}

func TestGenerateReleaseBundleRejectsDoggoRenamedOntoOutputAncestor(t *testing.T) {
	projectRoot := t.TempDir()
	binaryPath, binaryDigest := writeGenerationBinaryFixture(t, projectRoot, 0755)
	writeReleaseBundleFixture(t, projectRoot)
	doggoState := filepath.Join(projectRoot, ".doggo")
	outputParent := filepath.Join(projectRoot, "dist")
	beforeReleaseOutputParentOpenForTest = func() {
		if err := os.Rename(doggoState, outputParent); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { beforeReleaseOutputParentOpenForTest = nil })

	if _, err := GenerateReleaseBundle(projectRoot, generationBinarySnapshot(t, projectRoot, binaryPath, binaryDigest), nil, DefaultReleaseBundleDir, ""); err == nil {
		t.Fatal("GenerateReleaseBundle published into renamed generation state")
	}
	movedBinary := filepath.Join(outputParent, "generations", strings.Repeat("a", 64), "bin", "doggo")
	if got := fileSHA256(t, movedBinary); got != binaryDigest {
		t.Fatalf("generation binary changed: digest=%q", got)
	}
	if _, err := os.Lstat(filepath.Join(outputParent, "release")); !os.IsNotExist(err) {
		t.Fatalf("release output created in generation state: %v", err)
	}
}

func TestGenerateReleaseBundleRejectsRacingOutputAncestorAndPreservesRacer(t *testing.T) {
	projectRoot := t.TempDir()
	binaryPath, binaryDigest := writeGenerationBinaryFixture(t, projectRoot, 0755)
	writeReleaseBundleFixture(t, projectRoot)
	dist := filepath.Join(projectRoot, "dist")
	ownedDist := dist + "-owned"
	outside := t.TempDir()
	if err := os.Mkdir(filepath.Join(outside, "release"), 0755); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(outside, "release", "racer")
	if err := os.WriteFile(marker, []byte("racer"), 0644); err != nil {
		t.Fatal(err)
	}
	called := false
	replaced := false
	afterReleaseOutputRootOpenForTest = func() {
		called = true
		replaced = testutil.RenameRetained(t, dist, ownedDist)
		if !replaced {
			return
		}
		if err := os.Symlink(outside, dist); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { afterReleaseOutputRootOpenForTest = nil })

	_, generateErr := GenerateReleaseBundle(projectRoot, generationBinarySnapshot(t, projectRoot, binaryPath, binaryDigest), nil, DefaultReleaseBundleDir, "")
	if replaced && generateErr == nil {
		t.Fatal("GenerateReleaseBundle accepted a racing output ancestor")
	}
	if !replaced && generateErr != nil {
		t.Fatalf("generation after blocked output ancestor replacement: %v", generateErr)
	}
	if !called {
		t.Fatal("output-open race hook was not called")
	}
	entries, err := os.ReadDir(filepath.Join(outside, "release"))
	if err != nil || len(entries) != 1 || entries[0].Name() != "racer" {
		t.Fatalf("racer output changed: entries=%v err=%v", entries, err)
	}
	publishedDist := dist
	if replaced {
		publishedDist = ownedDist
	}
	published, err := os.ReadFile(filepath.Join(publishedDist, "release", "doggo"))
	if err != nil || fileSHA256(t, filepath.Join(publishedDist, "release", "doggo")) != binaryDigest {
		t.Fatalf("retained published doggo changed: content=%q err=%v", published, err)
	}
}

func TestGenerateReleaseBundleNeverSignsRacingLogicalDoggo(t *testing.T) {
	projectRoot := t.TempDir()
	binaryPath, binaryDigest := writeGenerationBinaryFixture(t, projectRoot, 0755)
	writeReleaseBundleFixture(t, projectRoot)
	keyDir := filepath.Join(projectRoot, "keys")
	privateKeyPath := filepath.Join(keyDir, PrivateKeyFileName)
	publicKeyPath := filepath.Join(keyDir, PublicKeyFileName)
	if err := GenerateKeyPair(keyDir); err != nil {
		t.Fatal(err)
	}
	attacker := []byte("attacker")
	called := false
	afterReleaseDoggoWriteForTest = func(outputRoot string) {
		called = true
		doggo := filepath.Join(outputRoot, "doggo")
		if err := os.Rename(doggo, doggo+"-owned"); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(doggo, attacker, 0755); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { afterReleaseDoggoWriteForTest = nil })

	if _, err := GenerateReleaseBundle(projectRoot, generationBinarySnapshot(t, projectRoot, binaryPath, binaryDigest), nil, DefaultReleaseBundleDir, privateKeyPath); err == nil {
		t.Fatal("GenerateReleaseBundle accepted a replaced logical doggo")
	}
	if !called {
		t.Fatal("doggo-write race hook was not called")
	}
	sums, err := os.ReadFile(filepath.Join(projectRoot, DefaultReleaseBundleDir, sha256SumsFileName))
	if err != nil {
		t.Fatal(err)
	}
	attackerSum := sha256.Sum256(attacker)
	if strings.Contains(string(sums), hex.EncodeToString(attackerSum[:])) || !strings.Contains(string(sums), binaryDigest) {
		t.Fatalf("SHA256SUMS signed racing doggo: %s", sums)
	}
	signature, err := os.ReadFile(filepath.Join(projectRoot, DefaultReleaseBundleDir, sha256SigFileName))
	if err != nil {
		t.Fatal(err)
	}
	publicKey, err := loadEd25519PublicKey(publicKeyPath)
	if err != nil {
		t.Fatal(err)
	}
	if !ed25519.Verify(publicKey, sums, signature) {
		t.Fatal("signature does not cover the validated SHA256SUMS")
	}
}

func TestGenerateKeyPairRejectsExistingKeyDirectoryWithoutMutation(t *testing.T) {
	root := t.TempDir()
	keyDir := filepath.Join(root, "keys")
	marker := filepath.Join(keyDir, "marker")
	if err := os.Mkdir(keyDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(marker, []byte("preserve"), 0644); err != nil {
		t.Fatal(err)
	}

	err := GenerateKeyPair(keyDir)
	if err == nil || !strings.Contains(err.Error(), "already exists") {
		t.Fatalf("GenerateKeyPair error = %v, want existing-directory rejection", err)
	}
	content, readErr := os.ReadFile(marker)
	if readErr != nil || string(content) != "preserve" {
		t.Fatalf("existing key directory changed: content=%q err=%v", content, readErr)
	}
	entries, readDirErr := os.ReadDir(keyDir)
	if readDirErr != nil || len(entries) != 1 || entries[0].Name() != "marker" {
		t.Fatalf("existing key directory entries = %v, err=%v", entries, readDirErr)
	}
}

func TestGenerateKeyPairFailureNeverPublishesOnlyOneKey(t *testing.T) {
	root := t.TempDir()
	keyDir := filepath.Join(root, "keys")
	beforeReleaseKeyPublishForTest = func(kind string) error {
		if kind == "directory" {
			return errors.New("injected key publication failure")
		}
		return nil
	}
	t.Cleanup(func() { beforeReleaseKeyPublishForTest = nil })

	err := GenerateKeyPair(keyDir)
	if err == nil || !strings.Contains(err.Error(), "injected key publication failure") {
		t.Fatalf("GenerateKeyPair error = %v", err)
	}
	if _, statErr := os.Lstat(keyDir); !os.IsNotExist(statErr) {
		t.Fatalf("key directory exists after pre-publication failure: %v", statErr)
	}
}

func TestGenerateKeyPairDoesNotAdoptDirectoryAppearingAtPublish(t *testing.T) {
	root := t.TempDir()
	keyDir := filepath.Join(root, "keys")
	marker := filepath.Join(keyDir, "marker")
	beforeReleaseKeyPublishForTest = func(kind string) error {
		if kind != "directory" {
			return nil
		}
		if err := os.Mkdir(keyDir, 0755); err != nil {
			t.Fatal(err)
		}
		return os.WriteFile(marker, []byte("racer"), 0644)
	}
	t.Cleanup(func() { beforeReleaseKeyPublishForTest = nil })

	if err := GenerateKeyPair(keyDir); err == nil {
		t.Fatalf("GenerateKeyPair error = %v", err)
	}
	content, err := os.ReadFile(marker)
	if err != nil || string(content) != "racer" {
		t.Fatalf("racing key directory changed: content=%q err=%v", content, err)
	}
	entries, err := os.ReadDir(keyDir)
	if err != nil || len(entries) != 1 || entries[0].Name() != "marker" {
		t.Fatalf("racing key directory entries = %v, err=%v", entries, err)
	}
}

func TestGenerateKeyPairRejectsStageMutationAtPublish(t *testing.T) {
	for _, mutation := range []string{"remove public", "replace public", "weaken private mode"} {
		t.Run(mutation, func(t *testing.T) {
			if mutation == "weaken private mode" && runtime.GOOS == "windows" {
				t.Skip("Windows chmod does not represent Unix private-key permission bits")
			}
			root := t.TempDir()
			keyDir := filepath.Join(root, "keys")
			beforeReleaseKeyPublishForTest = func(kind string) error {
				if kind != "directory" {
					return nil
				}
				entries, err := os.ReadDir(root)
				if err != nil {
					return err
				}
				for _, entry := range entries {
					if !strings.HasPrefix(entry.Name(), ".release-key-stage-") {
						continue
					}
					stage := filepath.Join(root, entry.Name())
					switch mutation {
					case "remove public":
						return os.Remove(filepath.Join(stage, PublicKeyFileName))
					case "replace public":
						return os.WriteFile(filepath.Join(stage, PublicKeyFileName), []byte("attacker"), 0644)
					case "weaken private mode":
						return os.Chmod(filepath.Join(stage, PrivateKeyFileName), 0644)
					}
				}
				return errors.New("key stage not found")
			}
			t.Cleanup(func() { beforeReleaseKeyPublishForTest = nil })

			if err := GenerateKeyPair(keyDir); err == nil {
				t.Fatal("GenerateKeyPair published a mutated key stage")
			}
			if _, err := os.Lstat(keyDir); !os.IsNotExist(err) {
				t.Fatalf("mutated key directory published: %v", err)
			}
			entries, err := os.ReadDir(root)
			if err != nil {
				t.Fatal(err)
			}
			for _, entry := range entries {
				if strings.HasPrefix(entry.Name(), ".release-key-stage-") {
					t.Fatalf("mutated key stage leaked: %s", entry.Name())
				}
			}
		})
	}
}

func TestGenerateKeyPairResynchronizesExactHookRewriteBeforePublish(t *testing.T) {
	root := t.TempDir()
	keyDir := filepath.Join(root, "keys")
	beforeReleaseKeyPublishForTest = func(kind string) error {
		if kind != "directory" {
			return nil
		}
		stage := findStage(t, root, ".release-key-stage-")
		for _, name := range []string{PrivateKeyFileName, PublicKeyFileName} {
			path := filepath.Join(stage, name)
			content, err := os.ReadFile(path)
			if err != nil {
				return err
			}
			info, err := os.Stat(path)
			if err != nil {
				return err
			}
			if err := os.WriteFile(path, content, info.Mode().Perm()); err != nil {
				return err
			}
		}
		return nil
	}
	t.Cleanup(func() { beforeReleaseKeyPublishForTest = nil })
	originalSync := syncReleaseStage
	syncCalls := 0
	want := errors.New("injected post-hook key stage sync failure")
	syncReleaseStage = func(stage *os.Root) error {
		syncCalls++
		if syncCalls == 2 {
			return want
		}
		return project.SyncRootTree(stage)
	}
	t.Cleanup(func() { syncReleaseStage = originalSync })

	err := GenerateKeyPair(keyDir)
	if !errors.Is(err, want) {
		t.Fatalf("GenerateKeyPair error = %v, want post-hook sync failure", err)
	}
	if syncCalls != 2 {
		t.Fatalf("key stage tree sync calls = %d, want initial and post-hook sync", syncCalls)
	}
	if _, statErr := os.Lstat(keyDir); !os.IsNotExist(statErr) {
		t.Fatalf("key directory published after post-hook sync failure: %v", statErr)
	}
}

func TestGenerateKeyPairRejectsReplacedStageWithoutPublishingOneKey(t *testing.T) {
	root := t.TempDir()
	keyDir := filepath.Join(root, "keys")
	var racerStage string
	afterReleaseDirectoryCreateForTest = func(kind, name string) {
		if kind != "key-stage" {
			return
		}
		racerStage = filepath.Join(root, name)
		if err := os.Rename(racerStage, racerStage+"-owned"); err != nil {
			t.Fatal(err)
		}
		if err := os.Mkdir(racerStage, 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(racerStage, PrivateKeyFileName), []byte("racer-secret"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	t.Cleanup(func() { afterReleaseDirectoryCreateForTest = nil })

	err := GenerateKeyPair(keyDir)
	if err == nil || !strings.Contains(err.Error(), "changed") {
		t.Fatalf("GenerateKeyPair error = %v", err)
	}
	if _, statErr := os.Lstat(keyDir); !os.IsNotExist(statErr) {
		t.Fatalf("key directory published after stage replacement: %v", statErr)
	}
	if content, readErr := os.ReadFile(filepath.Join(racerStage, PrivateKeyFileName)); readErr != nil || string(content) != "racer-secret" {
		t.Fatalf("racer stage changed: content=%q err=%v", content, readErr)
	}
}

func TestGenerateKeyPairPublishesMatchingUsablePair(t *testing.T) {
	root := t.TempDir()
	keyDir := filepath.Join(root, "keys")
	privatePath := filepath.Join(keyDir, PrivateKeyFileName)
	publicPath := filepath.Join(keyDir, PublicKeyFileName)
	if err := GenerateKeyPair(keyDir); err != nil {
		t.Fatal(err)
	}
	privateKey, err := loadEd25519PrivateKey(privatePath)
	if err != nil {
		t.Fatal(err)
	}
	publicKey, err := loadEd25519PublicKey(publicPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(privateKey.Public().(ed25519.PublicKey), publicKey) {
		t.Fatal("published key pair does not match")
	}
}

func TestLoadEd25519KeysRejectInvalidPEM(t *testing.T) {
	root := t.TempDir()
	privateKeyPath := filepath.Join(root, "private.pem")
	publicKeyPath := filepath.Join(root, "public.pem")
	if err := os.WriteFile(privateKeyPath, []byte("not pem"), 0644); err != nil {
		t.Fatalf("write private key fixture: %v", err)
	}
	if err := os.WriteFile(publicKeyPath, []byte("not pem"), 0644); err != nil {
		t.Fatalf("write public key fixture: %v", err)
	}

	if _, err := loadEd25519PrivateKey(privateKeyPath); err == nil || err.Error() != "invalid private key PEM: "+privateKeyPath {
		t.Fatalf("loadEd25519PrivateKey error = %v", err)
	}
	if _, err := loadEd25519PublicKey(publicKeyPath); err == nil || err.Error() != "invalid public key PEM: "+publicKeyPath {
		t.Fatalf("loadEd25519PublicKey error = %v", err)
	}
}

func TestLoadEd25519KeysRejectWrongKeyType(t *testing.T) {
	root := t.TempDir()
	privateKeyPath := filepath.Join(root, "private.pem")
	publicKeyPath := filepath.Join(root, "public.pem")

	rsaKey, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatalf("generate rsa key: %v", err)
	}
	privateKeyDER, err := x509.MarshalPKCS8PrivateKey(rsaKey)
	if err != nil {
		t.Fatalf("marshal rsa private key: %v", err)
	}
	publicKeyDER, err := x509.MarshalPKIXPublicKey(&rsaKey.PublicKey)
	if err != nil {
		t.Fatalf("marshal rsa public key: %v", err)
	}
	if err := os.WriteFile(privateKeyPath, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateKeyDER}), 0644); err != nil {
		t.Fatalf("write private key fixture: %v", err)
	}
	if err := os.WriteFile(publicKeyPath, pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: publicKeyDER}), 0644); err != nil {
		t.Fatalf("write public key fixture: %v", err)
	}

	if _, err := loadEd25519PrivateKey(privateKeyPath); err == nil || err.Error() != "private key is not ed25519: "+privateKeyPath {
		t.Fatalf("loadEd25519PrivateKey error = %v", err)
	}
	if _, err := loadEd25519PublicKey(publicKeyPath); err == nil || err.Error() != "public key is not ed25519: "+publicKeyPath {
		t.Fatalf("loadEd25519PublicKey error = %v", err)
	}
}

func TestGenerateGoModuleSBOMUsesShippedBinaryBuildInfo(t *testing.T) {
	binaryPath, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	binary, err := os.ReadFile(binaryPath)
	if err != nil {
		t.Fatal(err)
	}
	info, err := buildinfo.Read(bytes.NewReader(binary))
	if err != nil {
		t.Fatal(err)
	}
	content, err := generateGoModuleSBOM(binary, "2026-01-01T00:00:00Z")
	if err != nil {
		t.Fatalf("generateGoModuleSBOM failed: %v", err)
	}
	var sbom goModuleSBOM
	if err := json.Unmarshal(content, &sbom); err != nil {
		t.Fatalf("parse sbom json: %v", err)
	}
	if sbom.Schema != "doggo-sbom-go-modules-v1" {
		t.Fatalf("unexpected schema: %s", sbom.Schema)
	}
	if len(sbom.Components) != len(info.Deps)+1 {
		t.Fatalf("components = %d, want main + %d linked modules", len(sbom.Components), len(info.Deps))
	}
	for _, dependency := range info.Deps {
		if !containsGoModule(sbom.Components, dependency.Path, dependency.Version) {
			t.Fatalf("linked module missing from SBOM: %s@%s", dependency.Path, dependency.Version)
		}
	}
	again, err := generateGoModuleSBOM(binary, "2026-01-01T00:00:00Z")
	if err != nil || !bytes.Equal(content, again) {
		t.Fatalf("Go module SBOM is not deterministic: err=%v", err)
	}
}

func TestGenerateGoModuleSBOMRejectsBinaryWithoutGoBuildInfo(t *testing.T) {
	if _, err := generateGoModuleSBOM([]byte("not a Go binary"), "2026-01-01T00:00:00Z"); err == nil || !strings.Contains(err.Error(), "Go build info") {
		t.Fatalf("generateGoModuleSBOM error = %v", err)
	}
}

func TestModuleReplacementUsesBuildInfoIdentity(t *testing.T) {
	if got := moduleReplacement(&debug.Module{Path: "example.com/replacement", Version: "v1.2.3"}); got != "example.com/replacement@v1.2.3" {
		t.Fatalf("versioned replacement = %q", got)
	}
	if got := moduleReplacement(&debug.Module{Path: "../local"}); got != "../local" {
		t.Fatalf("local replacement = %q", got)
	}
}

func TestGenerateNPMPackageSBOMParsesPackageLock(t *testing.T) {
	lockJSON := `{
  "name": "doggo-vscode",
  "lockfileVersion": 3,
  "packages": {
    "": {"name":"doggo-vscode","version":"0.1.0"},
    "node_modules/foo": {"version":"1.2.3","resolved":"https://example.com/foo.tgz","integrity":"sha512-abc"},
    "node_modules/@scope/bar": {"version":"4.5.6","dev":true},
    "node_modules/glob/node_modules/minimatch": {"version":"9.0.0"},
    "node_modules/outer/node_modules/@nested/pkg": {"version":"2.3.4"}
  }
}`
	content, err := generateNPMPackageSBOM([]byte(lockJSON), "2026-01-01T00:00:00Z")
	if err != nil {
		t.Fatalf("generateNPMPackageSBOM failed: %v", err)
	}
	var sbom npmPackageSBOM
	if err := json.Unmarshal(content, &sbom); err != nil {
		t.Fatalf("parse npm sbom json: %v", err)
	}
	if sbom.Schema != "doggo-sbom-npm-packages-v1" {
		t.Fatalf("unexpected schema: %s", sbom.Schema)
	}
	if len(sbom.Components) != 4 {
		t.Fatalf("expected 4 components, got %d", len(sbom.Components))
	}
	versions := make(map[string]string, len(sbom.Components))
	for _, component := range sbom.Components {
		versions[component.Name] = component.Version
	}
	for name, want := range map[string]string{"minimatch": "9.0.0", "@nested/pkg": "2.3.4"} {
		if got := versions[name]; got != want {
			t.Fatalf("nested component %q version = %q, want %q; all=%v", name, got, want, versions)
		}
	}
}

func TestGenerateAndCheckReleaseIntegrityFiles(t *testing.T) {
	projectRoot := t.TempDir()
	binaryPath, binaryDigest := writeGenerationBinaryFixture(t, projectRoot, 0755)
	extra := "editors/vscode/dist/doggo-vscode.vsix"
	extraPath := filepath.Join(projectRoot, filepath.FromSlash(extra))
	if err := os.MkdirAll(filepath.Dir(extraPath), 0755); err != nil {
		t.Fatalf("mkdir artifact dir: %v", err)
	}
	if err := os.WriteFile(extraPath, []byte("content-"+extra), 0644); err != nil {
		t.Fatalf("write artifact: %v", err)
	}

	lockPath := filepath.Join(projectRoot, "editors/vscode/package-lock.json")
	if err := os.MkdirAll(filepath.Dir(lockPath), 0755); err != nil {
		t.Fatalf("mkdir lock dir: %v", err)
	}
	lockJSON := `{
  "name": "doggo-vscode",
  "lockfileVersion": 3,
  "packages": {
    "node_modules/foo": {"version":"1.0.0"}
  }
}`
	if err := os.WriteFile(lockPath, []byte(lockJSON), 0644); err != nil {
		t.Fatalf("write package-lock: %v", err)
	}
	if err := os.WriteFile(filepath.Join(projectRoot, "go.mod"), []byte("module doggo\n\ngo 1.24.0\n"), 0644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}

	keyDir := filepath.Join(projectRoot, "keys")
	privateKeyPath := filepath.Join(keyDir, PrivateKeyFileName)
	publicKeyPath := filepath.Join(keyDir, PublicKeyFileName)
	if err := GenerateKeyPair(keyDir); err != nil {
		t.Fatalf("GenerateKeyPair failed: %v", err)
	}

	out, err := GenerateReleaseBundle(projectRoot, generationBinarySnapshot(t, projectRoot, binaryPath, binaryDigest), []string{extra}, DefaultReleaseBundleDir, privateKeyPath)
	if err != nil {
		t.Fatalf("GenerateReleaseBundle failed: %v", err)
	}
	if out.SignaturePath == "" {
		t.Fatalf("expected signature path to be populated")
	}

	if err := CheckReleaseIntegrityFiles(projectRoot, DefaultReleaseBundleDir, publicKeyPath, true); err != nil {
		t.Fatalf("CheckReleaseIntegrityFiles failed: %v", err)
	}
}

func TestGenerateReleaseBundleRejectsExtraOutsideProjectRoot(t *testing.T) {
	projectRoot := t.TempDir()
	binaryPath, binaryDigest := writeGenerationBinaryFixture(t, projectRoot, 0755)
	writeReleaseBundleFixture(t, projectRoot)
	outsideDir := t.TempDir()
	outsideFile := filepath.Join(outsideDir, "outside.bin")
	if err := os.WriteFile(outsideFile, []byte("outside"), 0644); err != nil {
		t.Fatalf("write outside file: %v", err)
	}

	_, err := GenerateReleaseBundle(projectRoot, generationBinarySnapshot(t, projectRoot, binaryPath, binaryDigest), []string{outsideFile}, DefaultReleaseBundleDir, "")
	if err == nil {
		t.Fatalf("expected error for artifact outside project root")
	}
	if !strings.Contains(err.Error(), "outside project root") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGenerateReleaseBundle_RejectsOutDirOutsideProjectRoot(t *testing.T) {
	projectRoot := t.TempDir()
	outsideDir := t.TempDir()

	lockPath := filepath.Join(projectRoot, "editors/vscode/package-lock.json")
	if err := os.MkdirAll(filepath.Dir(lockPath), 0755); err != nil {
		t.Fatalf("mkdir lock dir: %v", err)
	}
	if err := os.WriteFile(lockPath, []byte(`{"lockfileVersion":3,"packages":{}}`), 0644); err != nil {
		t.Fatalf("write package-lock: %v", err)
	}
	artifactPath := filepath.Join(projectRoot, "generation", "doggo")
	if err := os.MkdirAll(filepath.Dir(artifactPath), 0755); err != nil {
		t.Fatalf("mkdir artifact dir: %v", err)
	}
	if err := os.WriteFile(artifactPath, []byte("doggo"), 0644); err != nil {
		t.Fatalf("write artifact: %v", err)
	}

	_, err := GenerateReleaseBundle(projectRoot, generationBinarySnapshot(t, projectRoot, artifactPath, fileSHA256(t, artifactPath)), nil, outsideDir, "")
	if err == nil {
		t.Fatalf("expected error for out-dir outside project root")
	}
	if !strings.Contains(err.Error(), "outside project root") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestGenerateReleaseBundle_RejectsOutDirViaSymlinkEscape(t *testing.T) {
	projectRoot := t.TempDir()
	outsideDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(projectRoot, "go.mod"), []byte("module doggo\n\ngo 1.24.0\n"), 0644); err != nil {
		t.Fatalf("write go.mod: %v", err)
	}

	lockPath := filepath.Join(projectRoot, "editors/vscode/package-lock.json")
	if err := os.MkdirAll(filepath.Dir(lockPath), 0755); err != nil {
		t.Fatalf("mkdir lock dir: %v", err)
	}
	if err := os.WriteFile(lockPath, []byte(`{"lockfileVersion":3,"packages":{}}`), 0644); err != nil {
		t.Fatalf("write package-lock: %v", err)
	}
	artifactPath := filepath.Join(projectRoot, "generation", "doggo")
	if err := os.MkdirAll(filepath.Dir(artifactPath), 0755); err != nil {
		t.Fatalf("mkdir artifact dir: %v", err)
	}
	if err := os.WriteFile(artifactPath, []byte("doggo"), 0644); err != nil {
		t.Fatalf("write artifact: %v", err)
	}

	distPath := filepath.Join(projectRoot, "dist")
	if err := os.Symlink(outsideDir, distPath); err != nil {
		t.Fatalf("symlink dist -> outside: %v", err)
	}

	_, err := GenerateReleaseBundle(projectRoot, generationBinarySnapshot(t, projectRoot, artifactPath, fileSHA256(t, artifactPath)), nil, "dist/release", "")
	if err == nil {
		t.Fatalf("expected error for out-dir symlink escape")
	}
	if !strings.Contains(err.Error(), "not ordinary") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestResolveProjectRelativePathErrors(t *testing.T) {
	projectRoot := t.TempDir()

	if _, err := resolveProjectRelativePath(projectRoot, ""); err == nil || err.Error() != "path is empty" {
		t.Fatalf("resolveProjectRelativePath empty path error = %v", err)
	}

	outsideDir := t.TempDir()
	outsideFile := filepath.Join(outsideDir, "artifact.bin")
	if err := os.WriteFile(outsideFile, []byte("artifact"), 0644); err != nil {
		t.Fatalf("write outside file: %v", err)
	}
	canonicalOutsideFile, err := project.NormalizeSystemPath(outsideFile)
	if err != nil {
		t.Fatal(err)
	}
	canonicalOutsideDir, err := project.NormalizeSystemPath(outsideDir)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := resolveProjectRelativePath(projectRoot, outsideFile); err == nil || !strings.Contains(err.Error(), "path "+filepath.ToSlash(canonicalOutsideFile)+" is outside project root ") {
		t.Fatalf("resolveProjectRelativePath outside path error = %v", err)
	}
	if _, err := resolveProjectRelativePath(projectRoot, outsideDir); err == nil || !strings.Contains(err.Error(), "path "+filepath.ToSlash(canonicalOutsideDir)+" is outside project root ") {
		t.Fatalf("resolveProjectRelativePath outside path error = %v", err)
	}
}

func writeReleaseBundleFixture(t *testing.T, projectRoot string, extras ...string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(projectRoot, "go.mod"), []byte("module example.com/release\n\ngo 1.24.0\n"), 0644); err != nil {
		t.Fatal(err)
	}
	for _, extra := range extras {
		path := filepath.Join(projectRoot, filepath.FromSlash(extra))
		if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("extra:"+extra), 0644); err != nil {
			t.Fatal(err)
		}
	}
	lockPath := filepath.Join(projectRoot, "editors", "vscode", "package-lock.json")
	if err := os.MkdirAll(filepath.Dir(lockPath), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(lockPath, []byte(`{"lockfileVersion":3,"packages":{}}`), 0644); err != nil {
		t.Fatal(err)
	}
}

func writeGenerationBinaryFixture(t *testing.T, projectRoot string, mode os.FileMode) (string, string) {
	t.Helper()
	binaryPath := filepath.Join(projectRoot, ".doggo", "generations", strings.Repeat("a", 64), "bin", "doggo")
	if err := os.MkdirAll(filepath.Dir(binaryPath), 0755); err != nil {
		t.Fatal(err)
	}
	testBinary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(testBinary)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(binaryPath, content, mode); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(binaryPath, mode); err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(content)
	return binaryPath, hex.EncodeToString(sum[:])
}

func generationBinarySnapshot(t *testing.T, projectRoot, binaryPath, digest string) project.GenerationBinarySnapshot {
	t.Helper()
	content, err := os.ReadFile(binaryPath)
	if err != nil {
		t.Fatal(err)
	}
	projectPath, err := filepath.Rel(projectRoot, binaryPath)
	if err != nil {
		t.Fatal(err)
	}
	return project.GenerationBinarySnapshot{ProjectPath: filepath.ToSlash(projectPath), SHA256: digest, Content: content}
}

func containsString(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func containsGoModule(components []goModuleComponent, name, version string) bool {
	for _, component := range components {
		if component.Name == name && component.Version == version {
			return true
		}
	}
	return false
}

func fileSHA256(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}
