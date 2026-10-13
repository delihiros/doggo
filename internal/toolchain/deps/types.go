package deps

type DependencyOutdatedStatus struct {
	Source         string
	Kind           string
	LockedRevision string
	LatestRevision string
	LockedDigest   string
	LatestDigest   string
	Outdated       bool
	CheckError     string
}

type DependencyUpdateResult struct {
	Source      string
	Kind        string
	OldRevision string
	NewRevision string
	OldDigest   string
	NewDigest   string
	Updated     bool
}

type DependencyAuditFinding struct {
	Source   string
	Kind     string
	Severity string
	Code     string
	Message  string
}
