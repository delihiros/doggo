package project

import (
	"cmp"
	"sort"
)

const (
	ModFileName  = "doggo.mod"
	LockFileName = "doggo.lock"
	LockVersion  = 3
)

type ModFile struct {
	Module  string
	Name    string
	Version string
	License string
	Compat  string
	Deps    []string
}

type LockDep struct {
	Source     string
	Kind       string
	ModuleID   string
	GoModule   string
	Revision   string
	Digest     string
	InstallDir string
}

type ReifyTarget struct {
	Source  string
	Runtime string
}

type LockScan struct {
	GoPackages   []string
	ReifyTargets []ReifyTarget
}

type LockBuild struct {
	Binary string
	SHA256 string
}

type LockFile struct {
	LockVersion         int
	Generation          string
	ModLockInputsSHA256 string
	Deps                []LockDep
	Scan                LockScan
	Build               LockBuild
}

type RuntimeConfig struct {
	ProjectRoot       string
	LoadPaths         []string
	GenerationSources []*GenerationSource
	Lock              *LockFile
}

type RegistryPackage struct {
	Name    string
	Version string
	Source  string
	License string
	Compat  string
}

type RegistryAdvisory struct {
	Name     string
	Range    string
	Severity string
	ID       string
	Message  string
}

type RegistryFile struct {
	Packages   []RegistryPackage
	Advisories []RegistryAdvisory
}

// NormalizeStringSlice canonicalizes schema-owned string lists by dropping empty
// values, deduplicating, and sorting for stable project file semantics.
func NormalizeStringSlice(values []string) []string {
	return normalizeStringSlice(values)
}

func normalizeStringSlice(values []string) []string {
	if len(values) == 0 {
		return []string{}
	}
	set := make(map[string]struct{}, len(values))
	for _, v := range values {
		if v == "" {
			continue
		}
		set[v] = struct{}{}
	}
	out := make([]string, 0, len(set))
	for v := range set {
		out = append(out, v)
	}
	sort.Strings(out)
	return out
}

// NormalizeLockDeps deduplicates lock dependencies by full identity and sorts
// them for stable lock file semantics.
func NormalizeLockDeps(deps []LockDep) []LockDep {
	if len(deps) == 0 {
		return []LockDep{}
	}
	set := make(map[LockDep]struct{}, len(deps))
	for _, dep := range deps {
		set[dep] = struct{}{}
	}
	out := make([]LockDep, 0, len(set))
	for dep := range set {
		out = append(out, dep)
	}
	sort.Slice(out, func(i, j int) bool {
		return compareLockDeps(out[i], out[j]) < 0
	})
	return out
}

func compareLockDeps(a, b LockDep) int {
	if c := cmp.Compare(a.ModuleID, b.ModuleID); c != 0 {
		return c
	}
	if c := cmp.Compare(a.GoModule, b.GoModule); c != 0 {
		return c
	}
	if c := cmp.Compare(a.Revision, b.Revision); c != 0 {
		return c
	}
	if c := cmp.Compare(a.Digest, b.Digest); c != 0 {
		return c
	}
	if c := cmp.Compare(a.Source, b.Source); c != 0 {
		return c
	}
	if c := cmp.Compare(a.Kind, b.Kind); c != 0 {
		return c
	}
	return cmp.Compare(a.InstallDir, b.InstallDir)
}

// NormalizeReifyTargets deduplicates source/runtime pairs and sorts by runtime
// identity, then source spelling.
func NormalizeReifyTargets(targets []ReifyTarget) []ReifyTarget {
	if len(targets) == 0 {
		return []ReifyTarget{}
	}
	set := make(map[ReifyTarget]struct{}, len(targets))
	for _, target := range targets {
		set[target] = struct{}{}
	}
	out := make([]ReifyTarget, 0, len(set))
	for target := range set {
		out = append(out, target)
	}
	sort.Slice(out, func(i, j int) bool {
		if c := cmp.Compare(out[i].Runtime, out[j].Runtime); c != 0 {
			return c < 0
		}
		return out[i].Source < out[j].Source
	})
	return out
}

func normalizeMod(mod *ModFile) *ModFile {
	if mod == nil {
		return &ModFile{Deps: []string{}}
	}
	return &ModFile{
		Module:  mod.Module,
		Name:    mod.Name,
		Version: mod.Version,
		License: mod.License,
		Compat:  mod.Compat,
		Deps:    normalizeStringSlice(mod.Deps),
	}
}
