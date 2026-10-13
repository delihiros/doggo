package deps

import (
	"bytes"
	"errors"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"doggo/internal/depsource"
	"doggo/internal/project"
	"doggo/internal/testutil"
	"doggo/internal/toolchain/generation"
)

func TestServiceUpdateDependenciesCommitsPreparedSnapshot(t *testing.T) {
	root, prior := updateProjectFixture(t)
	if err := os.WriteFile(filepath.Join(root, "dep", "value.dog"), []byte("(def value 2)\n"), 0644); err != nil {
		t.Fatal(err)
	}
	service := Service{
		ResolveProjectRoot: func() (string, bool, error) { return root, true, nil },
		Auditor: Auditor{AuditLock: func(contentRoot, sourceRoot string, _ *project.ModFile, lock *project.LockFile) ([]DependencyAuditFinding, error) {
			if sourceRoot != root || contentRoot == root || lock.Generation != "" {
				t.Fatalf("audit did not receive tentative generation: %q %q %#v", contentRoot, sourceRoot, lock)
			}
			return nil, nil
		}},
	}
	lock, results, err := service.UpdateDependencies("./dep", false)
	if err != nil {
		t.Fatal(err)
	}
	if lock.Generation == prior.Generation || len(results) != 1 || !results[0].Updated {
		t.Fatalf("update = lock %#v results %#v", lock, results)
	}
	if results[0].OldRevision != "" || results[0].NewRevision != "" || lock.Deps[0].Source != "./dep" {
		t.Fatalf("v2 update identity = %#v dep %#v", results[0], lock.Deps[0])
	}
}

func TestServiceUpdateDependenciesUsesCanonicalTargetDeclaration(t *testing.T) {
	root, _ := updateProjectFixture(t)
	service := Service{
		ResolveProjectRoot: func() (string, bool, error) { return root, true, nil },
		Auditor: Auditor{AuditLock: func(string, string, *project.ModFile, *project.LockFile) ([]DependencyAuditFinding, error) {
			return nil, nil
		}},
	}
	_, results, err := service.UpdateDependencies("./nested/../dep", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(results) != 1 || results[0].Source != "./dep" {
		t.Fatalf("results = %#v", results)
	}
}

func TestFillPathOutdatedStatusOwnsTemporaryRootAndReportsCleanupError(t *testing.T) {
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "dep"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := project.WriteModFile(project.ModPath(filepath.Join(root, "dep")), &project.ModFile{Module: "example/dep", Deps: []string{}}); err != nil {
		t.Fatal(err)
	}

	originalNew := newOutdatedTempRoot
	originalClose := closeOutdatedTempRoot
	want := errors.New("cleanup failed")
	var owned *project.OwnedTempRoot
	newOutdatedTempRoot = func(prefix string) (*project.OwnedTempRoot, error) {
		var err error
		owned, err = originalNew(prefix)
		return owned, err
	}
	closeOutdatedTempRoot = func(root *project.OwnedTempRoot) error {
		return errors.Join(originalClose(root), want)
	}
	t.Cleanup(func() {
		newOutdatedTempRoot = originalNew
		closeOutdatedTempRoot = originalClose
	})

	status := DependencyOutdatedStatus{}
	parsed, err := depsource.Parse("./dep")
	if err != nil {
		t.Fatal(err)
	}
	fillSnapshotOutdatedStatus(root, parsed, project.LockDep{Source: "./dep"}, &status)
	if !strings.Contains(status.CheckError, want.Error()) {
		t.Fatalf("CheckError = %q, want cleanup failure", status.CheckError)
	}
	if owned == nil {
		t.Fatal("outdated snapshot did not allocate an OwnedTempRoot")
	}
	if _, err := os.Lstat(owned.Path()); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("outdated temporary root remains: %v", err)
	}
}

func TestFillSnapshotOutdatedStatusComparesGoRevisionAndDigest(t *testing.T) {
	original := snapshotOutdatedDependenciesTree
	t.Cleanup(func() { snapshotOutdatedDependenciesTree = original })

	for _, tt := range []struct {
		name          string
		source        string
		lockedVersion string
		lockedDigest  string
		latestVersion string
		latestDigest  string
		wantOutdated  bool
	}{
		{name: "exact match", source: "example.com/math@v1.2.3", lockedVersion: "v1.2.3", lockedDigest: "same", latestVersion: "v1.2.3", latestDigest: "same"},
		{name: "omitted revision changed", source: "example.com/math", lockedVersion: "v1.2.3", lockedDigest: "same", latestVersion: "v1.2.4", latestDigest: "same", wantOutdated: true},
		{name: "latest digest changed", source: "example.com/math@latest", lockedVersion: "v1.2.3", lockedDigest: "old", latestVersion: "v1.2.3", latestDigest: "new", wantOutdated: true},
	} {
		t.Run(tt.name, func(t *testing.T) {
			parsed, err := depsource.Parse(tt.source)
			if err != nil {
				t.Fatal(err)
			}
			snapshotOutdatedDependenciesTree = func(_ *os.Root, _ string, _ *project.OwnedTree, sources []string) ([]project.LockDep, error) {
				if !reflect.DeepEqual(sources, []string{tt.source}) {
					t.Fatalf("snapshot sources = %#v", sources)
				}
				return []project.LockDep{{Source: tt.source, Kind: "go", Revision: tt.latestVersion, Digest: tt.latestDigest}}, nil
			}
			status := DependencyOutdatedStatus{LockedDigest: tt.lockedDigest, LockedRevision: tt.lockedVersion}
			fillSnapshotOutdatedStatus(t.TempDir(), parsed, project.LockDep{Source: tt.source, Kind: "go", Revision: tt.lockedVersion, Digest: tt.lockedDigest}, &status)
			if status.Outdated != tt.wantOutdated || status.LatestRevision != tt.latestVersion || status.LatestDigest != tt.latestDigest {
				t.Fatalf("status = %#v", status)
			}
		})
	}
}

func TestOutdatedDependencyStatusReportsInvalidSourceAndSnapshotErrors(t *testing.T) {
	invalid := outdatedDependencyStatus(t.TempDir(), "github.com/acme/math@main", nil)
	if !invalid.Outdated || invalid.CheckError == "" {
		t.Fatalf("invalid source status = %#v", invalid)
	}

	original := snapshotOutdatedDependenciesTree
	want := errors.New("snapshot failed")
	snapshotOutdatedDependenciesTree = func(*os.Root, string, *project.OwnedTree, []string) ([]project.LockDep, error) {
		return nil, want
	}
	t.Cleanup(func() { snapshotOutdatedDependenciesTree = original })
	status := outdatedDependencyStatus(t.TempDir(), "example.com/math", []project.LockDep{{
		Source: "example.com/math", Kind: "go", Revision: "v1.2.3", Digest: "old",
	}})
	if !status.Outdated || !strings.Contains(status.CheckError, want.Error()) {
		t.Fatalf("snapshot error status = %#v", status)
	}
}

func TestConcurrentTargetedUpdatesDoNotOverwriteAnotherCommit(t *testing.T) {
	base := t.TempDir()
	oneSource, oneOld := createGitUpdateDependency(t, base, "one")
	twoSource, twoOld := createGitUpdateDependency(t, base, "two")
	root := filepath.Join(base, "project")
	if err := os.Mkdir(root, 0755); err != nil {
		t.Fatal(err)
	}
	if err := project.WriteModFile(project.ModPath(root), &project.ModFile{Module: "example/project", Deps: []string{oneSource, twoSource}}); err != nil {
		t.Fatal(err)
	}
	prepared, err := (generation.Service{}).PrepareSync(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := prepared.Commit(); err != nil {
		t.Fatal(err)
	}
	oneNew := commitGitUpdateDependency(t, base, "one", "2")
	twoNew := commitGitUpdateDependency(t, base, "two", "2")

	ready := make(chan struct{}, 2)
	release := make(chan struct{})
	loadState := func(root string) (*project.ModFile, *project.LockFile, error) {
		mod, lock, err := loadModAndMatchingLock(root)
		ready <- struct{}{}
		<-release
		return mod, lock, err
	}
	type outcome struct {
		lock *project.LockFile
		err  error
	}
	outcomes := make(chan outcome, 2)
	for _, target := range []string{oneSource, twoSource} {
		target := target
		go func() {
			lock, _, err := runUpdateWorkflow(root, updateConfig{targetSource: target, loadState: loadState})
			outcomes <- outcome{lock: lock, err: err}
		}()
	}
	<-ready
	<-ready
	close(release)

	var successes, conflicts int
	for range 2 {
		result := <-outcomes
		if result.err == nil {
			successes++
			continue
		}
		if strings.Contains(result.err.Error(), "current generation changed") {
			conflicts++
			continue
		}
		t.Fatalf("concurrent update error = %v", result.err)
	}
	if successes != 1 || conflicts != 1 {
		t.Fatalf("concurrent outcomes = %d success, %d conflict; want one each", successes, conflicts)
	}

	_, lock, err := loadModAndMatchingLock(root)
	if err != nil {
		t.Fatal(err)
	}
	wantRevisions := map[string][2]string{
		oneSource: {oneOld, oneNew},
		twoSource: {twoOld, twoNew},
	}
	updated := 0
	for _, dep := range lock.Deps {
		want, ok := wantRevisions[dep.Source]
		if !ok {
			continue
		}
		switch dep.Revision {
		case want[0]:
		case want[1]:
			updated++
		default:
			t.Fatalf("dependency %s revision = %s, want %s or %s", dep.Source, dep.Revision, want[0], want[1])
		}
	}
	if updated != 1 {
		t.Fatalf("committed updated revisions = %d, want 1", updated)
	}
}

func TestServiceUpdateDependenciesPinsUnselectedGitSources(t *testing.T) {
	target := "https://example.com/one.git?ref=main"
	other := "https://example.com/two.git?ref=main"
	oldRevision := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	direct := []string{target, other}
	lock := &project.LockFile{Deps: []project.LockDep{
		{Source: target, Kind: "git", ModuleID: "doggo:one", Revision: "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"},
		{Source: other, Kind: "git", ModuleID: "doggo:two", Revision: oldRevision},
	}}

	sources, _, err := selectUpdateSources("/tmp/project", direct, lock, target, false)
	if err != nil {
		t.Fatal(err)
	}
	wantSources := []string{target, "https://example.com/two.git?ref=" + oldRevision}
	if !reflect.DeepEqual(sources, wantSources) {
		t.Fatalf("effective sources = %#v, want %#v", sources, wantSources)
	}
}

func TestSelectUpdateSourcesPinsOnlyUnselectedFloatingGoSources(t *testing.T) {
	target := "example.com/one@latest"
	other := "example.com/two"
	exact := "example.com/three@v1.0.0"
	direct := []string{target, other, exact}
	lock := &project.LockFile{Deps: []project.LockDep{
		{Source: target, Kind: "go", GoModule: "example.com/one", Revision: "v1.2.0"},
		{Source: other, Kind: "go", GoModule: "example.com/two", Revision: "v1.3.4"},
		{Source: exact, Kind: "go", GoModule: "example.com/three", Revision: "v1.0.0"},
	}}

	sources, aliases, err := selectUpdateSources("/tmp/project", direct, lock, target, false)
	if err != nil {
		t.Fatal(err)
	}
	wantSources := []string{target, "example.com/two@v1.3.4", exact}
	if !reflect.DeepEqual(sources, wantSources) {
		t.Fatalf("effective sources = %#v, want %#v", sources, wantSources)
	}
	restored, err := restoreDeclaredSources("/tmp/project", []project.LockDep{
		{Source: target, Kind: "go"},
		{Source: "example.com/two@v1.3.4", Kind: "go"},
		{Source: exact, Kind: "go"},
	}, aliases)
	if err != nil {
		t.Fatal(err)
	}
	wantRestored := project.NormalizeLockDeps([]project.LockDep{
		{Source: target, Kind: "go"},
		{Source: other, Kind: "go"},
		{Source: exact, Kind: "go"},
	})
	if !reflect.DeepEqual(restored, wantRestored) {
		t.Fatalf("restored dependencies = %#v, want %#v", restored, wantRestored)
	}
}

func TestSelectUpdateSourcesRejectsIdentityCollision(t *testing.T) {
	const omitted = "example.com/math"
	const latest = "example.com/math@latest"
	_, _, err := selectUpdateSources("/tmp/project", []string{omitted, latest}, &project.LockFile{}, "", true)
	if err == nil || !strings.Contains(err.Error(), omitted) || !strings.Contains(err.Error(), latest) || !strings.Contains(err.Error(), "example.com/math") {
		t.Fatalf("select error = %v, want both declarations and identity", err)
	}
}

func TestSelectUpdateSourcesRejectsMissingOrInvalidFloatingGoLock(t *testing.T) {
	const target = "example.com/one@latest"
	const other = "example.com/two"
	for _, tt := range []struct {
		name string
		lock *project.LockFile
		want string
	}{
		{name: "missing", lock: &project.LockFile{}, want: "is not locked"},
		{name: "invalid version", lock: &project.LockFile{Deps: []project.LockDep{{Source: other, Kind: "go", Revision: "main"}}}, want: "invalid Go module revision"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			_, _, err := selectUpdateSources("/tmp/project", []string{target, other}, tt.lock, target, false)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("select error = %v, want %q", err, tt.want)
			}
		})
	}
}

func TestSelectUpdateSourcesAllValidatesDeclarations(t *testing.T) {
	if _, _, err := selectUpdateSources("/tmp/project", []string{"github.com/acme/math@main"}, &project.LockFile{}, "", true); err == nil {
		t.Fatal("--all accepted an invalid declaration")
	}
}

func TestSourceIdentityKeyPropagatesAbsolutePathError(t *testing.T) {
	original := filepathAbs
	want := errors.New("absolute path failed")
	filepathAbs = func(string) (string, error) { return "", want }
	t.Cleanup(func() { filepathAbs = original })

	if _, err := sourceIdentityKey("/tmp/project", "./dep"); !errors.Is(err, want) {
		t.Fatalf("sourceIdentityKey error = %v, want %v", err, want)
	}
}

func TestSelectUpdateSourcesAllKeepsDeclaredSources(t *testing.T) {
	direct := []string{
		"https://example.com/one.git?ref=main",
		"https://example.com/two.git?ref=release",
	}
	sources, _, err := selectUpdateSources("/tmp/project", direct, &project.LockFile{}, "", true)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(sources, direct) {
		t.Fatalf("effective sources = %#v, want %#v", sources, direct)
	}
}

func TestServiceUpdateAuditFailurePreservesLock(t *testing.T) {
	root, _ := updateProjectFixture(t)
	before, err := os.ReadFile(project.LockPath(root))
	if err != nil {
		t.Fatal(err)
	}
	want := errors.New("audit failed")
	service := Service{
		ResolveProjectRoot: func() (string, bool, error) { return root, true, nil },
		Auditor: Auditor{AuditLock: func(string, string, *project.ModFile, *project.LockFile) ([]DependencyAuditFinding, error) {
			return nil, want
		}},
	}
	if _, _, err := service.UpdateDependencies("./dep", false); !errors.Is(err, want) {
		t.Fatalf("update error = %v", err)
	}
	after, err := os.ReadFile(project.LockPath(root))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, before) {
		t.Fatal("failed update changed doggo.lock")
	}
}

func TestServiceUpdateCommitFailurePreservesLock(t *testing.T) {
	root, _ := updateProjectFixture(t)
	before, err := os.ReadFile(project.LockPath(root))
	if err != nil {
		t.Fatal(err)
	}
	service := Service{
		ResolveProjectRoot: func() (string, bool, error) { return root, true, nil },
		Auditor: Auditor{AuditLock: func(_ string, _ string, _ *project.ModFile, lock *project.LockFile) ([]DependencyAuditFinding, error) {
			lock.LockVersion++
			return nil, nil
		}},
	}
	if _, _, err := service.UpdateDependencies("./dep", false); err == nil {
		t.Fatal("update unexpectedly committed an invalid tentative lock")
	}
	after, err := os.ReadFile(project.LockPath(root))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, before) {
		t.Fatal("failed update changed doggo.lock")
	}
}

func TestServiceUpdateMissingSelectedResultPreservesLock(t *testing.T) {
	root, _ := updateProjectFixture(t)
	before, err := os.ReadFile(project.LockPath(root))
	if err != nil {
		t.Fatal(err)
	}
	service := Service{
		ResolveProjectRoot: func() (string, bool, error) { return root, true, nil },
		PrepareSelectedSync: func(root, selected string, previous *project.LockFile, direct []project.LockDep) (*generation.PreparedGeneration, error) {
			prepared, err := (generation.Service{}).PrepareSelectedSync(root, selected, previous, direct)
			if err == nil {
				prepared.Lock.Deps = nil
			}
			return prepared, err
		},
		Auditor: Auditor{AuditLock: func(string, string, *project.ModFile, *project.LockFile) ([]DependencyAuditFinding, error) {
			return nil, nil
		}},
	}
	if _, _, err := service.UpdateDependencies("./dep", false); err == nil || !strings.Contains(err.Error(), "does not contain dependency source") {
		t.Fatalf("update error = %v, want missing selected result", err)
	}
	after, err := os.ReadFile(project.LockPath(root))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(after, before) {
		t.Fatal("missing selected result changed doggo.lock")
	}
}

func TestServiceUpdateJoinsAbortFailure(t *testing.T) {
	base := t.TempDir()
	root := filepath.Join(base, "project")
	moved := filepath.Join(base, "project-original")
	writeUpdateProjectFixture(t, root)
	want := errors.New("audit failed")
	var racer string
	service := Service{
		ResolveProjectRoot: func() (string, bool, error) { return root, true, nil },
		Auditor: Auditor{AuditLock: func(stage, _ string, _ *project.ModFile, _ *project.LockFile) ([]DependencyAuditFinding, error) {
			if !testutil.RenameRetained(t, root, moved) {
				racer = filepath.Join(stage, "toolchain", "main.go")
				testutil.ReplaceOwnedFile(t, racer)
				return nil, want
			}
			if err := os.Mkdir(root, 0755); err != nil {
				t.Fatal(err)
			}
			return nil, want
		}},
	}
	_, _, err := service.UpdateDependencies("./dep", false)
	if !errors.Is(err, want) || !errors.Is(err, project.ErrOwnershipChanged) {
		t.Fatalf("update error = %v, want audit and abort failures", err)
	}
	if racer != "" {
		if content, err := os.ReadFile(racer); err != nil || string(content) != "replacement" {
			t.Fatalf("abort changed replacement: %q, %v", content, err)
		}
	}
}

func TestDirectDependencyWinsTransitiveAliasAcrossWorkflows(t *testing.T) {
	root := t.TempDir()
	for _, name := range []string{"a", "shared"} {
		if err := os.Mkdir(filepath.Join(root, name), 0755); err != nil {
			t.Fatal(err)
		}
	}
	if err := project.WriteModFile(project.ModPath(filepath.Join(root, "a")), &project.ModFile{Module: "example/a", Deps: []string{"../shared"}}); err != nil {
		t.Fatal(err)
	}
	if err := project.WriteModFile(project.ModPath(filepath.Join(root, "shared")), &project.ModFile{Module: "example/shared", Deps: []string{}}); err != nil {
		t.Fatal(err)
	}
	mod := &project.ModFile{Module: "example/project", Deps: []string{"./a", "./shared"}}
	if err := project.WriteModFile(project.ModPath(root), mod); err != nil {
		t.Fatal(err)
	}
	prepared, err := (generation.Service{}).PrepareSync(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := prepared.Commit(); err != nil {
		t.Fatal(err)
	}

	shared, ok, err := findLockDepBySource(prepared.Lock.Deps, "./shared", "path")
	if err != nil || !ok || shared.ModuleID != "doggo:example/shared" {
		t.Fatalf("direct shared lock = %#v, ok=%t, err=%v; deps=%#v", shared, ok, err, prepared.Lock.Deps)
	}
	statuses, err := OutdatedDependencies(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, status := range statuses {
		if status.Source == "./shared" && strings.Contains(status.CheckError, "not locked") {
			t.Fatalf("direct shared outdated status = %#v", status)
		}
	}
	findings, err := auditDependencies(root, root, mod, prepared.Lock)
	if err != nil {
		t.Fatal(err)
	}
	for _, finding := range findings {
		if finding.Source == "./shared" && finding.Code == "unlocked-dependency" {
			t.Fatalf("direct shared audit finding = %#v", finding)
		}
	}
	service := Service{
		ResolveProjectRoot: func() (string, bool, error) { return root, true, nil },
		Auditor: Auditor{AuditLock: func(string, string, *project.ModFile, *project.LockFile) ([]DependencyAuditFinding, error) {
			return nil, nil
		}},
	}
	if _, _, err := service.UpdateDependencies("./shared", false); err != nil {
		t.Fatalf("update direct shared: %v", err)
	}
}

func TestDirectGitBranchWinsLaterTransitiveCommitAliasAcrossWorkflows(t *testing.T) {
	base := t.TempDir()
	sharedSource, sharedRevision := createGitUpdateDependency(t, base, "a-shared")
	exactSharedSource := strings.TrimSuffix(sharedSource, "main") + sharedRevision
	parentSource, _ := createGitUpdateDependency(t, base, "z-parent")
	if sharedSource >= parentSource {
		t.Fatalf("fixture order = %q then %q", sharedSource, parentSource)
	}
	if err := project.WriteModFile(project.ModPath(filepath.Join(base, "z-parent")), &project.ModFile{
		Module: "example/z-parent", Deps: []string{exactSharedSource},
	}); err != nil {
		t.Fatal(err)
	}
	commitGitUpdateDependency(t, base, "z-parent", "2")

	root := filepath.Join(base, "project")
	if err := os.Mkdir(root, 0755); err != nil {
		t.Fatal(err)
	}
	mod := &project.ModFile{Module: "example/project", Deps: []string{sharedSource, parentSource}}
	if err := project.WriteModFile(project.ModPath(root), mod); err != nil {
		t.Fatal(err)
	}
	prepared, err := (generation.Service{}).PrepareSync(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := prepared.Commit(); err != nil {
		t.Fatal(err)
	}

	shared, ok, err := findLockDepBySource(prepared.Lock.Deps, sharedSource, "git")
	if err != nil || !ok || shared.ModuleID != "doggo:example/a-shared" {
		t.Fatalf("direct shared lock = %#v, ok=%t, err=%v; deps=%#v", shared, ok, err, prepared.Lock.Deps)
	}
	statuses, err := OutdatedDependencies(root)
	if err != nil {
		t.Fatal(err)
	}
	for _, status := range statuses {
		if status.Source == sharedSource && strings.Contains(status.CheckError, "not locked") {
			t.Fatalf("direct shared outdated status = %#v", status)
		}
	}
	findings, err := auditDependencies(root, root, mod, prepared.Lock)
	if err != nil {
		t.Fatal(err)
	}
	for _, finding := range findings {
		if finding.Source == sharedSource && finding.Code == "unlocked-dependency" {
			t.Fatalf("direct shared audit finding = %#v", finding)
		}
	}
	service := Service{
		ResolveProjectRoot: func() (string, bool, error) { return root, true, nil },
		Auditor: Auditor{AuditLock: func(string, string, *project.ModFile, *project.LockFile) ([]DependencyAuditFinding, error) {
			return nil, nil
		}},
	}
	if _, _, err := service.UpdateDependencies(sharedSource, false); err != nil {
		t.Fatalf("update direct shared: %v", err)
	}
}

func updateProjectFixture(t *testing.T) (string, *project.LockFile) {
	t.Helper()
	root := t.TempDir()
	return root, writeUpdateProjectFixture(t, root)
}

func writeUpdateProjectFixture(t *testing.T, root string) *project.LockFile {
	t.Helper()
	if err := os.MkdirAll(root, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "dep"), 0755); err != nil {
		t.Fatal(err)
	}
	if err := project.WriteModFile(project.ModPath(filepath.Join(root, "dep")), &project.ModFile{Module: "example/dep", Deps: []string{}}); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "dep", "value.dog"), []byte("(def value 1)\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := project.WriteModFile(project.ModPath(root), &project.ModFile{Module: "example/project", Deps: []string{"./dep"}}); err != nil {
		t.Fatal(err)
	}
	prepared, err := (generation.Service{}).PrepareSync(root, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := prepared.Commit(); err != nil {
		t.Fatal(err)
	}
	return prepared.Lock
}

func createGitUpdateDependency(t *testing.T, base, name string) (string, string) {
	t.Helper()
	repository := filepath.Join(base, name)
	if err := os.Mkdir(repository, 0755); err != nil {
		t.Fatal(err)
	}
	runUpdateGit(t, repository, "init")
	runUpdateGit(t, repository, "symbolic-ref", "HEAD", "refs/heads/main")
	runUpdateGit(t, repository, "config", "user.email", "doggo@example.invalid")
	runUpdateGit(t, repository, "config", "user.name", "Doggo Test")
	if err := project.WriteModFile(project.ModPath(repository), &project.ModFile{Module: "example/" + name, Deps: []string{}}); err != nil {
		t.Fatal(err)
	}
	urlPath := filepath.ToSlash(repository)
	if filepath.VolumeName(repository) != "" {
		urlPath = "/" + urlPath
	}
	return (&url.URL{Scheme: "file", Path: urlPath}).String() + "?ref=main", commitGitUpdateDependency(t, base, name, "1")
}

func commitGitUpdateDependency(t *testing.T, base, name, value string) string {
	t.Helper()
	repository := filepath.Join(base, name)
	if err := os.WriteFile(filepath.Join(repository, "value.dog"), []byte("(def value "+value+")\n"), 0644); err != nil {
		t.Fatal(err)
	}
	runUpdateGit(t, repository, "add", ".")
	runUpdateGit(t, repository, "commit", "-m", "value "+value)
	return strings.TrimSpace(runUpdateGit(t, repository, "rev-parse", "HEAD"))
}

func runUpdateGit(t *testing.T, directory string, args ...string) string {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = directory
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v (%s)", args, err, output)
	}
	return string(output)
}
