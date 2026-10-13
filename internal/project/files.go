package project

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"doggo/internal/syntax/frontend"
	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
)

var afterInitModLeaseForTest func()

func InitModFile(projectRoot string) (err error) {
	lease, err := AcquireGenerationLease(projectRoot, true)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, lease.Close()) }()
	if afterInitModLeaseForTest != nil {
		afterInitModLeaseForTest()
	}
	return lease.CreateMod(&ModFile{Deps: []string{}})
}

func LoadModFile(path string) (*ModFile, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return parseModFileBytes(path, content)
}

func parseModFileBytes(path string, content []byte) (*ModFile, error) {
	form, err := parseSingleFormBytes(path, content)
	if err != nil {
		return nil, err
	}
	m, ok := form.(*coll.Map)
	if !ok {
		return nil, fmt.Errorf("%s: root must be a map", path)
	}

	mod := &ModFile{Deps: []string{}}
	if moduleVal := mapGetByKeyword(m, "module"); moduleVal != nil {
		str, ok := moduleVal.(atom.String)
		if !ok {
			return nil, fmt.Errorf("%s: :module must be a string", path)
		}
		mod.Module = string(str)
	}
	if nameVal := mapGetByKeyword(m, "name"); nameVal != nil {
		str, ok := nameVal.(atom.String)
		if !ok {
			return nil, fmt.Errorf("%s: :name must be a string", path)
		}
		mod.Name = string(str)
	}
	if versionVal := mapGetByKeyword(m, "version"); versionVal != nil {
		str, ok := versionVal.(atom.String)
		if !ok {
			return nil, fmt.Errorf("%s: :version must be a string", path)
		}
		mod.Version = string(str)
	}
	if licenseVal := mapGetByKeyword(m, "license"); licenseVal != nil {
		str, ok := licenseVal.(atom.String)
		if !ok {
			return nil, fmt.Errorf("%s: :license must be a string", path)
		}
		mod.License = string(str)
	}
	if compatVal := mapGetByKeyword(m, "compat"); compatVal != nil {
		str, ok := compatVal.(atom.String)
		if !ok {
			return nil, fmt.Errorf("%s: :compat must be a string", path)
		}
		mod.Compat = string(str)
	}

	depsVal := mapGetByKeyword(m, "deps")
	if depsVal == nil {
		return nil, fmt.Errorf("%s: :deps is required", path)
	}
	depsVec, ok := depsVal.(*coll.Vector)
	if !ok {
		return nil, fmt.Errorf("%s: :deps must be a vector of strings", path)
	}
	deps := make([]string, 0, len(depsVec.Items))
	for _, item := range depsVec.Items {
		depStr, ok := item.(atom.String)
		if !ok {
			return nil, fmt.Errorf("%s: :deps entries must be strings", path)
		}
		deps = append(deps, string(depStr))
	}
	mod.Deps = normalizeStringSlice(deps)
	return mod, nil
}

func WriteModFile(path string, mod *ModFile) error {
	norm := normalizeMod(mod)
	content := canonicalModString(norm)
	return os.WriteFile(path, []byte(content+"\n"), 0644)
}

func canonicalModString(mod *ModFile) string {
	deps := quoteStrings(mod.Deps)
	fields := make([]string, 0, 6)
	if mod.Module != "" {
		fields = append(fields, fmt.Sprintf(":module %s", strconv.Quote(mod.Module)))
	}
	if mod.Name != "" {
		fields = append(fields, fmt.Sprintf(":name %s", strconv.Quote(mod.Name)))
	}
	if mod.Version != "" {
		fields = append(fields, fmt.Sprintf(":version %s", strconv.Quote(mod.Version)))
	}
	if mod.License != "" {
		fields = append(fields, fmt.Sprintf(":license %s", strconv.Quote(mod.License)))
	}
	if mod.Compat != "" {
		fields = append(fields, fmt.Sprintf(":compat %s", strconv.Quote(mod.Compat)))
	}
	fields = append(fields, fmt.Sprintf(":deps [%s]", strings.Join(deps, " ")))
	return fmt.Sprintf("{%s}", strings.Join(fields, " "))
}

func (l *GenerationLease) LoadMod() (*ModFile, error) {
	if err := l.verifyProject(); err != nil {
		return nil, err
	}
	content, err := readVerifiedRootFile(l.project, ModFileName)
	if err != nil {
		return nil, errors.Join(err, l.verifyProject())
	}
	if err := l.verifyProject(); err != nil {
		return nil, err
	}
	return parseModFileBytes(ModFileName, content)
}

func (l *GenerationLease) CreateMod(mod *ModFile) error {
	if err := l.requireExclusive(); err != nil {
		return err
	}
	if _, err := l.project.Lstat(ModFileName); err == nil {
		return fmt.Errorf("%s already exists", ModFileName)
	} else if !errors.Is(err, os.ErrNotExist) {
		return errors.Join(err, l.verifyProject())
	}
	return l.writeMod(mod)
}

func (l *GenerationLease) WriteMod(mod *ModFile) error {
	if err := l.requireExclusive(); err != nil {
		return err
	}
	return l.writeMod(mod)
}

func (l *GenerationLease) writeMod(mod *ModFile) error {
	norm := normalizeMod(mod)
	writeErr := WriteFileAtomicRoot(l.project, ModFileName, []byte(canonicalModString(norm)+"\n"), 0644)
	return errors.Join(writeErr, l.verifyProject())
}

func HashModLockInputs(mod *ModFile) string {
	norm := normalizeMod(mod)
	sum := sha256.Sum256([]byte(canonicalModLockString(norm)))
	return hex.EncodeToString(sum[:])
}

func canonicalModLockString(mod *ModFile) string {
	deps := quoteStrings(mod.Deps)
	if mod.Module == "" {
		return fmt.Sprintf("{:deps [%s]}", strings.Join(deps, " "))
	}
	return fmt.Sprintf("{:module %s :deps [%s]}", strconv.Quote(mod.Module), strings.Join(deps, " "))
}

func LoadLockFile(path string) (*LockFile, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return parseLockFileBytes(path, content)
}

func parseLockFileBytes(path string, content []byte) (*LockFile, error) {
	form, err := parseSingleFormBytes(path, content)
	if err != nil {
		return nil, err
	}
	m, ok := form.(*coll.Map)
	if !ok {
		return nil, fmt.Errorf("%s: root must be a map", path)
	}

	lf := &LockFile{}
	lockVersion, err := mustInt(mapGetByKeyword(m, "lock-version"), path, ":lock-version")
	if err != nil {
		return nil, err
	}
	lf.LockVersion = lockVersion
	if lockVersion != LockVersion {
		return nil, fmt.Errorf("unsupported lock version: %d", lockVersion)
	}
	generation, err := mustString(mapGetByKeyword(m, "generation"), path, ":generation")
	if err != nil {
		return nil, err
	}
	lf.Generation = generation
	modLockInputsSHA, err := mustString(mapGetByKeyword(m, "mod-lock-inputs-sha256"), path, ":mod-lock-inputs-sha256")
	if err != nil {
		return nil, err
	}
	lf.ModLockInputsSHA256 = modLockInputsSHA

	depMaps, err := mapVectorEntries(mapGetByKeyword(m, "deps"), path, ":deps")
	if err != nil {
		return nil, err
	}
	lf.Deps = make([]LockDep, 0, len(depMaps))
	for i, depMap := range depMaps {
		fieldPrefix := fmt.Sprintf(":deps[%d]", i)
		source, err := mustString(mapGetByKeyword(depMap, "source"), path, fmt.Sprintf("%s.:source", fieldPrefix))
		if err != nil {
			return nil, err
		}
		kind, err := mustKeywordName(mapGetByKeyword(depMap, "kind"), path, fmt.Sprintf(":deps[%d].:kind", i))
		if err != nil {
			return nil, err
		}
		fields, err := mustStringFields(depMap, path, fieldPrefix, "module-id", "go-module", "revision", "digest", "install-dir")
		if err != nil {
			return nil, err
		}
		lf.Deps = append(lf.Deps, LockDep{
			Source:     source,
			Kind:       kind,
			ModuleID:   fields["module-id"],
			GoModule:   fields["go-module"],
			Revision:   fields["revision"],
			Digest:     fields["digest"],
			InstallDir: fields["install-dir"],
		})
	}

	scanVal := mapGetByKeyword(m, "scan")
	scanMap, ok := scanVal.(*coll.Map)
	if !ok {
		return nil, fmt.Errorf("%s: :scan must be a map", path)
	}
	goPkgs, err := mustStringVector(mapGetByKeyword(scanMap, "go-packages"), path, ":scan.:go-packages")
	if err != nil {
		return nil, err
	}
	reifyMaps, err := mapVectorEntries(mapGetByKeyword(scanMap, "reify-targets"), path, ":scan.:reify-targets")
	if err != nil {
		return nil, err
	}
	reifyTargets := make([]ReifyTarget, 0, len(reifyMaps))
	for i, reifyMap := range reifyMaps {
		fieldPrefix := fmt.Sprintf(":scan.:reify-targets[%d]", i)
		fields, err := mustStringFields(reifyMap, path, fieldPrefix, "source", "runtime")
		if err != nil {
			return nil, err
		}
		reifyTargets = append(reifyTargets, ReifyTarget{Source: fields["source"], Runtime: fields["runtime"]})
	}
	lf.Scan = LockScan{
		GoPackages:   normalizeStringSlice(goPkgs),
		ReifyTargets: NormalizeReifyTargets(reifyTargets),
	}

	buildVal := mapGetByKeyword(m, "build")
	buildMap, ok := buildVal.(*coll.Map)
	if !ok {
		return nil, fmt.Errorf("%s: :build must be a map", path)
	}
	binary, err := mustString(mapGetByKeyword(buildMap, "binary"), path, ":build.:binary")
	if err != nil {
		return nil, err
	}
	sha, err := mustString(mapGetByKeyword(buildMap, "sha256"), path, ":build.:sha256")
	if err != nil {
		return nil, err
	}
	lf.Build = LockBuild{Binary: binary, SHA256: sha}

	return lf, nil
}

func WriteLockFile(path string, lock *LockFile) error {
	if lock == nil {
		return fmt.Errorf("lock file is nil")
	}
	content := canonicalLockString(lock)
	return os.WriteFile(path, []byte(content+"\n"), 0644)
}

func canonicalLockString(lock *LockFile) string {
	depsEntries := make([]string, 0, len(lock.Deps))
	for _, dep := range NormalizeLockDeps(lock.Deps) {
		depsEntries = append(depsEntries, fmt.Sprintf("{:source %s :kind :%s :module-id %s :go-module %s :revision %s :digest %s :install-dir %s}",
			strconv.Quote(dep.Source), dep.Kind, strconv.Quote(dep.ModuleID), strconv.Quote(dep.GoModule), strconv.Quote(dep.Revision), strconv.Quote(dep.Digest), strconv.Quote(dep.InstallDir),
		))
	}
	scanGo := quoteStrings(normalizeStringSlice(lock.Scan.GoPackages))
	reifyEntries := make([]string, 0, len(lock.Scan.ReifyTargets))
	for _, target := range NormalizeReifyTargets(lock.Scan.ReifyTargets) {
		reifyEntries = append(reifyEntries, fmt.Sprintf("{:source %s :runtime %s}", strconv.Quote(target.Source), strconv.Quote(target.Runtime)))
	}
	return fmt.Sprintf(`{:lock-version %d :generation %s :mod-lock-inputs-sha256 %s :deps [%s] :scan {:go-packages [%s] :reify-targets [%s]} :build {:binary %s :sha256 %s}}`,
		lock.LockVersion,
		strconv.Quote(lock.Generation),
		strconv.Quote(lock.ModLockInputsSHA256),
		strings.Join(depsEntries, " "),
		strings.Join(scanGo, " "),
		strings.Join(reifyEntries, " "),
		strconv.Quote(lock.Build.Binary),
		strconv.Quote(lock.Build.SHA256),
	)
}

func parseSingleForm(path string) (types.Value, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return parseSingleFormBytes(path, content)
}

func parseSingleFormBytes(path string, content []byte) (types.Value, error) {
	return frontend.ReadSingleForm(path, string(content))
}

func quoteStrings(values []string) []string {
	out := make([]string, 0, len(values))
	for _, v := range values {
		out = append(out, strconv.Quote(v))
	}
	return out
}

func mapGetByKeyword(m *coll.Map, key string) types.Value {
	if m == nil {
		return nil
	}
	return m.Get(atom.NewKeyword(key))
}

func mapVectorEntries(v types.Value, filePath, field string) ([]*coll.Map, error) {
	vec, ok := v.(*coll.Vector)
	if !ok {
		return nil, fmt.Errorf("%s: %s must be a vector", filePath, field)
	}
	entries := make([]*coll.Map, 0, len(vec.Items))
	for i, item := range vec.Items {
		entry, ok := item.(*coll.Map)
		if !ok {
			return nil, fmt.Errorf("%s: %s[%d] must be a map", filePath, field, i)
		}
		entries = append(entries, entry)
	}
	return entries, nil
}

func mustString(v types.Value, filePath, field string) (string, error) {
	if v == nil {
		return "", fmt.Errorf("%s: missing %s", filePath, field)
	}
	s, ok := v.(atom.String)
	if !ok {
		return "", fmt.Errorf("%s: %s must be a string", filePath, field)
	}
	return string(s), nil
}

func mustStringFields(m *coll.Map, filePath, prefix string, names ...string) (map[string]string, error) {
	out := make(map[string]string, len(names))
	for _, name := range names {
		value, err := mustString(mapGetByKeyword(m, name), filePath, fmt.Sprintf("%s.:%s", prefix, name))
		if err != nil {
			return nil, err
		}
		out[name] = value
	}
	return out, nil
}

func mustKeywordName(v types.Value, filePath, field string) (string, error) {
	if v == nil {
		return "", fmt.Errorf("%s: missing %s", filePath, field)
	}
	kw, ok := v.(atom.Keyword)
	if !ok {
		return "", fmt.Errorf("%s: %s must be a keyword", filePath, field)
	}
	return string(kw), nil
}

func mustStringVector(v types.Value, filePath, field string) ([]string, error) {
	if v == nil {
		return nil, fmt.Errorf("%s: missing %s", filePath, field)
	}
	vec, ok := v.(*coll.Vector)
	if !ok {
		return nil, fmt.Errorf("%s: %s must be a vector", filePath, field)
	}
	out := make([]string, 0, len(vec.Items))
	for i, item := range vec.Items {
		str, ok := item.(atom.String)
		if !ok {
			return nil, fmt.Errorf("%s: %s[%d] must be a string", filePath, field, i)
		}
		out = append(out, string(str))
	}
	return out, nil
}

func mustInt(v types.Value, filePath, field string) (int, error) {
	if v == nil {
		return 0, fmt.Errorf("%s: missing %s", filePath, field)
	}
	n, ok := v.(*atom.Number)
	if !ok || n.IntVal == nil {
		return 0, fmt.Errorf("%s: %s must be an integer", filePath, field)
	}
	return int(*n.IntVal), nil
}

func ModPath(root string) string {
	return filepath.Join(root, ModFileName)
}

func LockPath(root string) string {
	return filepath.Join(root, LockFileName)
}
