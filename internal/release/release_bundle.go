package release

import (
	"bufio"
	"bytes"
	"crypto/ed25519"
	cryptorand "crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"debug/buildinfo"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"doggo/internal/project"
)

const (
	DefaultReleaseBundleDir = "dist/release"
	sha256SumsFileName      = "SHA256SUMS"
	sha256SigFileName       = "SHA256SUMS.sig"
	goSBOMFileName          = "sbom-cli-go-modules.json"
	vsixSBOMFileName        = "sbom-vscode-npm-packages.json"
	manifestFileName        = "release-bundle.json"
	PrivateKeyFileName      = "doggo-release-private.pem"
	PublicKeyFileName       = "doggo-release-public.pem"
)

var (
	afterReleaseInputDirectoryOpenForTest func(path string)
	beforeReleaseOutputParentOpenForTest  func()
	afterReleaseOutputRootOpenForTest     func()
	afterReleaseDoggoWriteForTest         func(outputRoot string)
	afterReleaseDirectoryCreateForTest    func(kind, name string)
	beforeReleasePublishForTest           func(name string) error
	beforeReleaseKeyPublishForTest        func(kind string) error
	syncReleaseStage                      = project.SyncRootTree
)

type ArtifactDigest struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

type ReleaseBundleOutputs struct {
	SHA256SumsPath string           `json:"sha256SumsPath"`
	SignaturePath  string           `json:"signaturePath,omitempty"`
	GoSBOMPath     string           `json:"goSbomPath"`
	VSCodeSBOMPath string           `json:"vsCodeSbomPath"`
	ManifestPath   string           `json:"manifestPath"`
	Artifacts      []ArtifactDigest `json:"artifacts"`
}

type goModuleComponent struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Replace string `json:"replace,omitempty"`
	Main    bool   `json:"main,omitempty"`
}

type goModuleSBOM struct {
	Schema      string              `json:"schema"`
	GeneratedAt string              `json:"generatedAt"`
	Tool        string              `json:"tool"`
	Components  []goModuleComponent `json:"components"`
}

type npmPackageComponent struct {
	Name      string `json:"name"`
	Version   string `json:"version"`
	Resolved  string `json:"resolved,omitempty"`
	Integrity string `json:"integrity,omitempty"`
	Dev       bool   `json:"dev,omitempty"`
}

type npmPackageSBOM struct {
	Schema      string                `json:"schema"`
	GeneratedAt string                `json:"generatedAt"`
	Tool        string                `json:"tool"`
	Components  []npmPackageComponent `json:"components"`
}

type releaseBundleManifest struct {
	Schema      string           `json:"schema"`
	GeneratedAt string           `json:"generatedAt"`
	SHA256Sums  string           `json:"sha256Sums"`
	Signature   string           `json:"signature,omitempty"`
	GoSBOM      string           `json:"goSbom"`
	VSCodeSBOM  string           `json:"vsCodeSbom"`
	Artifacts   []ArtifactDigest `json:"artifacts"`
}

type npmLockFile struct {
	Packages map[string]struct {
		Version   string `json:"version"`
		Resolved  string `json:"resolved"`
		Integrity string `json:"integrity"`
		Dev       bool   `json:"dev"`
	} `json:"packages"`
	Dependencies map[string]struct {
		Version string `json:"version"`
	} `json:"dependencies"`
}

type bundleFile struct {
	name string
	data []byte
	mode fs.FileMode
}

type preparedArtifact struct {
	digest ArtifactDigest
	data   []byte
}

type retainedAbsoluteParent struct {
	rootPath  string
	root      *os.Root
	directory *project.RetainedDirectory
	name      string
}

func GenerateReleaseBundle(projectRoot string, binarySnapshot project.GenerationBinarySnapshot, artifactPaths []string, outDir, privateKeyPath string) (_ *ReleaseBundleOutputs, err error) {
	binarySum := sha256.Sum256(binarySnapshot.Content)
	binaryDigest := hex.EncodeToString(binarySum[:])
	if !project.IsLowerHexDigest(binarySnapshot.SHA256) {
		return nil, fmt.Errorf("invalid generation binary digest: %s", binarySnapshot.SHA256)
	}
	if binaryDigest != binarySnapshot.SHA256 {
		return nil, fmt.Errorf("generation binary digest mismatch: expected %s, got %s", binarySnapshot.SHA256, binaryDigest)
	}
	binary := binarySnapshot.Content
	root, rootPath, err := project.OpenVerifiedRoot(projectRoot)
	if err != nil {
		return nil, fmt.Errorf("open project root: %w", err)
	}
	defer func() { err = errors.Join(err, root.Close()) }()
	binaryRel, err := resolveProjectRelativePath(rootPath, binarySnapshot.ProjectPath)
	if err != nil {
		return nil, fmt.Errorf("resolve generation binary project path: %w", err)
	}
	outputRel, err := resolveProjectRelativePath(rootPath, outDir)
	if err != nil {
		return nil, fmt.Errorf("resolve release bundle output directory: %w", err)
	}
	if err := rejectGenerationOutput(outputRel); err != nil {
		return nil, err
	}
	generationState, err := openOptionalGenerationState(root)
	if err != nil {
		return nil, fmt.Errorf("retain .doggo generation state: %w", err)
	}
	if generationState != nil {
		defer func() { err = errors.Join(err, generationState.Close()) }()
	}
	existingOutput, err := project.OpenRetainedDirectory(root, outputRel, 0, generationIdentity(generationState))
	if err == nil {
		return nil, errors.Join(
			fmt.Errorf("release bundle output directory already exists: %s; use a new immutable/versioned --out-dir", outputRel),
			existingOutput.Close(),
		)
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("inspect release bundle output path: %w", err)
	}
	artifacts, err := collectArtifacts(root, rootPath, artifactPaths, binaryRel, binary, binaryDigest)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	goSBOMContent, err := generateGoModuleSBOM(binary, now)
	if err != nil {
		return nil, err
	}
	packageLock, _, err := readRegularFileRoot(root, filepath.Join("editors", "vscode", "package-lock.json"))
	if err != nil {
		return nil, fmt.Errorf("read npm package-lock.json: %w", err)
	}
	vsixSBOMContent, err := generateNPMPackageSBOM(packageLock, now)
	if err != nil {
		return nil, err
	}

	artifactDigests := make([]ArtifactDigest, len(artifacts))
	for i, artifact := range artifacts {
		artifactDigests[i] = artifact.digest
	}
	manifest := releaseBundleManifest{
		Schema:      "doggo-release-bundle-v1",
		GeneratedAt: now,
		SHA256Sums:  sha256SumsFileName,
		GoSBOM:      goSBOMFileName,
		VSCodeSBOM:  vsixSBOMFileName,
		Artifacts:   artifactDigests,
	}
	var privateKey ed25519.PrivateKey
	if strings.TrimSpace(privateKeyPath) != "" {
		privateKey, err = loadEd25519PrivateKey(privateKeyPath)
		if err != nil {
			return nil, err
		}
		manifest.Signature = sha256SigFileName
	}
	manifestContent, err := marshalJSON(manifest, "release bundle manifest")
	if err != nil {
		return nil, err
	}
	checksummed := append([]ArtifactDigest{}, artifactDigests...)
	checksummed = append(checksummed,
		digestFor(goSBOMFileName, goSBOMContent),
		digestFor(vsixSBOMFileName, vsixSBOMContent),
		digestFor(manifestFileName, manifestContent),
	)
	sort.Slice(checksummed, func(i, j int) bool { return checksummed[i].Path < checksummed[j].Path })
	shaContent := marshalSHA256Sums(checksummed)
	var signature []byte
	if privateKey != nil {
		signature = ed25519.Sign(privateKey, shaContent)
	}

	files := make([]bundleFile, 0, len(artifacts)+5)
	for _, artifact := range artifacts {
		mode := fs.FileMode(0644)
		if artifact.digest.Path == "doggo" {
			mode = 0755
		}
		files = append(files, bundleFile{name: artifact.digest.Path, data: artifact.data, mode: mode})
	}
	files = append(files,
		bundleFile{name: goSBOMFileName, data: goSBOMContent, mode: 0644},
		bundleFile{name: vsixSBOMFileName, data: vsixSBOMContent, mode: 0644},
		bundleFile{name: sha256SumsFileName, data: shaContent, mode: 0644},
	)
	if signature != nil {
		files = append(files, bundleFile{name: sha256SigFileName, data: signature, mode: 0644})
	}
	files = append(files, bundleFile{name: manifestFileName, data: manifestContent, mode: 0644})

	if err := project.VerifyRootPath(rootPath, root); err != nil {
		return nil, fmt.Errorf("verify project root before staging release bundle: %w", err)
	}
	if err := verifyGenerationState(generationState); err != nil {
		return nil, fmt.Errorf("verify .doggo generation state before staging release bundle: %w", err)
	}
	if beforeReleaseOutputParentOpenForTest != nil {
		beforeReleaseOutputParentOpenForTest()
	}
	outputParentRel := filepath.ToSlash(filepath.Dir(filepath.FromSlash(outputRel)))
	parentDirectory, err := project.OpenRetainedDirectory(root, outputParentRel, 0755, generationIdentity(generationState))
	if err != nil {
		return nil, fmt.Errorf("open release bundle parent directory: %w", err)
	}
	defer func() { err = errors.Join(err, parentDirectory.Close()) }()
	parentRoot := parentDirectory.Root()
	outputName := filepath.Base(filepath.FromSlash(outputRel))
	if _, err := parentRoot.Lstat(outputName); err == nil {
		return nil, fmt.Errorf("%w: release bundle output appeared after preflight; use a new immutable/versioned --out-dir", project.ErrOwnershipChanged)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return nil, fmt.Errorf("inspect release bundle output directory: %w", err)
	}
	if err := parentDirectory.Verify(); err != nil {
		return nil, fmt.Errorf("verify release bundle parent directory: %w", err)
	}
	if err := verifyGenerationState(generationState); err != nil {
		return nil, fmt.Errorf("verify .doggo generation state: %w", err)
	}
	stageRoot, stageInfo, err := createBundleStage(parentRoot)
	if err != nil {
		return nil, fmt.Errorf("create release bundle stage: %w", err)
	}
	stageTree := project.NewOwnedTree(stageRoot)
	stageAtSource := true
	stageClosed := false
	cleanupStage := func() error {
		if stageClosed {
			return nil
		}
		stageClosed = true
		cleanupErr := error(nil)
		if stageAtSource {
			cleanupErr = stageTree.Cleanup(project.OwnedTreeCleanupHooks{})
		}
		closeErr := errors.Join(stageTree.Close(), stageRoot.Close())
		if stageAtSource && cleanupErr == nil && closeErr == nil {
			cleanupErr = project.RemoveCreatedEmptyRoot(parentRoot, stageInfo)
		}
		return errors.Join(cleanupErr, closeErr)
	}
	defer func() {
		err = errors.Join(err, cleanupStage())
	}()
	for _, file := range files {
		if err := stageTree.WriteFile(file.name, file.data, file.mode); err != nil {
			return nil, fmt.Errorf("stage %s: %w", file.name, err)
		}
	}
	if err := syncReleaseStage(stageRoot); err != nil {
		return nil, fmt.Errorf("synchronize release bundle stage: %w", err)
	}
	if err := verifyStagedFiles(stageTree, stageRoot, files); err != nil {
		return nil, fmt.Errorf("verify synchronized release bundle stage: %w", err)
	}
	if err := parentDirectory.Verify(); err != nil {
		return nil, fmt.Errorf("verify release bundle parent directory before publishing: %w", err)
	}
	if err := verifyGenerationState(generationState); err != nil {
		return nil, fmt.Errorf("verify .doggo generation state before publishing: %w", err)
	}
	if beforeReleasePublishForTest != nil {
		if err := beforeReleasePublishForTest("directory"); err != nil {
			return nil, err
		}
	}
	if err := syncReleaseStage(stageRoot); err != nil {
		return nil, fmt.Errorf("resynchronize release bundle stage before publishing: %w", err)
	}
	if err := verifyStagedFiles(stageTree, stageRoot, files); err != nil {
		return nil, fmt.Errorf("verify release bundle stage before publishing: %w", err)
	}
	if err := setAndVerifyRootDirectoryMode(stageRoot, 0755); err != nil {
		return nil, fmt.Errorf("prepare release bundle directory mode: %w", err)
	}
	if err := verifyStagedFiles(stageTree, stageRoot, files); err != nil {
		return nil, fmt.Errorf("verify prepared release bundle stage: %w", err)
	}
	if err := parentDirectory.Verify(); err != nil {
		return nil, fmt.Errorf("reverify release bundle parent directory before publishing: %w", err)
	}
	if err := verifyGenerationState(generationState); err != nil {
		return nil, fmt.Errorf("reverify .doggo generation state before publishing: %w", err)
	}
	publishErr := project.PublishRetainedDirectory(parentRoot, stageRoot, stageInfo.Name(), outputName)
	var atomicErr *project.AtomicWriteError
	if errors.As(publishErr, &atomicErr) && atomicErr.Renamed {
		stageAtSource = false
	}
	if publishErr != nil {
		return nil, fmt.Errorf("publish release bundle directory: %w", publishErr)
	}
	stageAtSource = false
	if afterReleaseDoggoWriteForTest != nil {
		afterReleaseDoggoWriteForTest(filepath.Join(rootPath, filepath.FromSlash(outputRel)))
	}
	publishedDirectory, err := project.OpenRetainedDirectory(parentRoot, outputName, 0, generationIdentity(generationState))
	if err != nil {
		return nil, fmt.Errorf("retain published release bundle directory: %w", err)
	}
	defer func() { err = errors.Join(err, publishedDirectory.Close()) }()
	if publishedDirectory.Identity() == nil || !os.SameFile(stageInfo, publishedDirectory.Identity()) {
		return nil, fmt.Errorf("%w: published release bundle directory changed", project.ErrOwnershipChanged)
	}
	if err := verifyRootDirectoryMode(publishedDirectory.Root(), 0755); err != nil {
		return nil, fmt.Errorf("verify published release bundle directory mode: %w", err)
	}
	if afterReleaseOutputRootOpenForTest != nil {
		afterReleaseOutputRootOpenForTest()
	}
	outputRoot := publishedDirectory.Root()
	var publicKey ed25519.PublicKey
	if privateKey != nil {
		publicKey = privateKey.Public().(ed25519.PublicKey)
	}
	if err := verifyBundleOutputRoot(outputRoot, publicKey, privateKey != nil); err != nil {
		return nil, fmt.Errorf("verify published release bundle: %w", err)
	}
	if err := publishedDirectory.Verify(); err != nil {
		return nil, fmt.Errorf("verify release bundle output directory after publishing: %w", err)
	}
	if err := parentDirectory.Verify(); err != nil {
		return nil, fmt.Errorf("verify release bundle parent directory after publishing: %w", err)
	}
	if err := verifyGenerationState(generationState); err != nil {
		return nil, fmt.Errorf("verify .doggo generation state after publishing: %w", err)
	}
	if err := project.VerifyRootPath(rootPath, root); err != nil {
		return nil, fmt.Errorf("verify project root after publishing release bundle: %w", err)
	}

	outputDir := filepath.Join(rootPath, filepath.FromSlash(outputRel))
	outputs := &ReleaseBundleOutputs{
		SHA256SumsPath: filepath.Join(outputDir, sha256SumsFileName),
		GoSBOMPath:     filepath.Join(outputDir, goSBOMFileName),
		VSCodeSBOMPath: filepath.Join(outputDir, vsixSBOMFileName),
		ManifestPath:   filepath.Join(outputDir, manifestFileName),
		Artifacts:      artifactDigests,
	}
	if signature != nil {
		outputs.SignaturePath = filepath.Join(outputDir, sha256SigFileName)
	}
	return outputs, nil
}

func CheckReleaseIntegrityFiles(projectRoot, outDir, publicKeyPath string, requireSignature bool) (err error) {
	root, rootPath, err := project.OpenVerifiedRoot(projectRoot)
	if err != nil {
		return fmt.Errorf("open project root: %w", err)
	}
	defer func() { err = errors.Join(err, root.Close()) }()
	outputRel, err := resolveProjectRelativePath(rootPath, outDir)
	if err != nil {
		return fmt.Errorf("resolve release bundle output directory: %w", err)
	}
	if err := rejectGenerationOutput(outputRel); err != nil {
		return err
	}
	generationState, err := openOptionalGenerationState(root)
	if err != nil {
		return fmt.Errorf("retain .doggo generation state: %w", err)
	}
	if generationState != nil {
		defer func() { err = errors.Join(err, generationState.Close()) }()
	}
	outputParentRel := filepath.ToSlash(filepath.Dir(filepath.FromSlash(outputRel)))
	parentDirectory, err := project.OpenRetainedDirectory(root, outputParentRel, 0, generationIdentity(generationState))
	if err != nil {
		return fmt.Errorf("open release bundle parent directory: %w", err)
	}
	defer func() { err = errors.Join(err, parentDirectory.Close()) }()
	outputName := filepath.Base(filepath.FromSlash(outputRel))
	outputDirectory, err := project.OpenRetainedDirectory(parentDirectory.Root(), outputName, 0, generationIdentity(generationState))
	if err != nil {
		return fmt.Errorf("open release bundle output directory: %w", err)
	}
	defer func() { err = errors.Join(err, outputDirectory.Close()) }()
	var publicKey ed25519.PublicKey
	if strings.TrimSpace(publicKeyPath) != "" {
		publicKey, err = loadEd25519PublicKey(publicKeyPath)
		if err != nil {
			return err
		}
	}
	if err := verifyBundleOutputRoot(outputDirectory.Root(), publicKey, requireSignature || publicKey != nil); err != nil {
		return err
	}
	if err := outputDirectory.Verify(); err != nil {
		return fmt.Errorf("verify release bundle output directory: %w", err)
	}
	if err := parentDirectory.Verify(); err != nil {
		return fmt.Errorf("verify release bundle parent directory: %w", err)
	}
	if err := verifyGenerationState(generationState); err != nil {
		return fmt.Errorf("verify .doggo generation state: %w", err)
	}
	if err := project.VerifyRootPath(rootPath, root); err != nil {
		return fmt.Errorf("verify project root: %w", err)
	}
	return nil
}

func collectArtifacts(root *os.Root, rootPath string, artifactPaths []string, binaryRel string, binary []byte, binaryDigest string) ([]preparedArtifact, error) {
	artifacts := []preparedArtifact{{digest: ArtifactDigest{Path: "doggo", SHA256: binaryDigest, Size: int64(len(binary))}, data: binary}}
	used := map[string]struct{}{canonicalBundleName("doggo"): {}}
	var inputInfos []fs.FileInfo
	var inputPaths []string
	for _, name := range []string{"doggo", sha256SumsFileName, sha256SigFileName, goSBOMFileName, vsixSBOMFileName, manifestFileName} {
		used[canonicalBundleName(name)] = struct{}{}
	}
	for _, path := range artifactPaths {
		// Validate the caller's basename before Windows path resolution can strip
		// trailing dots or reinterpret device names such as NUL.
		if !validBundleName(filepath.Base(path)) {
			return nil, fmt.Errorf("artifact %q does not have a portable bundle name", path)
		}
		rel, err := resolveProjectRelativePath(rootPath, path)
		if err != nil {
			return nil, fmt.Errorf("artifact path %q is outside project root: %w", path, err)
		}
		if sameProjectRelativePath(runtime.GOOS, rel, binaryRel) {
			return nil, fmt.Errorf("active generation binary cannot be listed in --artifacts; --artifacts accepts extras only: %s", rel)
		}
		name := filepath.Base(filepath.FromSlash(rel))
		if !validBundleName(name) {
			return nil, fmt.Errorf("artifact %q does not have a portable bundle name", path)
		}
		if _, exists := used[canonicalBundleName(name)]; exists {
			return nil, fmt.Errorf("reserved or duplicate bundle name %q from artifact %q", name, path)
		}
		content, info, err := readRegularFileRoot(root, rel)
		if err != nil {
			return nil, fmt.Errorf("read artifact %s: %w", rel, err)
		}
		for i, inputInfo := range inputInfos {
			if os.SameFile(inputInfo, info) {
				return nil, fmt.Errorf("artifacts %s and %s refer to the same file", inputPaths[i], rel)
			}
		}
		used[canonicalBundleName(name)] = struct{}{}
		artifacts = append(artifacts, preparedArtifact{digest: digestFor(name, content), data: content})
		inputInfos = append(inputInfos, info)
		inputPaths = append(inputPaths, rel)
	}
	sort.Slice(artifacts, func(i, j int) bool { return artifacts[i].digest.Path < artifacts[j].digest.Path })
	return artifacts, nil
}

func sameProjectRelativePath(goos, left, right string) bool {
	return left == right || goos == "windows" && strings.EqualFold(left, right)
}

func digestFor(name string, content []byte) ArtifactDigest {
	sum := sha256.Sum256(content)
	return ArtifactDigest{Path: name, SHA256: hex.EncodeToString(sum[:]), Size: int64(len(content))}
}

func marshalSHA256Sums(artifacts []ArtifactDigest) []byte {
	var content strings.Builder
	for _, artifact := range artifacts {
		fmt.Fprintf(&content, "%s  %s\n", artifact.SHA256, artifact.Path)
	}
	return []byte(content.String())
}

func verifyBundleOutputRoot(root *os.Root, publicKey ed25519.PublicKey, requireSignature bool) error {
	if requireSignature && len(publicKey) != ed25519.PublicKeySize {
		return fmt.Errorf("signature verification requires an ed25519 public key")
	}
	manifestContent, _, err := readRegularFileRoot(root, manifestFileName)
	if err != nil {
		return fmt.Errorf("read release bundle manifest: %w", err)
	}
	var manifest releaseBundleManifest
	if err := json.Unmarshal(manifestContent, &manifest); err != nil {
		return fmt.Errorf("parse release bundle manifest: %w", err)
	}
	if err := validateManifestContract(manifest); err != nil {
		return err
	}
	if manifest.Signature != "" && len(publicKey) != ed25519.PublicKeySize {
		return fmt.Errorf("signed release bundle requires an ed25519 public key")
	}
	shaContent, _, err := readRegularFileRoot(root, sha256SumsFileName)
	if err != nil {
		return fmt.Errorf("read checksum file: %w", err)
	}
	sums, err := parseSHA256Sums(shaContent)
	if err != nil {
		return err
	}
	expected := make(map[string]ArtifactDigest, len(manifest.Artifacts)+3)
	for _, artifact := range manifest.Artifacts {
		expected[artifact.Path] = artifact
	}
	for _, name := range []string{goSBOMFileName, vsixSBOMFileName, manifestFileName} {
		expected[name] = ArtifactDigest{Path: name}
	}
	expectedEntries := make(map[string]struct{}, len(expected)+2)
	for name := range expected {
		expectedEntries[name] = struct{}{}
	}
	expectedEntries[sha256SumsFileName] = struct{}{}
	if manifest.Signature != "" {
		expectedEntries[sha256SigFileName] = struct{}{}
	}
	if err := verifyBundleEntrySet(root, expectedEntries); err != nil {
		return err
	}
	if len(sums) != len(expected) {
		return fmt.Errorf("checksum entries do not match release bundle manifest")
	}
	for name, expectedDigest := range expected {
		hashValue, ok := sums[name]
		if !ok {
			return fmt.Errorf("checksum entry is missing for %s", name)
		}
		data, info, err := readRegularFileRoot(root, name)
		if err != nil {
			return fmt.Errorf("read bundle file for checksum verification (%s): %w", name, err)
		}
		if name == "doggo" && !releaseBinaryModeValid(runtime.GOOS, info.Mode()) {
			return fmt.Errorf("release binary mode is %o, want 755", info.Mode().Perm())
		}
		actual := digestFor(name, data)
		if actual.SHA256 != hashValue {
			return fmt.Errorf("checksum mismatch for %s: expected %s, got %s", name, hashValue, actual.SHA256)
		}
		if expectedDigest.SHA256 != "" && (expectedDigest.SHA256 != actual.SHA256 || expectedDigest.Size != actual.Size) {
			return fmt.Errorf("manifest artifact digest mismatch for %s", name)
		}
	}
	if requireSignature && manifest.Signature != sha256SigFileName {
		return fmt.Errorf("release bundle manifest does not declare a signature")
	}
	if manifest.Signature != "" {
		signature, _, err := readRegularFileRoot(root, sha256SigFileName)
		if err != nil {
			return fmt.Errorf("read release signature: %w", err)
		}
		if !ed25519.Verify(publicKey, shaContent, signature) {
			return fmt.Errorf("signature verification failed")
		}
	} else if publicKey != nil {
		return fmt.Errorf("release bundle manifest does not declare a signature")
	}
	return nil
}

func validateManifestContract(manifest releaseBundleManifest) error {
	if manifest.Schema != "doggo-release-bundle-v1" || strings.TrimSpace(manifest.GeneratedAt) == "" || manifest.SHA256Sums != sha256SumsFileName || manifest.GoSBOM != goSBOMFileName || manifest.VSCodeSBOM != vsixSBOMFileName {
		return fmt.Errorf("invalid release bundle manifest contract")
	}
	if _, err := time.Parse(time.RFC3339, manifest.GeneratedAt); err != nil {
		return fmt.Errorf("invalid release bundle manifest generation time: %w", err)
	}
	if manifest.Signature != "" && manifest.Signature != sha256SigFileName {
		return fmt.Errorf("invalid release bundle manifest signature path")
	}
	seen := make(map[string]struct{}, len(manifest.Artifacts))
	hasDoggo := false
	for _, artifact := range manifest.Artifacts {
		if !validBundleName(artifact.Path) || isReservedBundleName(artifact.Path) {
			return fmt.Errorf("invalid manifest artifact path %q", artifact.Path)
		}
		canonical := canonicalBundleName(artifact.Path)
		if _, exists := seen[canonical]; exists {
			return fmt.Errorf("duplicate manifest artifact path %q", artifact.Path)
		}
		if len(artifact.SHA256) != sha256.Size*2 {
			return fmt.Errorf("invalid manifest artifact digest for %s", artifact.Path)
		}
		if _, err := hex.DecodeString(artifact.SHA256); err != nil || artifact.Size < 0 {
			return fmt.Errorf("invalid manifest artifact digest for %s", artifact.Path)
		}
		if artifact.Path == "doggo" {
			hasDoggo = true
		}
		seen[canonical] = struct{}{}
	}
	if !hasDoggo {
		return fmt.Errorf("release bundle manifest is missing doggo")
	}
	return nil
}

func parseSHA256Sums(content []byte) (map[string]string, error) {
	sums := make(map[string]string)
	scanner := bufio.NewScanner(bytes.NewReader(content))
	for lineNo := 1; scanner.Scan(); lineNo++ {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		hashValue, name, ok := parseSHA256Line(line)
		if !ok || !validBundleName(name) || len(hashValue) != sha256.Size*2 {
			return nil, fmt.Errorf("invalid checksum line %d: %s", lineNo, line)
		}
		if _, err := hex.DecodeString(hashValue); err != nil {
			return nil, fmt.Errorf("invalid checksum line %d: %s", lineNo, line)
		}
		for existing := range sums {
			if canonicalBundleName(existing) == canonicalBundleName(name) {
				return nil, fmt.Errorf("duplicate checksum entry %s", name)
			}
		}
		sums[name] = hashValue
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("scan checksum file: %w", err)
	}
	return sums, nil
}

func generateGoModuleSBOM(binary []byte, generatedAt string) ([]byte, error) {
	info, err := buildinfo.Read(bytes.NewReader(binary))
	if err != nil {
		return nil, fmt.Errorf("read Go build info from generation binary: %w", err)
	}
	components := make([]goModuleComponent, 0, len(info.Deps)+1)
	main := info.Main
	if main.Path == "" {
		main.Path = info.Path
	}
	components = append(components, goModuleComponent{Name: main.Path, Version: mainModuleVersion(main.Version), Replace: moduleReplacement(main.Replace), Main: true})
	for _, dependency := range info.Deps {
		if dependency == nil || dependency.Path == "" {
			continue
		}
		components = append(components, goModuleComponent{Name: dependency.Path, Version: dependency.Version, Replace: moduleReplacement(dependency.Replace)})
	}
	sort.Slice(components, func(i, j int) bool {
		if components[i].Name == components[j].Name {
			return components[i].Version < components[j].Version
		}
		return components[i].Name < components[j].Name
	})
	return marshalJSON(goModuleSBOM{Schema: "doggo-sbom-go-modules-v1", GeneratedAt: generatedAt, Tool: "debug/buildinfo", Components: components}, "Go module SBOM")
}

func mainModuleVersion(version string) string {
	if version == "" || version == "(devel)" {
		return "(main)"
	}
	return version
}

func moduleReplacement(module *debug.Module) string {
	if module == nil {
		return ""
	}
	replacement := module.Path
	if module.Version != "" {
		replacement += "@" + module.Version
	}
	return replacement
}

func generateNPMPackageSBOM(content []byte, generatedAt string) ([]byte, error) {
	var lock npmLockFile
	if err := json.Unmarshal(content, &lock); err != nil {
		return nil, fmt.Errorf("parse package-lock.json: %w", err)
	}
	components := make([]npmPackageComponent, 0)
	if len(lock.Packages) > 0 {
		for path, pkg := range lock.Packages {
			const marker = "node_modules/"
			index := strings.LastIndex(path, marker)
			if index < 0 {
				continue
			}
			name := path[index+len(marker):]
			if name != "" {
				components = append(components, npmPackageComponent{Name: name, Version: pkg.Version, Resolved: pkg.Resolved, Integrity: pkg.Integrity, Dev: pkg.Dev})
			}
		}
	} else {
		for name, dependency := range lock.Dependencies {
			if strings.TrimSpace(name) != "" {
				components = append(components, npmPackageComponent{Name: name, Version: dependency.Version})
			}
		}
	}
	sort.Slice(components, func(i, j int) bool {
		if components[i].Name == components[j].Name {
			return components[i].Version < components[j].Version
		}
		return components[i].Name < components[j].Name
	})
	return marshalJSON(npmPackageSBOM{Schema: "doggo-sbom-npm-packages-v1", GeneratedAt: generatedAt, Tool: "npm package-lock parser", Components: components}, "npm package SBOM")
}

func parseSHA256Line(line string) (string, string, bool) {
	parts := strings.SplitN(line, "  ", 2)
	if len(parts) != 2 {
		return "", "", false
	}
	hashValue, name := strings.TrimSpace(parts[0]), strings.TrimSpace(parts[1])
	if hashValue == "" || name == "" {
		return "", "", false
	}
	if filepath.ToSlash(filepath.Clean(filepath.FromSlash(name))) != name {
		return "", "", false
	}
	return hashValue, name, true
}

func validBundleName(name string) bool {
	if !utf8.ValidString(name) || name == "" || name == "." || name == ".." || strings.HasSuffix(name, ".") {
		return false
	}
	for i := 0; i < len(name); i++ {
		b := name[i]
		if !((b >= 'a' && b <= 'z') || (b >= 'A' && b <= 'Z') || (b >= '0' && b <= '9') || b == '.' || b == '-' || b == '_') {
			return false
		}
	}
	stem, _, _ := strings.Cut(strings.ToUpper(name), ".")
	switch stem {
	case "CON", "PRN", "AUX", "NUL", "CLOCK$", "CONIN$", "CONOUT$":
		return false
	}
	return len(stem) != 4 || (stem[:3] != "COM" && stem[:3] != "LPT") || stem[3] < '1' || stem[3] > '9'
}

func isReservedBundleName(name string) bool {
	for _, reserved := range []string{sha256SumsFileName, sha256SigFileName, goSBOMFileName, vsixSBOMFileName, manifestFileName} {
		if strings.EqualFold(name, reserved) {
			return true
		}
	}
	return false
}

func canonicalBundleName(name string) string { return strings.ToUpper(name) }

func normalizeArtifactPath(path string) string {
	p := filepath.ToSlash(filepath.Clean(strings.TrimSpace(path)))
	if p == "." || p == "" {
		return ""
	}
	return p
}

func loadPEMKey(path, keyType, format string, parse func([]byte) (any, error)) (any, error) {
	content, err := readRegularFilePath(path)
	if err != nil {
		return nil, fmt.Errorf("read %s key: %w", keyType, err)
	}
	return parsePEMKey(content, path, keyType, format, parse)
}

func parsePEMKey(content []byte, source, keyType, format string, parse func([]byte) (any, error)) (any, error) {
	block, _ := pem.Decode(content)
	if block == nil {
		return nil, fmt.Errorf("invalid %s key PEM: %s", keyType, source)
	}
	parsed, err := parse(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse %s key %s: %w", keyType, format, err)
	}
	return parsed, nil
}

func loadEd25519PrivateKey(path string) (ed25519.PrivateKey, error) {
	parsed, err := loadPEMKey(path, "private", "PKCS8", x509.ParsePKCS8PrivateKey)
	if err != nil {
		return nil, err
	}
	key, ok := parsed.(ed25519.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("private key is not ed25519: %s", path)
	}
	return key, nil
}

func parseEd25519PrivateKey(content []byte, source string) (ed25519.PrivateKey, error) {
	parsed, err := parsePEMKey(content, source, "private", "PKCS8", x509.ParsePKCS8PrivateKey)
	if err != nil {
		return nil, err
	}
	key, ok := parsed.(ed25519.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("private key is not ed25519: %s", source)
	}
	return key, nil
}

func loadEd25519PublicKey(path string) (ed25519.PublicKey, error) {
	parsed, err := loadPEMKey(path, "public", "PKIX", x509.ParsePKIXPublicKey)
	if err != nil {
		return nil, err
	}
	key, ok := parsed.(ed25519.PublicKey)
	if !ok {
		return nil, fmt.Errorf("public key is not ed25519: %s", path)
	}
	return key, nil
}

func parseEd25519PublicKey(content []byte, source string) (ed25519.PublicKey, error) {
	parsed, err := parsePEMKey(content, source, "public", "PKIX", x509.ParsePKIXPublicKey)
	if err != nil {
		return nil, err
	}
	key, ok := parsed.(ed25519.PublicKey)
	if !ok {
		return nil, fmt.Errorf("public key is not ed25519: %s", source)
	}
	return key, nil
}

func GenerateKeyPair(keyDir string) (err error) {
	publicKey, privateKey, err := ed25519.GenerateKey(nil)
	if err != nil {
		return fmt.Errorf("generate ed25519 key pair: %w", err)
	}
	privateDER, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		return fmt.Errorf("marshal private key PKCS8: %w", err)
	}
	publicDER, err := x509.MarshalPKIXPublicKey(publicKey)
	if err != nil {
		return fmt.Errorf("marshal public key PKIX: %w", err)
	}
	privateContent := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateDER})
	publicContent := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: publicDER})

	keyParent, err := openRetainedAbsoluteParent(keyDir, 0755)
	if err != nil {
		return fmt.Errorf("retain key directory parent: %w", err)
	}
	defer func() { err = errors.Join(err, keyParent.Close()) }()
	if _, err := keyParent.Root().Lstat(keyParent.name); err == nil {
		return fmt.Errorf("key directory already exists: %s", keyDir)
	} else if !errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("inspect key directory: %w", err)
	}
	if err := keyParent.Verify(); err != nil {
		return fmt.Errorf("verify key directory parent: %w", err)
	}
	stageRoot, stageInfo, err := createReleaseStage(keyParent.Root(), ".release-key-stage-", "key-stage")
	if err != nil {
		return fmt.Errorf("create key directory stage: %w", err)
	}
	stageTree := project.NewOwnedTree(stageRoot)
	stageAtSource := true
	stageClosed := false
	defer func() {
		if stageClosed {
			return
		}
		stageClosed = true
		cleanupErr := error(nil)
		if stageAtSource {
			// Remove the exact private identity first so a failed validation can
			// never leave a secret-only stage. Replacements remain untouched.
			cleanupErr = errors.Join(
				stageTree.RemoveFile(PrivateKeyFileName),
				stageTree.RemoveFile(PublicKeyFileName),
				stageTree.Cleanup(project.OwnedTreeCleanupHooks{}),
			)
		}
		closeErr := errors.Join(stageTree.Close(), stageRoot.Close())
		if stageAtSource && closeErr == nil {
			cleanupErr = errors.Join(cleanupErr, project.RemoveCreatedEmptyRoot(keyParent.Root(), stageInfo))
		}
		err = errors.Join(err, cleanupErr, closeErr)
	}()
	if err := stageTree.WriteFile(PrivateKeyFileName, privateContent, 0600); err != nil {
		return fmt.Errorf("stage private key: %w", err)
	}
	if err := stageTree.WriteFile(PublicKeyFileName, publicContent, 0644); err != nil {
		return fmt.Errorf("stage public key: %w", err)
	}
	if err := stageTree.VerifyComplete(); err != nil {
		return fmt.Errorf("verify staged key pair: %w", err)
	}
	if err := syncReleaseStage(stageRoot); err != nil {
		return fmt.Errorf("synchronize staged key pair: %w", err)
	}
	if err := verifyStagedKeyPair(stageTree, stageRoot, privateContent, publicContent); err != nil {
		return fmt.Errorf("verify synchronized key pair: %w", err)
	}
	if err := keyParent.Verify(); err != nil {
		return fmt.Errorf("verify key directory parent before publishing: %w", err)
	}
	if beforeReleaseKeyPublishForTest != nil {
		if err := beforeReleaseKeyPublishForTest("directory"); err != nil {
			return err
		}
	}
	if err := syncReleaseStage(stageRoot); err != nil {
		return fmt.Errorf("resynchronize staged key pair before publishing: %w", err)
	}
	if err := verifyStagedKeyPair(stageTree, stageRoot, privateContent, publicContent); err != nil {
		return fmt.Errorf("verify key pair before publishing: %w", err)
	}
	publishErr := project.PublishRetainedDirectory(keyParent.Root(), stageRoot, stageInfo.Name(), keyParent.name)
	var atomicErr *project.AtomicWriteError
	if errors.As(publishErr, &atomicErr) && atomicErr.Renamed {
		stageAtSource = false
	}
	if publishErr != nil {
		return fmt.Errorf("publish key directory: %w", publishErr)
	}
	stageAtSource = false
	published, err := project.OpenRetainedDirectory(keyParent.Root(), keyParent.name, 0)
	if err != nil {
		return fmt.Errorf("retain published key directory: %w", err)
	}
	defer func() { err = errors.Join(err, published.Close()) }()
	if published.Identity() == nil || !os.SameFile(stageInfo, published.Identity()) {
		return fmt.Errorf("%w: published key directory changed", project.ErrOwnershipChanged)
	}
	if err := verifyStagedKeyPair(stageTree, stageRoot, privateContent, publicContent); err != nil {
		return fmt.Errorf("verify published key pair: %w", err)
	}
	if err := errors.Join(published.Verify(), keyParent.Verify()); err != nil {
		return fmt.Errorf("verify published key directory: %w", err)
	}
	return nil
}

func verifyStagedKeyPair(tree *project.OwnedTree, root *os.Root, privateContent, publicContent []byte) error {
	return verifyStagedFiles(tree, root, []bundleFile{
		{name: PrivateKeyFileName, data: privateContent, mode: 0600},
		{name: PublicKeyFileName, data: publicContent, mode: 0644},
	})
}

func verifyStagedFiles(tree *project.OwnedTree, root *os.Root, files []bundleFile) error {
	if err := tree.VerifyComplete(); err != nil {
		return err
	}
	for _, file := range files {
		content, err := tree.ReadFile(file.name)
		if err != nil {
			return fmt.Errorf("read staged %s: %w", file.name, err)
		}
		if !bytes.Equal(content, file.data) {
			return fmt.Errorf("staged %s content changed", file.name)
		}
		info, err := root.Lstat(file.name)
		if err != nil {
			return fmt.Errorf("inspect staged %s: %w", file.name, err)
		}
		if !info.Mode().IsRegular() {
			return fmt.Errorf("staged %s is not a regular file", file.name)
		}
		if runtime.GOOS != "windows" && info.Mode().Perm() != file.mode.Perm() {
			return fmt.Errorf("staged %s mode is %o, want %o", file.name, info.Mode().Perm(), file.mode.Perm())
		}
	}
	return tree.VerifyComplete()
}

func setAndVerifyRootDirectoryMode(root *os.Root, mode fs.FileMode) error {
	directory, err := root.Open(".")
	if err != nil {
		return err
	}
	info, statErr := directory.Stat()
	if statErr != nil || !info.IsDir() {
		if statErr == nil {
			statErr = fmt.Errorf("release stage root is not a directory")
		}
		return errors.Join(statErr, directory.Close())
	}
	chmodErr := directory.Chmod(mode)
	closeErr := directory.Close()
	syncErr := error(nil)
	if chmodErr == nil {
		syncErr = syncReleaseStage(root)
	}
	return errors.Join(chmodErr, closeErr, syncErr, verifyRootDirectoryMode(root, mode))
}

func verifyRootDirectoryMode(root *os.Root, mode fs.FileMode) error {
	info, err := root.Stat(".")
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("release stage root is not a directory")
	}
	if runtime.GOOS != "windows" && info.Mode().Perm() != mode.Perm() {
		return fmt.Errorf("release stage directory mode is %o, want %o", info.Mode().Perm(), mode.Perm())
	}
	return nil
}

func ParseArtifactList(list string) []string {
	parts := strings.Split(list, ",")
	artifacts := make([]string, 0, len(parts))
	for _, part := range parts {
		if normalized := normalizeArtifactPath(part); normalized != "" {
			artifacts = append(artifacts, normalized)
		}
	}
	return artifacts
}

func resolveProjectRelativePath(rootPath, inputPath string) (string, error) {
	pathValue := strings.TrimSpace(inputPath)
	if pathValue == "" {
		return "", fmt.Errorf("path is empty")
	}
	candidate := pathValue
	if !filepath.IsAbs(candidate) {
		candidate = filepath.Join(rootPath, filepath.FromSlash(candidate))
	}
	abs, err := filepath.Abs(candidate)
	if err != nil {
		return "", fmt.Errorf("resolve absolute path: %w", err)
	}
	rel, err := filepath.Rel(rootPath, abs)
	if err != nil {
		return "", fmt.Errorf("resolve relative path: %w", err)
	}
	if filepath.IsAbs(rel) || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("path %s is outside project root %s", filepath.ToSlash(abs), filepath.ToSlash(rootPath))
	}
	if rel == "." {
		return "", fmt.Errorf("path is the project root")
	}
	return filepath.ToSlash(filepath.Clean(rel)), nil
}

func rejectGenerationOutput(outputRel string) error {
	if outputRel == ".doggo" || strings.HasPrefix(outputRel, ".doggo/") {
		return fmt.Errorf("release bundle output cannot overlap .doggo generation state")
	}
	return nil
}

func openOptionalGenerationState(root *os.Root) (*project.RetainedDirectory, error) {
	directory, err := project.OpenRetainedDirectory(root, ".doggo", 0)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	return directory, err
}

func generationIdentity(directory *project.RetainedDirectory) fs.FileInfo {
	if directory == nil {
		return nil
	}
	return directory.Identity()
}

func verifyGenerationState(directory *project.RetainedDirectory) error {
	if directory == nil {
		return nil
	}
	return directory.Verify()
}

func readRegularFileRoot(root *os.Root, path string) (_ []byte, _ fs.FileInfo, err error) {
	clean := filepath.Clean(filepath.FromSlash(path))
	if filepath.IsAbs(clean) || clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return nil, nil, fmt.Errorf("path is outside retained root")
	}
	parentDirectory, err := project.OpenRetainedDirectory(root, filepath.ToSlash(filepath.Dir(clean)), 0)
	if err != nil {
		return nil, nil, err
	}
	defer func() { err = errors.Join(err, parentDirectory.Close()) }()
	if afterReleaseInputDirectoryOpenForTest != nil {
		afterReleaseInputDirectoryOpenForTest(filepath.ToSlash(clean))
	}
	parent := parentDirectory.Root()
	name := filepath.Base(clean)
	before, err := parent.Lstat(name)
	if err != nil {
		return nil, nil, err
	}
	if before.Mode()&os.ModeSymlink != 0 || !before.Mode().IsRegular() {
		return nil, nil, fmt.Errorf("%s is not an ordinary file", filepath.ToSlash(clean))
	}
	file, err := parent.Open(name)
	if err != nil {
		return nil, nil, err
	}
	defer func() { err = errors.Join(err, file.Close()) }()
	opened, err := file.Stat()
	if err != nil {
		return nil, nil, err
	}
	if !opened.Mode().IsRegular() || !os.SameFile(before, opened) {
		return nil, nil, fmt.Errorf("%s changed while opening", filepath.ToSlash(clean))
	}
	content, err := io.ReadAll(file)
	if err != nil {
		return nil, nil, err
	}
	after, statErr := parent.Lstat(name)
	if statErr != nil || after.Mode()&os.ModeSymlink != 0 || !after.Mode().IsRegular() || !os.SameFile(opened, after) {
		return nil, nil, errors.Join(fmt.Errorf("%s changed while reading", filepath.ToSlash(clean)), statErr)
	}
	if err := parentDirectory.Verify(); err != nil {
		return nil, nil, fmt.Errorf("directory identity changed while reading %s: %w", filepath.ToSlash(clean), err)
	}
	return content, opened, nil
}

func readRootDirectory(root *os.Root) (_ []fs.DirEntry, err error) {
	before, err := root.Stat(".")
	if err != nil {
		return nil, err
	}
	directory, err := root.Open(".")
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, directory.Close()) }()
	opened, err := directory.Stat()
	if err != nil {
		return nil, err
	}
	if !opened.IsDir() || !os.SameFile(before, opened) {
		return nil, fmt.Errorf("retained release bundle directory changed while opening")
	}
	entries, err := directory.ReadDir(-1)
	if err != nil {
		return nil, err
	}
	after, err := root.Stat(".")
	if err != nil || !os.SameFile(opened, after) {
		return nil, errors.Join(fmt.Errorf("retained release bundle directory changed while reading"), err)
	}
	return entries, nil
}

func verifyBundleEntrySet(root *os.Root, expected map[string]struct{}) error {
	entries, err := readRootDirectory(root)
	if err != nil {
		return fmt.Errorf("list release bundle: %w", err)
	}
	for _, entry := range entries {
		if _, ok := expected[entry.Name()]; !ok {
			return fmt.Errorf("untracked bundle entry %q", entry.Name())
		}
	}
	if len(entries) != len(expected) {
		return fmt.Errorf("release bundle entries do not match the manifest")
	}
	return nil
}

func readRegularFilePath(path string) (_ []byte, err error) {
	parent, err := openRetainedAbsoluteParent(path, 0)
	if err != nil {
		return nil, err
	}
	defer func() { err = errors.Join(err, parent.Close()) }()
	content, _, err := readRegularFileRoot(parent.Root(), parent.name)
	if err != nil {
		return nil, err
	}
	if err := parent.Verify(); err != nil {
		return nil, err
	}
	return content, nil
}

func openRetainedAbsoluteParent(path string, createMode fs.FileMode) (*retainedAbsoluteParent, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return nil, err
	}
	abs = filepath.Clean(abs)
	name := filepath.Base(abs)
	if name == "." || name == string(filepath.Separator) {
		return nil, fmt.Errorf("file target is invalid: %s", path)
	}
	rootPath := filepath.VolumeName(abs) + string(filepath.Separator)
	if rootPath == "" {
		rootPath = string(filepath.Separator)
	}
	relativeParent, err := filepath.Rel(rootPath, filepath.Dir(abs))
	if err != nil || filepath.IsAbs(relativeParent) || relativeParent == ".." || strings.HasPrefix(relativeParent, ".."+string(filepath.Separator)) {
		return nil, errors.Join(fmt.Errorf("file target is outside filesystem root: %s", path), err)
	}
	root, verifiedRootPath, err := project.OpenVerifiedRoot(rootPath)
	if err != nil {
		return nil, err
	}
	directory, err := project.OpenRetainedDirectory(root, filepath.ToSlash(relativeParent), createMode)
	if err != nil {
		return nil, errors.Join(err, root.Close())
	}
	return &retainedAbsoluteParent{rootPath: verifiedRootPath, root: root, directory: directory, name: name}, nil
}

func (p *retainedAbsoluteParent) Root() *os.Root { return p.directory.Root() }

func (p *retainedAbsoluteParent) Verify() error {
	return errors.Join(p.directory.Verify(), project.VerifyRootPath(p.rootPath, p.root))
}

func (p *retainedAbsoluteParent) Close() error {
	if p == nil {
		return nil
	}
	return errors.Join(p.directory.Close(), p.root.Close())
}

func createBundleStage(parent *os.Root) (*os.Root, fs.FileInfo, error) {
	return createReleaseStage(parent, ".release-stage-", "stage")
}

func createReleaseStage(parent *os.Root, prefix, kind string) (*os.Root, fs.FileInfo, error) {
	for {
		name := prefix + cryptorand.Text()
		err := parent.Mkdir(name, 0700)
		if errors.Is(err, fs.ErrExist) {
			continue
		}
		if err != nil {
			return nil, nil, err
		}
		created, err := parent.Lstat(name)
		if err != nil || created.Mode()&os.ModeSymlink != 0 || !created.IsDir() {
			cleanupErr := error(nil)
			if created != nil {
				cleanupErr = project.RemoveCreatedEmptyRoot(parent, created)
			}
			return nil, nil, errors.Join(fmt.Errorf("created release stage is not an ordinary directory"), err, cleanupErr)
		}
		if afterReleaseDirectoryCreateForTest != nil {
			afterReleaseDirectoryCreateForTest(kind, name)
		}
		root, err := project.OpenVerifiedSubroot(parent, name)
		if err == nil {
			opened, statErr := root.Stat(".")
			if statErr != nil || !os.SameFile(created, opened) {
				err = errors.Join(fmt.Errorf("created release stage changed before opening"), statErr)
			}
		}
		if err != nil {
			return nil, nil, errors.Join(err, closeBundleRoot(root), project.RemoveCreatedEmptyRoot(parent, created))
		}
		return root, created, nil
	}
}

func closeBundleRoot(root *os.Root) error {
	if root == nil {
		return nil
	}
	return root.Close()
}

func releaseBinaryModeValid(goos string, mode fs.FileMode) bool {
	return mode.IsRegular() && (goos == "windows" || mode.Perm() == 0755)
}

func marshalJSON(value any, label string) ([]byte, error) {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshal %s: %w", label, err)
	}
	return append(data, '\n'), nil
}
