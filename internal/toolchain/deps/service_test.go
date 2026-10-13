package deps

import (
	"testing"

	"doggo/internal/project"
)

func TestServiceAuditSplitsBlockingFindings(t *testing.T) {
	root, _ := updateProjectFixture(t)
	service := Service{
		ResolveProjectRoot: func() (string, bool, error) { return root, true, nil },
		Auditor: Auditor{AuditLock: func(_, _ string, _ *project.ModFile, _ *project.LockFile) ([]DependencyAuditFinding, error) {
			return []DependencyAuditFinding{
				{Severity: "high", Code: "blocking"},
				{Severity: "medium", Code: "warning"},
			}, nil
		}},
	}
	summary, err := service.Audit()
	if err != nil {
		t.Fatal(err)
	}
	if len(summary.Blocking) != 1 || len(summary.NonBlocking) != 1 {
		t.Fatalf("audit summary = %#v", summary)
	}
}
