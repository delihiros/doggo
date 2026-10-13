package project

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"doggo/internal/types"
	"doggo/internal/types/atom"
	"doggo/internal/types/coll"
	"golang.org/x/mod/semver"
)

// SortRegistryPackages applies the canonical on-disk ordering for registry entries.
func SortRegistryPackages(packages []RegistryPackage) {
	sortRegistryPackages(packages)
}

// IsSemver reports whether a registry version participates in canonical semver handling.
func IsSemver(v string) bool {
	return isSemver(v)
}

// CompareRegistryVersions compares valid semver canonically, ranks valid versions above invalid ones,
// and compares invalid versions lexically after trimming whitespace.
func CompareRegistryVersions(a, b string) int {
	return compareSemver(a, b)
}

// NormalizeRegistryVersion trims registry versions and canonicalizes valid semver.
func NormalizeRegistryVersion(v string) string {
	return normalizeRegistryVersion(v)
}

// RegistryVersionsEqual compares valid semver canonically and invalid versions after trimming whitespace.
func RegistryVersionsEqual(a, b string) bool {
	ca := normalizeRegistryVersion(a)
	cb := normalizeRegistryVersion(b)
	if ca != "" && cb != "" && isSemver(ca) && isSemver(cb) {
		return ca == cb
	}
	return strings.TrimSpace(a) == strings.TrimSpace(b)
}

func LoadRegistryFile(path string) (*RegistryFile, error) {
	form, err := parseSingleForm(path)
	if err != nil {
		return nil, err
	}
	m, ok := form.(*coll.Map)
	if !ok {
		return nil, fmt.Errorf("%s: root must be a map", path)
	}

	registry := &RegistryFile{
		Packages:   []RegistryPackage{},
		Advisories: []RegistryAdvisory{},
	}

	packagesVal := mapGetByKeyword(m, "packages")
	if packagesVal != nil {
		pkgMaps, err := mapVectorEntries(packagesVal, path, ":packages")
		if err != nil {
			return nil, err
		}
		registry.Packages = make([]RegistryPackage, 0, len(pkgMaps))
		for i, pkgMap := range pkgMaps {
			name, err := mustString(mapGetByKeyword(pkgMap, "name"), path, fmt.Sprintf(":packages[%d].:name", i))
			if err != nil {
				return nil, err
			}
			version, err := mustString(mapGetByKeyword(pkgMap, "version"), path, fmt.Sprintf(":packages[%d].:version", i))
			if err != nil {
				return nil, err
			}
			version = normalizeRegistryVersion(version)
			if !isSemver(version) {
				return nil, fmt.Errorf("%s: :packages[%d].:version must be semver", path, i)
			}
			source, err := mustString(mapGetByKeyword(pkgMap, "source"), path, fmt.Sprintf(":packages[%d].:source", i))
			if err != nil {
				return nil, err
			}
			license, err := optionalString(mapGetByKeyword(pkgMap, "license"), path, fmt.Sprintf(":packages[%d].:license", i))
			if err != nil {
				return nil, err
			}
			compat, err := optionalString(mapGetByKeyword(pkgMap, "compat"), path, fmt.Sprintf(":packages[%d].:compat", i))
			if err != nil {
				return nil, err
			}
			registry.Packages = append(registry.Packages, RegistryPackage{
				Name:    name,
				Version: version,
				Source:  source,
				License: license,
				Compat:  compat,
			})
		}
	}

	advisoriesVal := mapGetByKeyword(m, "advisories")
	if advisoriesVal != nil {
		advMaps, err := mapVectorEntries(advisoriesVal, path, ":advisories")
		if err != nil {
			return nil, err
		}
		registry.Advisories = make([]RegistryAdvisory, 0, len(advMaps))
		for i, advMap := range advMaps {
			fields, err := optionalStringFields(advMap, path, fmt.Sprintf(":advisories[%d]", i), "name", "range", "severity", "id", "message")
			if err != nil {
				return nil, err
			}
			registry.Advisories = append(registry.Advisories, RegistryAdvisory{
				Name:     fields["name"],
				Range:    fields["range"],
				Severity: fields["severity"],
				ID:       fields["id"],
				Message:  fields["message"],
			})
		}
	}
	sortRegistryPackages(registry.Packages)
	return registry, nil
}

func WriteRegistryFile(path string, registry *RegistryFile) error {
	if registry == nil {
		return fmt.Errorf("registry file is nil")
	}
	sortRegistryPackages(registry.Packages)
	content := canonicalRegistryString(registry)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(content+"\n"), 0644)
}

func canonicalRegistryString(registry *RegistryFile) string {
	packages := make([]string, 0, len(registry.Packages))
	for _, pkg := range registry.Packages {
		fields := []string{
			fmt.Sprintf(":name %s", strconv.Quote(pkg.Name)),
			fmt.Sprintf(":version %s", strconv.Quote(normalizeRegistryVersion(pkg.Version))),
			fmt.Sprintf(":source %s", strconv.Quote(pkg.Source)),
		}
		if strings.TrimSpace(pkg.License) != "" {
			fields = append(fields, fmt.Sprintf(":license %s", strconv.Quote(pkg.License)))
		}
		if strings.TrimSpace(pkg.Compat) != "" {
			fields = append(fields, fmt.Sprintf(":compat %s", strconv.Quote(pkg.Compat)))
		}
		packages = append(packages, fmt.Sprintf("{%s}", strings.Join(fields, " ")))
	}

	advisories := make([]string, 0, len(registry.Advisories))
	for _, advisory := range registry.Advisories {
		fields := []string{}
		if strings.TrimSpace(advisory.Name) != "" {
			fields = append(fields, fmt.Sprintf(":name %s", strconv.Quote(advisory.Name)))
		}
		if strings.TrimSpace(advisory.Range) != "" {
			fields = append(fields, fmt.Sprintf(":range %s", strconv.Quote(advisory.Range)))
		}
		if strings.TrimSpace(advisory.Severity) != "" {
			fields = append(fields, fmt.Sprintf(":severity %s", strconv.Quote(advisory.Severity)))
		}
		if strings.TrimSpace(advisory.ID) != "" {
			fields = append(fields, fmt.Sprintf(":id %s", strconv.Quote(advisory.ID)))
		}
		if strings.TrimSpace(advisory.Message) != "" {
			fields = append(fields, fmt.Sprintf(":message %s", strconv.Quote(advisory.Message)))
		}
		advisories = append(advisories, fmt.Sprintf("{%s}", strings.Join(fields, " ")))
	}

	return fmt.Sprintf(
		"{:packages [%s] :advisories [%s]}",
		strings.Join(packages, " "),
		strings.Join(advisories, " "),
	)
}

func optionalString(v types.Value, filePath, field string) (string, error) {
	if v == nil {
		return "", nil
	}
	s, ok := v.(atom.String)
	if !ok {
		return "", fmt.Errorf("%s: %s must be a string", filePath, field)
	}
	return string(s), nil
}

func optionalStringFields(m *coll.Map, filePath, prefix string, names ...string) (map[string]string, error) {
	out := make(map[string]string, len(names))
	for _, name := range names {
		value, err := optionalString(mapGetByKeyword(m, name), filePath, fmt.Sprintf("%s.:%s", prefix, name))
		if err != nil {
			return nil, err
		}
		out[name] = value
	}
	return out, nil
}

func sortRegistryPackages(packages []RegistryPackage) {
	sort.Slice(packages, func(i, j int) bool {
		if packages[i].Name != packages[j].Name {
			return packages[i].Name < packages[j].Name
		}
		if packages[i].Version != packages[j].Version {
			return compareSemver(packages[i].Version, packages[j].Version) > 0
		}
		return packages[i].Source < packages[j].Source
	})
}

func isSemver(v string) bool {
	canonical := normalizeRegistryVersion(v)
	return canonical != "" && semver.IsValid("v"+canonical)
}

func compareSemver(a, b string) int {
	ca := normalizeRegistryVersion(a)
	cb := normalizeRegistryVersion(b)
	aValid := ca != "" && semver.IsValid("v"+ca)
	bValid := cb != "" && semver.IsValid("v"+cb)
	if aValid && bValid {
		return semver.Compare("v"+ca, "v"+cb)
	}
	if aValid && !bValid {
		return 1
	}
	if !aValid && bValid {
		return -1
	}
	ta := strings.TrimSpace(a)
	tb := strings.TrimSpace(b)
	if ta == tb {
		return 0
	}
	if ta > tb {
		return 1
	}
	return -1
}

func normalizeRegistryVersion(v string) string {
	trimmed := strings.TrimSpace(v)
	if trimmed == "" {
		return trimmed
	}
	withPrefix := trimmed
	if !strings.HasPrefix(withPrefix, "v") {
		withPrefix = "v" + withPrefix
	}
	if !semver.IsValid(withPrefix) {
		return trimmed
	}
	canonical := semver.Canonical(withPrefix)
	if canonical == "" {
		return trimmed
	}
	return strings.TrimPrefix(canonical, "v")
}
