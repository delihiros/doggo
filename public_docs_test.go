package doggo

import (
	"fmt"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"doggo/internal/project"
	"doggo/internal/types"
	"doggo/internal/vm"
)

var publicDocuments = []string{
	"README.md",
	"docs/getting-started.md",
	"docs/language-guide.md",
	"docs/standard-library.md",
	"docs/stdlib/core-and-data.md",
	"docs/stdlib/formats-and-values.md",
	"docs/stdlib/io-and-system.md",
	"docs/stdlib/application-and-tools.md",
	"docs/tooling.md",
	"docs/go-interop.md",
	"docs/ebitengine-tutorial.md",
	"docs/how-doggo-works.md",
}

var inlineMarkdownLink = regexp.MustCompile(`\[[^]]*\]\(([^\s)]+)(?:\s+[^)]*)?\)`)

var privateDocumentReferences = []string{
	"docs/dev/",
	"docs/superpowers/",
	"docs/architecture/",
	"docs/language_specification.md",
	"doggo-private",
}

func isPrivateDocumentTarget(target string) bool {
	for _, forbidden := range privateDocumentReferences {
		if strings.HasSuffix(forbidden, "/") {
			root := strings.TrimSuffix(forbidden, "/")
			if target == root || strings.HasPrefix(target, root+"/") {
				return true
			}
			continue
		}
		if strings.Contains(target, forbidden) {
			return true
		}
	}
	return false
}

func readPublicDocumentManifest(t *testing.T) []string {
	t.Helper()

	data, err := os.ReadFile("docs/public-documents.txt")
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSuffix(string(data), "\n"), "\n")
}

func TestPublicDocumentManifest(t *testing.T) {
	got := readPublicDocumentManifest(t)
	if len(got) != len(publicDocuments) {
		t.Fatalf("public document manifest has %d entries, want %d: %v", len(got), len(publicDocuments), got)
	}

	seen := make(map[string]bool, len(got))
	for i, want := range publicDocuments {
		if seen[got[i]] {
			t.Errorf("public document manifest contains duplicate %q", got[i])
		}
		seen[got[i]] = true
		if got[i] != want {
			t.Errorf("public document manifest entry %d = %q, want %q", i+1, got[i], want)
		}
	}
}

func TestPublicDocsTrackedRootGenerationContract(t *testing.T) {
	ignore, err := os.ReadFile(".gitignore")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains("\n"+string(ignore), "\n/.doggo/\n") {
		t.Fatal("root .gitignore must ignore only the repository-root .doggo directory")
	}

	content, err := os.ReadFile(project.LockFileName)
	if err != nil {
		t.Fatal(err)
	}
	for _, stale := range []string{":reify-interfaces", "bin/doggo"} {
		if strings.Contains(string(content), stale) {
			t.Errorf("tracked root lock contains stale contract %q", stale)
		}
	}
	lock, err := project.LoadLockFile(project.LockFileName)
	if err != nil {
		t.Fatalf("load tracked root lock: %v", err)
	}
	if lock.LockVersion != project.LockVersion {
		t.Errorf("tracked root lock version = %d, want %d", lock.LockVersion, project.LockVersion)
	}
	if lock.Generation == "" {
		t.Error("tracked root lock has no generation identity")
	}
	if lock.Build.Binary != "" || lock.Build.SHA256 != "" {
		t.Errorf("tracked root lock build = %+v, want sync-only", lock.Build)
	}
}

func TestBenchmarkExampleRuns(t *testing.T) {
	source, err := os.ReadFile("examples/benchmark.dog")
	if err != nil {
		t.Fatal(err)
	}
	runner, err := vm.NewRunner()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := runner.Run(string(source)); err != nil {
		t.Fatalf("run examples/benchmark.dog: %v", err)
	}
}

func TestPublicDocumentLinksAndBoundary(t *testing.T) {
	manifest := readPublicDocumentManifest(t)
	public := make(map[string]bool, len(manifest))
	for _, document := range manifest {
		public[document] = true
	}

	for _, document := range manifest {
		data, err := os.ReadFile(document)
		if err != nil {
			t.Errorf("read %s: %v", document, err)
			continue
		}
		content := string(data)
		for _, forbidden := range privateDocumentReferences {
			if strings.Contains(content, forbidden) {
				t.Errorf("%s contains private reference %q", document, forbidden)
			}
		}

		for _, match := range inlineMarkdownLink.FindAllStringSubmatch(content, -1) {
			target := strings.Trim(match[1], "<>")
			if strings.HasPrefix(target, "#") {
				continue
			}
			u, err := url.Parse(target)
			if err != nil {
				t.Errorf("%s has invalid link %q: %v", document, target, err)
				continue
			}
			if u.Scheme == "http" || u.Scheme == "https" || u.Scheme == "mailto" {
				continue
			}
			if u.Scheme != "" {
				t.Errorf("%s has unsupported local link %q", document, target)
				continue
			}
			targetPath, err := url.PathUnescape(u.Path)
			if err != nil {
				t.Errorf("%s has invalid local link %q: %v", document, target, err)
				continue
			}
			resolved := path.Clean(path.Join(path.Dir(document), targetPath))
			if isPrivateDocumentTarget(resolved) {
				t.Errorf("%s links to private target %q", document, resolved)
			}
			if _, err := os.Stat(resolved); err != nil {
				t.Errorf("%s links to missing local target %q: %v", document, resolved, err)
			}
			if document == "README.md" && strings.HasSuffix(resolved, ".md") && !public[resolved] {
				t.Errorf("README.md links to non-public document %q", resolved)
			}
		}
	}
}

func TestStandardLibraryDetailsCoverRuntimePublicVars(t *testing.T) {
	detailNamespaces := map[string][]string{
		"docs/stdlib/core-and-data.md": {
			"doggo.basis", "doggo.core", "doggo.data", "doggo.datafy", "doggo.math",
			"doggo.schema", "doggo.set", "doggo.string", "doggo.walk", "doggo.zip",
		},
		"docs/stdlib/formats-and-values.md": {
			"doggo.csv", "doggo.edn", "doggo.instant", "doggo.json", "doggo.template",
			"doggo.time", "doggo.url", "doggo.uuid", "doggo.xml",
		},
		"docs/stdlib/io-and-system.md": {
			"doggo.http", "doggo.io", "doggo.os", "doggo.process",
		},
		"docs/stdlib/application-and-tools.md": {
			"doggo.cli", "doggo.observability", "doggo.pprint", "doggo.repl",
			"doggo.stacktrace", "doggo.test",
		},
	}

	details := make(map[string]string, len(detailNamespaces))
	expectedDetail := make(map[string]string)
	for document, namespaces := range detailNamespaces {
		data, err := os.ReadFile(document)
		if err != nil {
			t.Fatal(err)
		}
		details[document] = string(data)
		for _, namespace := range namespaces {
			if previous := expectedDetail[namespace]; previous != "" {
				t.Fatalf("namespace %s assigned to both %s and %s", namespace, previous, document)
			}
			expectedDetail[namespace] = document
		}
	}

	paths, err := filepath.Glob("stdlib/doggo/*.dog")
	if err != nil {
		t.Fatal(err)
	}
	index, err := os.ReadFile("docs/standard-library.md")
	if err != nil {
		t.Fatal(err)
	}
	runner, err := vm.NewRunner()
	if err != nil {
		t.Fatal(err)
	}

	found := make(map[string]bool)
	for _, file := range paths {
		if strings.HasSuffix(file, "_test.dog") {
			continue
		}
		namespace := "doggo." + strings.TrimSuffix(filepath.Base(file), ".dog")
		found[namespace] = true
		if !strings.Contains(string(index), "`"+namespace+"`") {
			t.Errorf("standard library index is missing %s", namespace)
		}

		document := expectedDetail[namespace]
		if document == "" {
			t.Errorf("namespace %s has no detailed category document", namespace)
			continue
		}
		count := 0
		for _, text := range details {
			count += namespaceHeadingCount(text, namespace)
		}
		if count != 1 {
			t.Errorf("namespace %s has %d detailed sections, want exactly 1", namespace, count)
			continue
		}
		section, ok := exactNamespaceSection(details[document], namespace)
		if !ok {
			t.Errorf("%s is missing exact section for %s", document, namespace)
			continue
		}

		if _, err := runner.Run(fmt.Sprintf("(require '[doggo.repl :as repl] '[%s])", namespace)); err != nil {
			t.Errorf("require %s: %v", namespace, err)
			continue
		}
		value, err := runner.Run(fmt.Sprintf("(repl/dir '%s)", namespace))
		if err != nil {
			t.Errorf("inspect %s public vars: %v", namespace, err)
			continue
		}
		vars, ok := value.(types.Vector)
		if !ok {
			t.Errorf("repl/dir %s returned %T, want vector", namespace, value)
			continue
		}
		for i := 0; i < vars.Count(); i++ {
			name := vars.Nth(i).String()
			if !hasCompleteAPIRow(section, name) {
				t.Errorf("%s section is missing public API `%s`", namespace, name)
			}
		}
	}

	for namespace, document := range expectedDetail {
		if !found[namespace] {
			t.Errorf("%s assigns nonexistent namespace %s", document, namespace)
		}
	}
}

func hasCompleteAPIRow(section, name string) bool {
	for _, line := range strings.Split(section, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "|") || !strings.HasSuffix(line, "|") {
			continue
		}
		cells := strings.Split(strings.ReplaceAll(line, `\|`, "escaped-pipe"), "|")
		if len(cells) != 6 || strings.TrimSpace(cells[1]) != "`"+name+"`" {
			continue
		}
		complete := true
		for _, cell := range cells[1:5] {
			if strings.TrimSpace(cell) == "" {
				complete = false
				break
			}
		}
		if complete {
			return true
		}
	}
	return false
}

func TestStandardLibraryAPIRowRequiresFourNonEmptyColumns(t *testing.T) {
	tests := []struct {
		name    string
		section string
		want    bool
	}{
		{"body mention", "Use `name` in this example.", false},
		{"missing name", "| | args | return | side effects/errors |", false},
		{"missing args", "| `name` | | return | side effects/errors |", false},
		{"missing return", "| `name` | args | | side effects/errors |", false},
		{"missing effects", "| `name` | args | return | |", false},
		{"complete", "| `name` | args | return | side effects/errors |", true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := hasCompleteAPIRow(test.section, "name"); got != test.want {
				t.Errorf("hasCompleteAPIRow() = %t, want %t", got, test.want)
			}
		})
	}
}

func exactNamespaceSection(document, namespace string) (string, bool) {
	heading := regexp.MustCompile(`(?m)^## ` + regexp.QuoteMeta("`"+namespace+"`") + `[ \t]*\r?$`)
	locations := heading.FindAllStringIndex(document, -1)
	if len(locations) != 1 {
		return "", false
	}
	start := locations[0][0]
	next := regexp.MustCompile(`(?m)^#{1,2}(?:[ \t]+|$)`).FindStringIndex(document[locations[0][1]:])
	if next == nil {
		return document[start:], true
	}
	return document[start : locations[0][1]+next[0]], true
}

func TestStandardLibraryExactNamespaceSectionStopsAtLevelOneHeading(t *testing.T) {
	document := "## `doggo.example`\n`documented`\n\n# Appendix\n`leaked`\n"
	section, ok := exactNamespaceSection(document, "doggo.example")
	if !ok {
		t.Fatal("namespace section not found")
	}
	if strings.Contains(section, "`leaked`") {
		t.Fatal("namespace section includes content after a level-one heading")
	}
}

func namespaceHeadingCount(document, namespace string) int {
	heading := regexp.MustCompile(`(?m)^## ` + regexp.QuoteMeta("`"+namespace+"`") + `[ \t]*\r?$`)
	return len(heading.FindAllStringIndex(document, -1))
}
